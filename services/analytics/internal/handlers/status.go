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

	// The tenant's freshness is the time and LSN of its own latest event.
	//
	// processing_watermarks is not used: it holds one row per AGENT, and its
	// tenant_id is overwritten by whichever tenant's event arrived last. For an
	// agent serving several tenants most of them would have no row, and would be
	// reported healthy with LSN 0/0.
	var lastLSN string
	var lastProcessedAt time.Time

	err := h.db.QueryRowContext(r.Context(), `
		SELECT lsn, occurred_at
		FROM raw_events
		WHERE tenant_id = $1
		ORDER BY occurred_at DESC, id DESC
		LIMIT 1
	`, tenantID).Scan(&lastLSN, &lastProcessedAt)
	if err != nil && err != sql.ErrNoRows {
		respondError(w, http.StatusInternalServerError, "Failed to query freshness")
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
