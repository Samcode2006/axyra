package handlers

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/axyra/analytics/internal/models"
)

// maxHealthyLagSeconds is the lag threshold above which the pipeline is
// considered unhealthy. 5 minutes matches the PRD "good" freshness target.
const maxHealthyLagSeconds = 5 * 60

func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	tenantID := h.resolveRequestTenant(r)

	// Fetch the most recent processing watermark for this tenant.
	var lastLSN string
	var lastProcessedAt time.Time

	err := h.db.QueryRowContext(r.Context(), `
		SELECT last_lsn, last_processed_at
		FROM processing_watermarks
		WHERE tenant_id = $1
		ORDER BY last_processed_at DESC
		LIMIT 1
	`, tenantID).Scan(&lastLSN, &lastProcessedAt)
	if err != nil && err != sql.ErrNoRows {
		respondError(w, http.StatusInternalServerError, "Failed to query watermark")
		return
	}
	if err == sql.ErrNoRows || lastProcessedAt.IsZero() {
		// No data processed yet — report a zero state rather than a server error.
		respondJSON(w, http.StatusOK, models.AnalyticsStatus{
			TenantID:        tenantID,
			LastProcessedAt: time.Now().UTC(),
			LastLSN:         "0/0",
			LagSeconds:      0,
			IsHealthy:       true,
		})
		return
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
		IsHealthy:       lagSeconds < maxHealthyLagSeconds,
	})
}
