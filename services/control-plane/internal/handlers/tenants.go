package handlers

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/axyra/control-plane/internal/models"
	"github.com/go-chi/chi/v5"
)

const (
	defaultMaxEventsPerDay      int64 = 1_000_000
	defaultMaxConcurrentQueries       = 10

	// Upper bounds keep a typo from creating an effectively unlimited tenant.
	maxEventsPerDayLimit      int64 = 1_000_000_000_000
	maxConcurrentQueriesLimit       = 1000
)

var tenantStatuses = map[string]bool{"active": true, "suspended": true, "inactive": true}

const tenantCols = `id, organization_id, external_tenant_id, name, status, max_events_per_day, max_concurrent_queries, created_at`

func scanTenant(row interface{ Scan(...any) error }) (models.Tenant, error) {
	var t models.Tenant
	var q models.TenantQuota
	err := row.Scan(&t.ID, &t.OrganizationID, &t.ExternalTenantID, &t.Name, &t.Status,
		&q.MaxEventsPerDay, &q.MaxConcurrentQueries, &t.CreatedAt)
	t.Quota = &q
	return t, err
}

// validateQuota checks only the fields that were supplied.
func validateQuota(q *models.UpdateTenantQuota) (string, bool) {
	if q == nil {
		return "", true
	}
	if q.MaxEventsPerDay != nil && (*q.MaxEventsPerDay < 1 || *q.MaxEventsPerDay > maxEventsPerDayLimit) {
		return "max_events_per_day must be between 1 and 1000000000000", false
	}
	if q.MaxConcurrentQueries != nil && (*q.MaxConcurrentQueries < 1 || *q.MaxConcurrentQueries > maxConcurrentQueriesLimit) {
		return "max_concurrent_queries must be between 1 and 1000", false
	}
	return "", true
}

func (h *Handler) ListTenants(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	var status *string
	if v := r.URL.Query().Get("status"); v != "" {
		if !tenantStatuses[v] {
			respondError(w, http.StatusBadRequest, "status must be one of active, suspended, inactive")
			return
		}
		status = &v
	}
	page := queryInt(r, "page", 1, 1, 1_000_000)
	perPage := queryInt(r, "per_page", 50, 1, 200)
	org := scopeOrgFilter(r, p)

	var total int
	if err := h.db.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM tenants
		WHERE ($1::text IS NULL OR organization_id = $1) AND ($2::text IS NULL OR status = $2)`,
		org, status).Scan(&total); err != nil {
		serverError(w, r, err)
		return
	}

	rows, err := h.db.QueryContext(r.Context(), `
		SELECT `+tenantCols+` FROM tenants
		WHERE ($1::text IS NULL OR organization_id = $1) AND ($2::text IS NULL OR status = $2)
		ORDER BY created_at ASC, id ASC
		LIMIT $3 OFFSET $4`, org, status, perPage, (page-1)*perPage)
	if err != nil {
		serverError(w, r, err)
		return
	}
	defer rows.Close()

	list := make([]models.Tenant, 0)
	for rows.Next() {
		t, err := scanTenant(rows)
		if err != nil {
			serverError(w, r, err)
			return
		}
		list = append(list, t)
	}
	if err := rows.Err(); err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, models.PaginatedResponse[models.Tenant]{Data: list, Total: total, Page: page, PerPage: perPage})
}

func (h *Handler) GetTenant(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	t, err := scanTenant(h.db.QueryRowContext(r.Context(),
		`SELECT `+tenantCols+` FROM tenants WHERE id = $1`, chi.URLParam(r, "id")))
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !p.CanAccessOrg(t.OrganizationID)) {
		notFound(w, "Tenant")
		return
	}
	if err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, t)
}

func (h *Handler) CreateTenant(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	var req models.CreateTenantRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	name, ok1 := cleanName(req.Name, maxNameLen)
	ext, ok2 := cleanExternalID(req.ExternalTenantID)
	if !ok1 || !ok2 {
		respondError(w, http.StatusBadRequest, "name (max 255) and external_tenant_id (max 64) are required")
		return
	}
	if msg, ok := validateQuota(req.Quota); !ok {
		respondError(w, http.StatusBadRequest, msg)
		return
	}
	orgID, ok := resolveCreateOrg(w, p, req.OrganizationID)
	if !ok {
		return
	}

	events, queries := defaultMaxEventsPerDay, defaultMaxConcurrentQueries
	if req.Quota != nil {
		if req.Quota.MaxEventsPerDay != nil {
			events = *req.Quota.MaxEventsPerDay
		}
		if req.Quota.MaxConcurrentQueries != nil {
			queries = *req.Quota.MaxConcurrentQueries
		}
	}

	var t models.Tenant
	err := h.withTx(r.Context(), func(tx *sql.Tx) error {
		var err error
		t, err = scanTenant(tx.QueryRowContext(r.Context(), `
			INSERT INTO tenants (id, organization_id, external_tenant_id, name, status, max_events_per_day, max_concurrent_queries)
			VALUES ($1, $2, $3, $4, 'active', $5, $6)
			RETURNING `+tenantCols, newID("tenant_"), orgID, ext, name, events, queries))
		if err != nil {
			return err
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: orgID, Action: "tenant.create", ResourceType: "tenant", ResourceID: t.ID,
			Details: map[string]any{"name": name, "external_tenant_id": ext, "quota": t.Quota}})
	})
	if err != nil {
		if isForeignKeyViolation(err) {
			notFound(w, "Organization")
			return
		}
		writeDBError(w, r, err, "A tenant with this external_tenant_id already exists in the organization")
		return
	}
	respondJSON(w, http.StatusCreated, t)
}

func (h *Handler) UpdateTenant(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	id := chi.URLParam(r, "id")
	var req models.UpdateTenantRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == nil && req.Status == nil && req.Quota == nil {
		respondError(w, http.StatusBadRequest, "Nothing to update: provide name, status and/or quota")
		return
	}
	if req.Status != nil && !tenantStatuses[*req.Status] {
		respondError(w, http.StatusBadRequest, "status must be one of active, suspended, inactive")
		return
	}
	var name *string
	if req.Name != nil {
		n, ok := cleanName(*req.Name, maxNameLen)
		if !ok {
			respondError(w, http.StatusBadRequest, "name must be 1-255 characters")
			return
		}
		name = &n
	}
	if msg, ok := validateQuota(req.Quota); !ok {
		respondError(w, http.StatusBadRequest, msg)
		return
	}
	var events *int64
	var queries *int
	if req.Quota != nil {
		events, queries = req.Quota.MaxEventsPerDay, req.Quota.MaxConcurrentQueries
	}

	var t models.Tenant
	err := h.withTx(r.Context(), func(tx *sql.Tx) error {
		before, err := scanTenant(tx.QueryRowContext(r.Context(),
			`SELECT `+tenantCols+` FROM tenants WHERE id = $1 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		if !p.CanAccessOrg(before.OrganizationID) {
			return sql.ErrNoRows
		}

		// All requested changes are applied in one statement, so a partial
		// update can never be observed or half-committed.
		t, err = scanTenant(tx.QueryRowContext(r.Context(), `
			UPDATE tenants SET
				name = COALESCE($2, name),
				status = COALESCE($3, status),
				max_events_per_day = COALESCE($4, max_events_per_day),
				max_concurrent_queries = COALESCE($5, max_concurrent_queries)
			WHERE id = $1
			RETURNING `+tenantCols, id, name, req.Status, events, queries))
		if err != nil {
			return err
		}

		changes := map[string]any{}
		if name != nil && *name != before.Name {
			changes["name"] = map[string]any{"from": before.Name, "to": t.Name}
		}
		if req.Status != nil && *req.Status != before.Status {
			changes["status"] = map[string]any{"from": before.Status, "to": t.Status}
		}
		if before.Quota.MaxEventsPerDay != t.Quota.MaxEventsPerDay {
			changes["max_events_per_day"] = map[string]any{"from": before.Quota.MaxEventsPerDay, "to": t.Quota.MaxEventsPerDay}
		}
		if before.Quota.MaxConcurrentQueries != t.Quota.MaxConcurrentQueries {
			changes["max_concurrent_queries"] = map[string]any{"from": before.Quota.MaxConcurrentQueries, "to": t.Quota.MaxConcurrentQueries}
		}
		if len(changes) == 0 {
			return nil // nothing actually changed; do not write a misleading audit record
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: t.OrganizationID, Action: "tenant.update",
			ResourceType: "tenant", ResourceID: id, Details: changes})
	})
	if errors.Is(err, sql.ErrNoRows) {
		notFound(w, "Tenant")
		return
	}
	if err != nil {
		writeDBError(w, r, err, "Tenant already exists")
		return
	}
	respondJSON(w, http.StatusOK, t)
}

// DeleteTenant removes a tenant together with its mappings and policies.
// Already-ingested analytics rows are keyed by tenant id and are not touched here.
func (h *Handler) DeleteTenant(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	id := chi.URLParam(r, "id")
	err := h.withTx(r.Context(), func(tx *sql.Tx) error {
		t, err := scanTenant(tx.QueryRowContext(r.Context(),
			`SELECT `+tenantCols+` FROM tenants WHERE id = $1 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		if !p.CanAccessOrg(t.OrganizationID) {
			return sql.ErrNoRows
		}
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM tenants WHERE id = $1`, id); err != nil {
			return err
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: t.OrganizationID, Action: "tenant.delete", ResourceType: "tenant",
			ResourceID: id, Details: map[string]any{"name": t.Name, "external_tenant_id": t.ExternalTenantID}})
	})
	if errors.Is(err, sql.ErrNoRows) {
		notFound(w, "Tenant")
		return
	}
	if err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
