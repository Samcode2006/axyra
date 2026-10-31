package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/axyra/control-plane/internal/models"
	"github.com/google/uuid"
)

func (h *Handler) ListApplications(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, organization_id, name, description, created_at 
		FROM applications 
		ORDER BY created_at ASC
	`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Query error: %v", err))
		return
	}
	defer rows.Close()

	apps := make([]models.Application, 0)
	for rows.Next() {
		var a models.Application
		if err := rows.Scan(&a.ID, &a.OrganizationID, &a.Name, &a.Description, &a.CreatedAt); err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("Scan error: %v", err))
			return
		}
		apps = append(apps, a)
	}

	respondJSON(w, http.StatusOK, apps)
}

func (h *Handler) CreateApplication(w http.ResponseWriter, r *http.Request) {
	var req models.CreateApplicationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.OrganizationID == "" {
		respondError(w, http.StatusBadRequest, "Invalid request body: name and organization_id are required")
		return
	}

	id := "app_" + uuid.New().String()[:8]
	now := time.Now().UTC()

	_, err := h.db.ExecContext(r.Context(), `
		INSERT INTO applications (id, organization_id, name, description, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, id, req.OrganizationID, req.Name, req.Description, now)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create application: %v", err))
		return
	}

	respondJSON(w, http.StatusCreated, models.Application{
		ID:             id,
		OrganizationID: req.OrganizationID,
		Name:           req.Name,
		Description:    req.Description,
		CreatedAt:      now,
	})
}
