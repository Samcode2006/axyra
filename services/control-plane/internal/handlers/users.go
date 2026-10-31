package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"unicode/utf8"

	"github.com/axyra/control-plane/internal/auth"
	"github.com/axyra/control-plane/internal/models"
	"github.com/go-chi/chi/v5"
)

const (
	minPasswordLen = 12
	maxPasswordLen = 128 // bounds PBKDF2 work per request
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]{2,63}$`)

type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// ListOrganizationUsers lists console users of an organization. Admins may
// list their own organization; operators any.
func (h *Handler) ListOrganizationUsers(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	orgID := chi.URLParam(r, "id")
	if !p.CanAccessOrg(orgID) {
		notFound(w, "Organization")
		return
	}
	rows, err := h.db.QueryContext(r.Context(),
		`SELECT id, username, role, organization_id FROM users WHERE organization_id = $1 ORDER BY created_at ASC, id ASC`, orgID)
	if err != nil {
		serverError(w, r, err)
		return
	}
	defer rows.Close()

	list := make([]models.User, 0)
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.OrganizationID); err != nil {
			serverError(w, r, err)
			return
		}
		list = append(list, u)
	}
	if err := rows.Err(); err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, list)
}

// CreateOrganizationUser creates a customer administrator for an organization.
// Operators only: it is how a newly onboarded customer gets its first login.
func (h *Handler) CreateOrganizationUser(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil || !requireOperator(w, p) {
		return
	}
	orgID := chi.URLParam(r, "id")
	var req createUserRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !usernamePattern.MatchString(req.Username) {
		respondError(w, http.StatusBadRequest, "username must be 3-64 characters: letters, digits, '.', '_', '@' or '-'")
		return
	}
	if n := utf8.RuneCountInString(req.Password); n < minPasswordLen || len(req.Password) > maxPasswordLen {
		respondError(w, http.StatusBadRequest, "password must be 12-128 characters")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		serverError(w, r, err)
		return
	}

	u := models.User{ID: newID("user_"), Username: req.Username, Role: string(auth.RoleAdmin), OrganizationID: &orgID}
	err = h.withTx(r.Context(), func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(r.Context(),
			`INSERT INTO users (id, organization_id, username, password_hash, role) VALUES ($1, $2, $3, $4, 'admin')`,
			u.ID, orgID, u.Username, hash); err != nil {
			return err
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: orgID, Action: "user.create", ResourceType: "user",
			ResourceID: u.ID, Details: map[string]any{"username": u.Username, "role": u.Role}})
	})
	if err != nil {
		if isForeignKeyViolation(err) {
			notFound(w, "Organization")
			return
		}
		writeDBError(w, r, err, "Username is already taken")
		return
	}
	respondJSON(w, http.StatusCreated, u)
}

// DeleteOrganizationUser removes a console user. Operators only. Existing
// sessions stay valid until their JWT expires (at most 12 hours).
func (h *Handler) DeleteOrganizationUser(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil || !requireOperator(w, p) {
		return
	}
	orgID, userID := chi.URLParam(r, "id"), chi.URLParam(r, "user_id")
	err := h.withTx(r.Context(), func(tx *sql.Tx) error {
		var username string
		if err := tx.QueryRowContext(r.Context(),
			`DELETE FROM users WHERE id = $1 AND organization_id = $2 RETURNING username`, userID, orgID).Scan(&username); err != nil {
			return err
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: orgID, Action: "user.delete", ResourceType: "user",
			ResourceID: userID, Details: map[string]any{"username": username}})
	})
	if errors.Is(err, sql.ErrNoRows) {
		notFound(w, "User")
		return
	}
	if err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
