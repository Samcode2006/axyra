package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/axyra/control-plane/internal/models"
	"github.com/go-chi/chi/v5"
)

const maxDescriptionLen = 2000

const applicationCols = `id, organization_id, name, description, created_at`

func scanApplication(row interface{ Scan(...any) error }) (models.Application, error) {
	var a models.Application
	err := row.Scan(&a.ID, &a.OrganizationID, &a.Name, &a.Description, &a.CreatedAt)
	return a, err
}

// cleanDescription trims an optional description; ok=false if it is too long.
func cleanDescription(s *string) (*string, bool) {
	if s == nil {
		return nil, true
	}
	v := strings.TrimSpace(*s)
	if utf8.RuneCountInString(v) > maxDescriptionLen {
		return nil, false
	}
	return &v, true
}

func (h *Handler) ListApplications(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT `+applicationCols+`
		FROM applications
		WHERE ($1::text IS NULL OR organization_id = $1)
		ORDER BY created_at ASC, id ASC`, scopeOrgFilter(r, p))
	if err != nil {
		serverError(w, r, err)
		return
	}
	defer rows.Close()

	apps := make([]models.Application, 0)
	for rows.Next() {
		a, err := scanApplication(rows)
		if err != nil {
			serverError(w, r, err)
			return
		}
		apps = append(apps, a)
	}
	if err := rows.Err(); err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, apps)
}

func (h *Handler) GetApplication(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	a, err := scanApplication(h.db.QueryRowContext(r.Context(),
		`SELECT `+applicationCols+` FROM applications WHERE id = $1`, chi.URLParam(r, "id")))
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !p.CanAccessOrg(a.OrganizationID)) {
		notFound(w, "Application")
		return
	}
	if err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, a)
}

func (h *Handler) CreateApplication(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	var req models.CreateApplicationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	name, ok := cleanName(req.Name, maxNameLen)
	if !ok {
		respondError(w, http.StatusBadRequest, "name is required (max 255 characters)")
		return
	}
	desc, ok := cleanDescription(req.Description)
	if !ok {
		respondError(w, http.StatusBadRequest, "description is too long (max 2000 characters)")
		return
	}
	orgID, ok := resolveCreateOrg(w, p, req.OrganizationID)
	if !ok {
		return
	}

	var a models.Application
	err := h.withTx(r.Context(), func(tx *sql.Tx) error {
		var err error
		a, err = scanApplication(tx.QueryRowContext(r.Context(), `
			INSERT INTO applications (id, organization_id, name, description)
			VALUES ($1, $2, $3, $4)
			RETURNING `+applicationCols, newID("app_"), orgID, name, desc))
		if err != nil {
			return err
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: orgID, Action: "application.create",
			ResourceType: "application", ResourceID: a.ID, Details: map[string]any{"name": name}})
	})
	if err != nil {
		var missingOrg = isForeignKeyViolation(err)
		if missingOrg {
			notFound(w, "Organization")
			return
		}
		writeDBError(w, r, err, "Application already exists")
		return
	}
	respondJSON(w, http.StatusCreated, a)
}

func (h *Handler) UpdateApplication(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	id := chi.URLParam(r, "id")
	var req models.UpdateApplicationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == nil && req.Description == nil {
		respondError(w, http.StatusBadRequest, "Nothing to update: provide name and/or description")
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
	desc, ok := cleanDescription(req.Description)
	if !ok {
		respondError(w, http.StatusBadRequest, "description is too long (max 2000 characters)")
		return
	}

	var a models.Application
	err := h.withTx(r.Context(), func(tx *sql.Tx) error {
		var org string
		if err := tx.QueryRowContext(r.Context(),
			`SELECT organization_id FROM applications WHERE id = $1 FOR UPDATE`, id).Scan(&org); err != nil {
			return err
		}
		if !p.CanAccessOrg(org) {
			return sql.ErrNoRows
		}
		var err error
		a, err = scanApplication(tx.QueryRowContext(r.Context(), `
			UPDATE applications
			SET name = COALESCE($2, name),
			    description = CASE WHEN $4 THEN $3 ELSE description END
			WHERE id = $1
			RETURNING `+applicationCols, id, name, desc, req.Description != nil))
		if err != nil {
			return err
		}
		changed := []string{}
		if name != nil {
			changed = append(changed, "name")
		}
		if req.Description != nil {
			changed = append(changed, "description")
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: org, Action: "application.update",
			ResourceType: "application", ResourceID: id, Details: map[string]any{"changed": changed}})
	})
	if errors.Is(err, sql.ErrNoRows) {
		notFound(w, "Application")
		return
	}
	if err != nil {
		writeDBError(w, r, err, "Application already exists")
		return
	}
	respondJSON(w, http.StatusOK, a)
}

// DeleteApplication removes an application and, by cascade, its agents and tokens.
func (h *Handler) DeleteApplication(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	id := chi.URLParam(r, "id")
	err := h.withTx(r.Context(), func(tx *sql.Tx) error {
		var org, name string
		var agents int
		if err := tx.QueryRowContext(r.Context(), `
			SELECT a.organization_id, a.name, (SELECT COUNT(*) FROM agents WHERE application_id = a.id)
			FROM applications a WHERE a.id = $1 FOR UPDATE`, id).Scan(&org, &name, &agents); err != nil {
			return err
		}
		if !p.CanAccessOrg(org) {
			return sql.ErrNoRows
		}
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM applications WHERE id = $1`, id); err != nil {
			return err
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: org, Action: "application.delete",
			ResourceType: "application", ResourceID: id, Details: map[string]any{"name": name, "agents_removed": agents}})
	})
	if errors.Is(err, sql.ErrNoRows) {
		notFound(w, "Application")
		return
	}
	if err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
