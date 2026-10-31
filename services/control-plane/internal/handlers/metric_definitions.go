package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/axyra/control-plane/internal/auth"
	"github.com/axyra/control-plane/internal/metrics"
	"github.com/axyra/control-plane/internal/models"
	"github.com/go-chi/chi/v5"
)

const metricCols = `id, organization_id, name, description, config, enabled, organization_id IS NULL, created_at, updated_at`

// GetMetricForTenant is service-only. Derive ownership from the authorized
// tenant, never from a caller-provided organization ID. Console routes retain
// their existing JWT checks and are not accessible with the service credential.
func (h *Handler) GetMetricForTenant(w http.ResponseWriter, r *http.Request) {
	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		respondError(w, http.StatusBadRequest, "tenant_id is required")
		return
	}
	m, err := scanMetric(h.db.QueryRowContext(r.Context(), `
		SELECT `+metricCols+` FROM metric_definitions
		WHERE id = $1 AND EXISTS (
			SELECT 1 FROM tenants t WHERE t.id = $2
			AND (metric_definitions.organization_id IS NULL OR metric_definitions.organization_id = t.organization_id))
		ORDER BY (organization_id IS NULL) ASC LIMIT 1`, chi.URLParam(r, "metric_id"), tenantID))
	if errors.Is(err, sql.ErrNoRows) {
		notFound(w, "Metric definition")
		return
	}
	if err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, m)
}

func scanMetric(row interface{ Scan(...any) error }) (models.MetricDefinition, error) {
	var m models.MetricDefinition
	var cfg []byte
	err := row.Scan(&m.ID, &m.OrganizationID, &m.Name, &m.Description, &cfg, &m.Enabled, &m.Builtin, &m.CreatedAt, &m.UpdatedAt)
	m.Config = json.RawMessage(cfg)
	return m, err
}

// metricOwner resolves which definition a request addresses: an organization's
// own, or the built-in scope. Admins are always pinned to their organization
// (falling back to built-ins on read); operators choose with ?organization_id=.
func metricOwner(r *http.Request, p *auth.Principal) string {
	if !p.IsOperator() {
		return p.OrgID
	}
	return r.URL.Query().Get("organization_id")
}

// ListMetricDefinitions returns built-in definitions plus the caller's
// organization's own. Operators see everything, or one organization with ?organization_id=.
func (h *Handler) ListMetricDefinitions(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT `+metricCols+` FROM metric_definitions
		WHERE ($1::text IS NULL OR organization_id IS NULL OR organization_id = $1)
		ORDER BY (organization_id IS NULL) DESC, id ASC`, scopeOrgFilter(r, p))
	if err != nil {
		serverError(w, r, err)
		return
	}
	defer rows.Close()

	list := make([]models.MetricDefinition, 0)
	for rows.Next() {
		m, err := scanMetric(rows)
		if err != nil {
			serverError(w, r, err)
			return
		}
		list = append(list, m)
	}
	if err := rows.Err(); err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, list)
}

func (h *Handler) GetMetricDefinition(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	id := chi.URLParam(r, "metric_id")
	owner := metricOwner(r, p)

	// An organization's own definition wins; otherwise fall back to the built-in.
	m, err := scanMetric(h.db.QueryRowContext(r.Context(), `
		SELECT `+metricCols+` FROM metric_definitions
		WHERE id = $1 AND (organization_id = NULLIF($2, '') OR organization_id IS NULL)
		ORDER BY (organization_id IS NULL) ASC LIMIT 1`, id, owner))
	if errors.Is(err, sql.ErrNoRows) {
		notFound(w, "Metric definition")
		return
	}
	if err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, m)
}

// PutMetricDefinition creates or replaces a definition (PUT is idempotent).
// The body is validated against the fixed metric schema; it can never carry SQL.
func (h *Handler) PutMetricDefinition(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	id := chi.URLParam(r, "metric_id")
	if !metrics.ValidID(id) {
		respondError(w, http.StatusBadRequest, "metric id must be 3-64 characters: lowercase letters, digits and underscores, starting with a letter")
		return
	}

	var req models.PutMetricDefinitionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	name, ok := cleanName(req.Name, metrics.MaxNameLen)
	if !ok {
		respondError(w, http.StatusBadRequest, "name is required (max 255 characters)")
		return
	}
	desc, ok := cleanDescription(req.Description)
	if !ok {
		respondError(w, http.StatusBadRequest, "description is too long (max 2000 characters)")
		return
	}
	cfg, err := metrics.ParseConfig(req.Config)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	canonical, err := cfg.Marshal()
	if err != nil {
		serverError(w, r, err)
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	// Admins always write to their own organization. Operators write to the
	// named organization, or to the built-in scope when they name none.
	var orgID string
	if p.IsOperator() {
		orgID = req.OrganizationID
	} else {
		if req.OrganizationID != "" && req.OrganizationID != p.OrgID {
			notFound(w, "Organization")
			return
		}
		orgID = p.OrgID
	}

	var m models.MetricDefinition
	var created bool
	err = h.withTx(r.Context(), func(tx *sql.Tx) error {
		if orgID != "" {
			// A customer definition must not shadow a built-in one.
			var reserved bool
			if err := tx.QueryRowContext(r.Context(),
				`SELECT EXISTS(SELECT 1 FROM metric_definitions WHERE id = $1 AND organization_id IS NULL)`, id).Scan(&reserved); err != nil {
				return err
			}
			if reserved {
				return errMetricReserved
			}
		}

		var err error
		row := tx.QueryRowContext(r.Context(), `
			INSERT INTO metric_definitions AS m (id, organization_id, name, description, config, enabled)
			VALUES ($1, NULLIF($2, ''), $3, $4, $5, $6)
			ON CONFLICT (scope, id) DO UPDATE
			   SET name = EXCLUDED.name, description = EXCLUDED.description,
			       config = EXCLUDED.config, enabled = EXCLUDED.enabled, updated_at = NOW()
			RETURNING m.id, m.organization_id, m.name, m.description, m.config, m.enabled,
			          m.organization_id IS NULL, m.created_at, m.updated_at, (xmax = 0)`,
			id, orgID, name, desc, string(canonical), enabled)
		var cfgRaw []byte
		err = row.Scan(&m.ID, &m.OrganizationID, &m.Name, &m.Description, &cfgRaw, &m.Enabled, &m.Builtin, &m.CreatedAt, &m.UpdatedAt, &created)
		if err != nil {
			return err
		}
		m.Config = json.RawMessage(cfgRaw)

		action := "metric_definition.update"
		if created {
			action = "metric_definition.create"
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: orgID, Action: action, ResourceType: "metric_definition", ResourceID: id,
			Details: map[string]any{"name": name, "enabled": enabled, "config": json.RawMessage(canonical)}})
	})
	switch {
	case errors.Is(err, errMetricReserved):
		respondError(w, http.StatusConflict, "This id is reserved by a built-in metric; choose another id")
	case err != nil && isForeignKeyViolation(err):
		notFound(w, "Organization")
	case err != nil:
		serverError(w, r, err)
	case created:
		respondJSON(w, http.StatusCreated, m)
	default:
		respondJSON(w, http.StatusOK, m)
	}
}

var errMetricReserved = errors.New("metric id reserved by built-in")

// DeleteMetricDefinition removes an organization's own definition. Built-in
// definitions cannot be deleted (they would be re-created at startup); disable them instead.
func (h *Handler) DeleteMetricDefinition(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	id := chi.URLParam(r, "metric_id")
	owner := metricOwner(r, p)
	if owner == "" {
		respondError(w, http.StatusBadRequest, "Built-in metrics cannot be deleted; set enabled to false instead")
		return
	}

	err := h.withTx(r.Context(), func(tx *sql.Tx) error {
		var name string
		if err := tx.QueryRowContext(r.Context(),
			`DELETE FROM metric_definitions WHERE id = $1 AND organization_id = $2 RETURNING name`, id, owner).Scan(&name); err != nil {
			return err
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: owner, Action: "metric_definition.delete",
			ResourceType: "metric_definition", ResourceID: id, Details: map[string]any{"name": name}})
	})
	if errors.Is(err, sql.ErrNoRows) {
		notFound(w, "Metric definition")
		return
	}
	if err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
