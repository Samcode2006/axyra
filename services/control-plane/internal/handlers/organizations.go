package handlers

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/axyra/control-plane/internal/models"
	"github.com/go-chi/chi/v5"
)

const maxNameLen = 255

func (h *Handler) ListOrganizations(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}

	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, name, created_at
		FROM organizations
		WHERE ($1::text IS NULL OR id = $1)
		ORDER BY created_at ASC, id ASC`, p.OrgScope())
	if err != nil {
		serverError(w, r, err)
		return
	}
	defer rows.Close()

	orgs := make([]models.Organization, 0)
	for rows.Next() {
		var o models.Organization
		if err := rows.Scan(&o.ID, &o.Name, &o.CreatedAt); err != nil {
			serverError(w, r, err)
			return
		}
		orgs = append(orgs, o)
	}
	if err := rows.Err(); err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, orgs)
}

func (h *Handler) GetOrganization(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	id := chi.URLParam(r, "id")
	if !p.CanAccessOrg(id) {
		notFound(w, "Organization")
		return
	}
	var o models.Organization
	err := h.db.QueryRowContext(r.Context(),
		`SELECT id, name, created_at FROM organizations WHERE id = $1`, id).Scan(&o.ID, &o.Name, &o.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		notFound(w, "Organization")
		return
	}
	if err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, o)
}

// CreateOrganization onboards a new customer. Operators only: customers cannot
// create sibling organizations.
func (h *Handler) CreateOrganization(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil || !requireOperator(w, p) {
		return
	}
	var req models.CreateOrganizationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	name, ok := cleanName(req.Name, maxNameLen)
	if !ok {
		respondError(w, http.StatusBadRequest, "name is required (max 255 characters)")
		return
	}

	var o models.Organization
	o.ID, o.Name = newID("org_"), name
	err := h.withTx(r.Context(), func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(r.Context(),
			`INSERT INTO organizations (id, name) VALUES ($1, $2) RETURNING created_at`, o.ID, o.Name).Scan(&o.CreatedAt); err != nil {
			return err
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: o.ID, Action: "organization.create",
			ResourceType: "organization", ResourceID: o.ID, Details: map[string]any{"name": o.Name}})
	})
	if err != nil {
		writeDBError(w, r, err, "Organization already exists")
		return
	}
	respondJSON(w, http.StatusCreated, o)
}

func (h *Handler) UpdateOrganization(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	id := chi.URLParam(r, "id")
	if !p.CanAccessOrg(id) {
		notFound(w, "Organization")
		return
	}
	var req models.UpdateOrganizationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == nil {
		respondError(w, http.StatusBadRequest, "Nothing to update: name is required")
		return
	}
	name, ok := cleanName(*req.Name, maxNameLen)
	if !ok {
		respondError(w, http.StatusBadRequest, "name must be 1-255 characters")
		return
	}

	var o models.Organization
	err := h.withTx(r.Context(), func(tx *sql.Tx) error {
		var old string
		if err := tx.QueryRowContext(r.Context(),
			`SELECT name FROM organizations WHERE id = $1 FOR UPDATE`, id).Scan(&old); err != nil {
			return err
		}
		if err := tx.QueryRowContext(r.Context(),
			`UPDATE organizations SET name = $2 WHERE id = $1 RETURNING id, name, created_at`, id, name).
			Scan(&o.ID, &o.Name, &o.CreatedAt); err != nil {
			return err
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: id, Action: "organization.update",
			ResourceType: "organization", ResourceID: id, Details: map[string]any{"name": map[string]string{"from": old, "to": name}}})
	})
	if errors.Is(err, sql.ErrNoRows) {
		notFound(w, "Organization")
		return
	}
	if err != nil {
		writeDBError(w, r, err, "Organization already exists")
		return
	}
	respondJSON(w, http.StatusOK, o)
}

// DeleteOrganization removes a customer and, by cascade, all of its
// applications, agents, tokens, tenants, mappings and policies. Operators only.
func (h *Handler) DeleteOrganization(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil || !requireOperator(w, p) {
		return
	}
	id := chi.URLParam(r, "id")

	err := h.withTx(r.Context(), func(tx *sql.Tx) error {
		var name string
		var apps, tenants int
		if err := tx.QueryRowContext(r.Context(), `
			SELECT o.name,
			       (SELECT COUNT(*) FROM applications WHERE organization_id = o.id),
			       (SELECT COUNT(*) FROM tenants WHERE organization_id = o.id)
			FROM organizations o WHERE o.id = $1 FOR UPDATE`, id).Scan(&name, &apps, &tenants); err != nil {
			return err
		}
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM organizations WHERE id = $1`, id); err != nil {
			return err
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: id, Action: "organization.delete",
			ResourceType: "organization", ResourceID: id,
			Details: map[string]any{"name": name, "applications_removed": apps, "tenants_removed": tenants}})
	})
	if errors.Is(err, sql.ErrNoRows) {
		notFound(w, "Organization")
		return
	}
	if err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
