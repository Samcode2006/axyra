package handlers

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/axyra/analytics/internal/models"
)

func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	tenantID := h.resolveRequestTenant(r)

	var lastLSN string
	var lastProcessedAt time.Time

	err := h.db.QueryRowContext(r.Context(), `
		SELECT last_lsn, last_processed_at 
		FROM processing_watermarks 
		WHERE tenant_id = $1
		ORDER BY last_processed_at DESC
		LIMIT 1
	`, tenantID).Scan(&lastLSN, &lastProcessedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			lastLSN = "0/0"
			lastProcessedAt = time.Now().UTC()
		} else {
			lastLSN = "0/0"
			lastProcessedAt = time.Now().UTC()
		}
	}

	lagSeconds := time.Since(lastProcessedAt).Seconds()
	if lagSeconds < 0 {
		lagSeconds = 0
	}

	respondJSON(w, http.StatusOK, models.AnalyticsStatus{
		TenantID:        tenantID,
		LastProcessedAt: lastProcessedAt,
		LastLSN:         lastLSN,
		LagSeconds:      lagSeconds,
		IsHealthy:       true,
	})
}
