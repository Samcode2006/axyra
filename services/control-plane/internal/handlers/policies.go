package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/axyra/control-plane/internal/models"
	"github.com/go-chi/chi/v5"
)

// Policy configs are typed per policy type. Unknown keys are rejected, so a
// misspelled limit fails loudly instead of silently doing nothing.
type rateLimitConfig struct {
	MaxEventsPerMinute *int `json:"max_events_per_minute"`
}

type queryLimitConfig struct {
	MaxConcurrentQueries *int `json:"max_concurrent_queries"`
	MaxQueriesPerMinute  *int `json:"max_queries_per_minute,omitempty"`
}

type dataAccessConfig struct {
	MaxLookbackDays *int     `json:"max_lookback_days,omitempty"`
	AllowedMetrics  []string `json:"allowed_metrics,omitempty"`
}

const (
	maxPolicyLimit   = 1_000_000_000
	maxLookbackDays  = 3650
	maxAllowedMetric = 100
)

// validatePolicyConfig returns the canonical JSON for a policy's config, or a
// message describing why it is invalid.
func validatePolicyConfig(policyType string, raw json.RawMessage) (json.RawMessage, string) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		raw = json.RawMessage(`{}`)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	positive := func(name string, v *int, max int) string {
		if v != nil && (*v < 1 || *v > max) {
			return fmt.Sprintf("%s must be between 1 and %d", name, max)
		}
		return ""
	}

	var out any
	switch policyType {
	case "rate_limit":
		var c rateLimitConfig
		if err := dec.Decode(&c); err != nil {
			return nil, "config is not valid for rate_limit: " + err.Error()
		}
		if c.MaxEventsPerMinute == nil {
			return nil, "rate_limit requires max_events_per_minute"
		}
		if m := positive("max_events_per_minute", c.MaxEventsPerMinute, maxPolicyLimit); m != "" {
			return nil, m
		}
		out = c
	case "query_limit":
		var c queryLimitConfig
		if err := dec.Decode(&c); err != nil {
			return nil, "config is not valid for query_limit: " + err.Error()
		}
		if c.MaxConcurrentQueries == nil {
			return nil, "query_limit requires max_concurrent_queries"
		}
		for _, e := range []struct {
			n string
			v *int
		}{{"max_concurrent_queries", c.MaxConcurrentQueries}, {"max_queries_per_minute", c.MaxQueriesPerMinute}} {
			if m := positive(e.n, e.v, maxPolicyLimit); m != "" {
				return nil, m
			}
		}
		out = c
	case "data_access":
		var c dataAccessConfig
		if err := dec.Decode(&c); err != nil {
			return nil, "config is not valid for data_access: " + err.Error()
		}
		if c.MaxLookbackDays == nil && len(c.AllowedMetrics) == 0 {
			return nil, "data_access requires max_lookback_days and/or allowed_metrics"
		}
		if m := positive("max_lookback_days", c.MaxLookbackDays, maxLookbackDays); m != "" {
			return nil, m
		}
		if len(c.AllowedMetrics) > maxAllowedMetric {
			return nil, fmt.Sprintf("allowed_metrics supports at most %d entries", maxAllowedMetric)
		}
		for _, id := range c.AllowedMetrics {
			if len(id) == 0 || len(id) > 64 {
				return nil, "allowed_metrics entries must be 1-64 characters"
			}
		}
		out = c
	default:
		return nil, "type must be one of rate_limit, query_limit, data_access"
	}
	if dec.More() {
		return nil, "config must be a single JSON object"
	}
	canonical, err := json.Marshal(out)
	if err != nil {
		return nil, "config could not be encoded"
	}
	return canonical, ""
}

const policyCols = `p.id, p.tenant_id, p.name, p.type, p.config, p.created_at`

func scanPolicy(row interface{ Scan(...any) error }) (models.Policy, error) {
	var p models.Policy
	var cfg []byte
	err := row.Scan(&p.ID, &p.TenantID, &p.Name, &p.Type, &cfg, &p.CreatedAt)
	p.Config = json.RawMessage(cfg)
	return p, err
}

func (h *Handler) ListPolicies(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	var tenant, ptype *string
	if v := r.URL.Query().Get("tenant_id"); v != "" {
		tenant = &v
	}
	if v := r.URL.Query().Get("type"); v != "" {
		ptype = &v
	}
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT `+policyCols+`
		FROM policies p JOIN tenants t ON t.id = p.tenant_id
		WHERE ($1::text IS NULL OR t.organization_id = $1)
		  AND ($2::text IS NULL OR p.tenant_id = $2)
		  AND ($3::text IS NULL OR p.type = $3)
		ORDER BY p.created_at ASC, p.id ASC`, scopeOrgFilter(r, p), tenant, ptype)
	if err != nil {
		serverError(w, r, err)
		return
	}
	defer rows.Close()

	list := make([]models.Policy, 0)
	for rows.Next() {
		pol, err := scanPolicy(rows)
		if err != nil {
			serverError(w, r, err)
			return
		}
		list = append(list, pol)
	}
	if err := rows.Err(); err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, list)
}

func (h *Handler) GetPolicy(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	id := chi.URLParam(r, "id")
	if _, ok := authorizeOrg(w, r, p, "Policy", func() (string, error) { return orgOfPolicy(r.Context(), h.db, id) }); !ok {
		return
	}
	pol, err := scanPolicy(h.db.QueryRowContext(r.Context(), `SELECT `+policyCols+` FROM policies p WHERE p.id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		notFound(w, "Policy")
		return
	}
	if err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, pol)
}

func (h *Handler) CreatePolicy(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	var req models.CreatePolicyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	name, ok := cleanName(req.Name, maxNameLen)
	if !ok || req.TenantID == "" || req.Type == "" {
		respondError(w, http.StatusBadRequest, "tenant_id, name and type are required")
		return
	}
	cfg, msg := validatePolicyConfig(req.Type, req.Config)
	if msg != "" {
		respondError(w, http.StatusBadRequest, msg)
		return
	}
	org, ok := authorizeOrg(w, r, p, "Tenant", func() (string, error) { return orgOfTenant(r.Context(), h.db, req.TenantID) })
	if !ok {
		return
	}

	var pol models.Policy
	err := h.withTx(r.Context(), func(tx *sql.Tx) error {
		var err error
		pol, err = scanPolicy(tx.QueryRowContext(r.Context(), `
			INSERT INTO policies AS p (id, tenant_id, name, type, config)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING `+policyCols, newID("pol_"), req.TenantID, name, req.Type, string(cfg)))
		if err != nil {
			return err
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: org, Action: "policy.create", ResourceType: "policy", ResourceID: pol.ID,
			Details: map[string]any{"tenant_id": req.TenantID, "type": req.Type, "name": name, "config": json.RawMessage(cfg)}})
	})
	if err != nil {
		if isForeignKeyViolation(err) {
			notFound(w, "Tenant")
			return
		}
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusCreated, pol)
}

// UpdatePolicy changes a policy's name and/or config. Its type and tenant are
// fixed; create a new policy to change those.
func (h *Handler) UpdatePolicy(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	id := chi.URLParam(r, "id")
	var req models.UpdatePolicyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == nil && len(req.Config) == 0 {
		respondError(w, http.StatusBadRequest, "Nothing to update: provide name and/or config")
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

	var pol models.Policy
	err := h.withTx(r.Context(), func(tx *sql.Tx) error {
		var org, ptype string
		if err := tx.QueryRowContext(r.Context(), `
			SELECT t.organization_id, p.type FROM policies p JOIN tenants t ON t.id = p.tenant_id
			WHERE p.id = $1 FOR UPDATE OF p`, id).Scan(&org, &ptype); err != nil {
			return err
		}
		if !p.CanAccessOrg(org) {
			return sql.ErrNoRows
		}

		var cfg *string
		if len(req.Config) > 0 {
			canonical, msg := validatePolicyConfig(ptype, req.Config)
			if msg != "" {
				return validationError(msg)
			}
			s := string(canonical)
			cfg = &s
		}
		var err error
		pol, err = scanPolicy(tx.QueryRowContext(r.Context(), `
			UPDATE policies AS p SET name = COALESCE($2, p.name), config = COALESCE($3::jsonb, p.config)
			WHERE p.id = $1 RETURNING `+policyCols, id, name, cfg))
		if err != nil {
			return err
		}
		changed := []string{}
		if name != nil {
			changed = append(changed, "name")
		}
		if cfg != nil {
			changed = append(changed, "config")
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: org, Action: "policy.update", ResourceType: "policy", ResourceID: id,
			Details: map[string]any{"changed": changed, "config": pol.Config}})
	})
	var ve validationError
	switch {
	case errors.As(err, &ve):
		respondError(w, http.StatusBadRequest, string(ve))
	case errors.Is(err, sql.ErrNoRows):
		notFound(w, "Policy")
	case err != nil:
		serverError(w, r, err)
	default:
		respondJSON(w, http.StatusOK, pol)
	}
}

// validationError lets a transaction callback report a client error.
type validationError string

func (v validationError) Error() string { return string(v) }

func (h *Handler) DeletePolicy(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	id := chi.URLParam(r, "id")
	err := h.withTx(r.Context(), func(tx *sql.Tx) error {
		var org, tenantID, name, ptype string
		if err := tx.QueryRowContext(r.Context(), `
			SELECT t.organization_id, p.tenant_id, p.name, p.type
			FROM policies p JOIN tenants t ON t.id = p.tenant_id
			WHERE p.id = $1 FOR UPDATE OF p`, id).Scan(&org, &tenantID, &name, &ptype); err != nil {
			return err
		}
		if !p.CanAccessOrg(org) {
			return sql.ErrNoRows
		}
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM policies WHERE id = $1`, id); err != nil {
			return err
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: org, Action: "policy.delete", ResourceType: "policy", ResourceID: id,
			Details: map[string]any{"tenant_id": tenantID, "name": name, "type": ptype}})
	})
	if errors.Is(err, sql.ErrNoRows) {
		notFound(w, "Policy")
		return
	}
	if err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
