package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/axyra/control-plane/internal/models"
	"github.com/google/uuid"
)

func (h *Handler) ListOrganizations(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, name, created_at 
		FROM organizations 
		ORDER BY created_at ASC
	`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Query error: %v", err))
		return
	}
	defer rows.Close()

	orgs := make([]models.Organization, 0)
	for rows.Next() {
		var o models.Organization
		if err := rows.Scan(&o.ID, &o.Name, &o.CreatedAt); err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("Scan error: %v", err))
			return
		}
		orgs = append(orgs, o)
	}

	respondJSON(w, http.StatusOK, orgs)
}

func (h *Handler) CreateOrganization(w http.ResponseWriter, r *http.Request) {
	var req models.CreateOrganizationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		respondError(w, http.StatusBadRequest, "Invalid request body or missing name")
		return
	}

	id := "org_" + uuid.New().String()[:8]
	now := time.Now().UTC()

	_, err := h.db.ExecContext(r.Context(), `
		INSERT INTO organizations (id, name, created_at)
		VALUES ($1, $2, $3)
	`, id, req.Name, now)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create organization: %v", err))
		return
	}

	respondJSON(w, http.StatusCreated, models.Organization{
		ID:        id,
		Name:      req.Name,
		CreatedAt: now,
	})
}
