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

func (h *Handler) ListTenants(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, organization_id, external_tenant_id, name, status, max_events_per_day, max_concurrent_queries, created_at
		FROM tenants 
		ORDER BY created_at ASC
	`)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Query error: %v", err))
		return
	}
	defer rows.Close()

	tenantList := make([]models.Tenant, 0)
	for rows.Next() {
		var t models.Tenant
		var maxEvents int64
		var maxQueries int
		if err := rows.Scan(&t.ID, &t.OrganizationID, &t.ExternalTenantID, &t.Name, &t.Status, &maxEvents, &maxQueries, &t.CreatedAt); err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("Scan error: %v", err))
			return
		}
		t.Quota = &models.TenantQuota{
			MaxEventsPerDay:      maxEvents,
			MaxConcurrentQueries: maxQueries,
		}
		tenantList = append(tenantList, t)
	}

	respondJSON(w, http.StatusOK, models.PaginatedResponse[models.Tenant]{
		Data:    tenantList,
		Total:   len(tenantList),
		Page:    1,
		PerPage: 50,
	})
}

func (h *Handler) CreateTenant(w http.ResponseWriter, r *http.Request) {
	var req models.CreateTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ExternalTenantID == "" || req.Name == "" || req.OrganizationID == "" {
		respondError(w, http.StatusBadRequest, "Invalid request body: organization_id, external_tenant_id and name are required")
		return
	}

	id := "tenant_" + uuid.New().String()[:8]
	now := time.Now().UTC()

	_, err := h.db.ExecContext(r.Context(), `
		INSERT INTO tenants (id, organization_id, external_tenant_id, name, status, max_events_per_day, max_concurrent_queries, created_at)
		VALUES ($1, $2, $3, $4, 'active', 1000000, 10, $5)
	`, id, req.OrganizationID, req.ExternalTenantID, req.Name, now)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create tenant: %v", err))
		return
	}

	respondJSON(w, http.StatusCreated, models.Tenant{
		ID:               id,
		OrganizationID:   req.OrganizationID,
		ExternalTenantID: req.ExternalTenantID,
		Name:             req.Name,
		Status:           "active",
		Quota: &models.TenantQuota{
			MaxEventsPerDay:      1000000,
			MaxConcurrentQueries: 10,
		},
		CreatedAt: now,
	})
}

func (h *Handler) UpdateTenant(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "id")
	var req models.UpdateTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Status != nil {
		_, err := h.db.ExecContext(r.Context(), "UPDATE tenants SET status = $1 WHERE id = $2", *req.Status, tenantID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("Update status error: %v", err))
			return
		}
	}

	if req.Quota != nil {
		_, err := h.db.ExecContext(r.Context(), `
			UPDATE tenants 
			SET max_events_per_day = $1, max_concurrent_queries = $2 
			WHERE id = $3
		`, req.Quota.MaxEventsPerDay, req.Quota.MaxConcurrentQueries, tenantID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("Update quota error: %v", err))
			return
		}
	}

	// Fetch updated tenant
	var t models.Tenant
	var maxEvents int64
	var maxQueries int
	err := h.db.QueryRowContext(r.Context(), `
		SELECT id, organization_id, external_tenant_id, name, status, max_events_per_day, max_concurrent_queries, created_at
		FROM tenants 
		WHERE id = $1
	`, tenantID).Scan(&t.ID, &t.OrganizationID, &t.ExternalTenantID, &t.Name, &t.Status, &maxEvents, &maxQueries, &t.CreatedAt)
	if err != nil {
		respondError(w, http.StatusNotFound, "Tenant not found")
		return
	}
	t.Quota = &models.TenantQuota{
		MaxEventsPerDay:      maxEvents,
		MaxConcurrentQueries: maxQueries,
	}

	respondJSON(w, http.StatusOK, t)
}
