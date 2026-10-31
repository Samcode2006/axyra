package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/axyra/control-plane/internal/models"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *Handler) ListPolicies(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, tenant_id, name, type, config, created_at 
		FROM policies 
		ORDER BY created_at ASC
	`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Query error: %v", err))
		return
	}
	defer rows.Close()

	policiesList := make([]models.Policy, 0)
	for rows.Next() {
		var p models.Policy
		var rawCfg []byte
		if err := rows.Scan(&p.ID, &p.TenantID, &p.Name, &p.Type, &rawCfg, &p.CreatedAt); err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("Scan error: %v", err))
			return
		}
		p.Config = json.RawMessage(rawCfg)
		policiesList = append(policiesList, p)
	}

	respondJSON(w, http.StatusOK, policiesList)
}

func (h *Handler) CreatePolicy(w http.ResponseWriter, r *http.Request) {
	var req models.CreatePolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TenantID == "" || req.Name == "" || req.Type == "" {
		respondError(w, http.StatusBadRequest, "Invalid request body: tenant_id, name, and type are required")
		return
	}

	id := "pol_" + uuid.New().String()[:8]
	now := time.Now().UTC()
	cfg := req.Config
	if len(cfg) == 0 {
		cfg = json.RawMessage(`{}`)
	}

	_, err := h.db.ExecContext(r.Context(), `
		INSERT INTO policies (id, tenant_id, name, type, config, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, id, req.TenantID, req.Name, req.Type, string(cfg), now)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create policy: %v", err))
		return
	}

	respondJSON(w, http.StatusCreated, models.Policy{
		ID:        id,
		TenantID:  req.TenantID,
		Name:      req.Name,
		Type:      req.Type,
		Config:    cfg,
		CreatedAt: now,
	})
}

func (h *Handler) DeletePolicy(w http.ResponseWriter, r *http.Request) {
	policyID := chi.URLParam(r, "id")

	res, err := h.db.ExecContext(r.Context(), "DELETE FROM policies WHERE id = $1", policyID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Delete error: %v", err))
		return
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		respondError(w, http.StatusNotFound, "Policy not found")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
