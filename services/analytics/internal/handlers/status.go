package handlers

import (
	"context"
	"database/sql"
	"github.com/axyra/analytics/internal/models"
	"net/http"
	"time"
)

const maxHealthyLagSeconds = 5 * 60

// This describes the last completed event, not the age of a queued event.
// No recent activity is idle, not evidence that ingestion has failed.
func eventActivity(last *time.Time, delay *float64, now time.Time) string {
	if last == nil {
		return "waiting"
	}
	if now.Sub(*last) >= 5*time.Minute {
		return "idle"
	}
	if delay != nil && *delay >= maxHealthyLagSeconds {
		return "delayed"
	}
	return "active"
}

func (h *Handler) tenantFreshness(ctx context.Context, tenantID string) (models.AnalyticsStatus, error) {
	out := models.AnalyticsStatus{TenantID: tenantID, LastLSN: "0/0", ActivityState: "waiting", IsHealthy: true}
	var at time.Time
	var delay float64
	err := h.db.QueryRowContext(ctx, `
  SELECT r.lsn, p.processed_at,
         GREATEST(EXTRACT(EPOCH FROM (p.processed_at-r.occurred_at)),0)
  FROM raw_events r JOIN processed_events p ON p.event_id=r.event_id AND p.tenant_id=r.tenant_id
  WHERE r.tenant_id IN (SELECT analytics_scope_tenants($1)) AND p.status='processed'
  ORDER BY r.id DESC LIMIT 1`, tenantID).Scan(&out.LastLSN, &at, &delay)
	if err == sql.ErrNoRows {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.LastProcessedAt = &at
	out.LagSeconds = &delay
	out.ActivityState = eventActivity(&at, &delay, time.Now())
	out.IsHealthy = out.ActivityState != "delayed"
	return out, nil
}

func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	out, err := h.tenantFreshness(r.Context(), h.resolveRequestTenant(r))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to query freshness")
		return
	}
	respondJSON(w, http.StatusOK, out)
}
