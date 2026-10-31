package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/axyra/control-plane/internal/models"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *Handler) ListAgents(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, application_id, name, status, last_seen_at, created_at 
		FROM agents 
		ORDER BY created_at ASC
	`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Query error: %v", err))
		return
	}
	defer rows.Close()

	agentsList := make([]models.Agent, 0)
	for rows.Next() {
		var a models.Agent
		if err := rows.Scan(&a.ID, &a.ApplicationID, &a.Name, &a.Status, &a.LastSeenAt, &a.CreatedAt); err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("Scan error: %v", err))
			return
		}
		agentsList = append(agentsList, a)
	}

	respondJSON(w, http.StatusOK, agentsList)
}

func (h *Handler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	var req models.CreateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.ApplicationID == "" {
		respondError(w, http.StatusBadRequest, "Invalid request body: name and application_id are required")
		return
	}

	id := "agent_" + uuid.New().String()[:8]
	now := time.Now().UTC()

	_, err := h.db.ExecContext(r.Context(), `
		INSERT INTO agents (id, application_id, name, status, created_at)
		VALUES ($1, $2, $3, 'disconnected', $4)
	`, id, req.ApplicationID, req.Name, now)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create agent: %v", err))
		return
	}

	respondJSON(w, http.StatusCreated, models.Agent{
		ID:            id,
		ApplicationID: req.ApplicationID,
		Name:          req.Name,
		Status:        "disconnected",
		CreatedAt:     now,
	})
}

func (h *Handler) IssueAgentToken(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agent_id")
	if agentID == "" {
		respondError(w, http.StatusBadRequest, "agent_id is required")
		return
	}

	// Verify agent exists
	var exists bool
	err := h.db.QueryRowContext(r.Context(), "SELECT EXISTS(SELECT 1 FROM agents WHERE id = $1)", agentID).Scan(&exists)
	if err != nil || !exists {
		respondError(w, http.StatusNotFound, "Agent not found")
		return
	}

	tokenID := "tok_" + uuid.New().String()[:8]
	randBytes := make([]byte, 24)
	_, _ = rand.Read(randBytes)
	tokenValue := "axyra_agt_" + hex.EncodeToString(randBytes)

	now := time.Now().UTC()
	tokenName := fmt.Sprintf("Token created at %s", now.Format(time.RFC3339))

	_, err = h.db.ExecContext(r.Context(), `
		INSERT INTO agent_tokens (id, agent_id, token, name, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, tokenID, agentID, tokenValue, tokenName, now)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to issue token: %v", err))
		return
	}

	respondJSON(w, http.StatusCreated, models.IssueTokenResponse{
		TokenID: tokenID,
		Token:   tokenValue,
	})
}

func (h *Handler) RevokeAgentToken(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agent_id")
	tokenID := chi.URLParam(r, "token_id")

	res, err := h.db.ExecContext(r.Context(), `
		UPDATE agent_tokens 
		SET revoked_at = NOW() 
		WHERE id = $1 AND agent_id = $2 AND revoked_at IS NULL
	`, tokenID, agentID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to revoke token: %v", err))
		return
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		respondError(w, http.StatusNotFound, "Active token not found")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (h *Handler) ListTenantMappings(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agent_id")

	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, agent_id, external_tenant_id, tenant_id, created_at 
		FROM tenant_mappings 
		WHERE agent_id = $1
		ORDER BY created_at ASC
	`, agentID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Query error: %v", err))
		return
	}
	defer rows.Close()

	mappings := make([]models.TenantMapping, 0)
	for rows.Next() {
		var m models.TenantMapping
		if err := rows.Scan(&m.ID, &m.AgentID, &m.ExternalTenantID, &m.TenantID, &m.CreatedAt); err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("Scan error: %v", err))
			return
		}
		mappings = append(mappings, m)
	}

	respondJSON(w, http.StatusOK, mappings)
}

func (h *Handler) CreateTenantMapping(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agent_id")
	var req models.CreateTenantMappingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ExternalTenantID == "" || req.TenantID == "" {
		respondError(w, http.StatusBadRequest, "Invalid request body: external_tenant_id and tenant_id are required")
		return
	}

	id := "map_" + uuid.New().String()[:8]
	now := time.Now().UTC()

	_, err := h.db.ExecContext(r.Context(), `
		INSERT INTO tenant_mappings (id, agent_id, external_tenant_id, tenant_id, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (agent_id, external_tenant_id) 
		DO UPDATE SET tenant_id = EXCLUDED.tenant_id
	`, id, agentID, req.ExternalTenantID, req.TenantID, now)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create mapping: %v", err))
		return
	}

	respondJSON(w, http.StatusCreated, models.TenantMapping{
		ID:               id,
		AgentID:          agentID,
		ExternalTenantID: req.ExternalTenantID,
		TenantID:         req.TenantID,
		CreatedAt:        now,
	})
}
