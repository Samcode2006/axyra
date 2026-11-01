package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"time"
)

// MonitoringStats is the monitoring API response (B-5).
type MonitoringStats struct {
	// Queue
	QueueDepth int64 `json:"queue_depth"`

	// Ingestion rate: events received in the last 60 seconds
	EventsLast60s int64 `json:"events_last_60s"`
	// Ingestion rate: events received in the last 5 minutes
	EventsLast5m int64 `json:"events_last_5m"`

	// Worker health: last successfully processed event timestamp
	LastProcessedAt *time.Time `json:"last_processed_at,omitempty"`
	WorkerHealthy   bool       `json:"worker_healthy"`
	WorkerState     string     `json:"worker_state"`

	// Failed / dead-letter events
	DeadLetterCount int64 `json:"dead_letter_count"`

	// Processed event counts (last 5 minutes)
	ProcessedLast5m int64 `json:"processed_last_5m"`

	// Query latency percentiles (ms) across all tenants, last 5 minutes
	LatencyP50   *float64 `json:"latency_p50_ms,omitempty"`
	LatencyP95   *float64 `json:"latency_p95_ms,omitempty"`
	LatencyP99   *float64 `json:"latency_p99_ms,omitempty"`
	LatencyCount int64    `json:"latency_sample_count"`
}

// MonitoringHandler returns platform-wide operational metrics for the Monitoring page.
// GET /v1/analytics/monitoring
func (h *Handler) MonitoringHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now().UTC()
	ago60s := now.Add(-60 * time.Second)
	ago5m := now.Add(-5 * time.Minute)
	// Worker is considered healthy if it processed something in the last 5 minutes.
	agoHealthy := now.Add(-5 * time.Minute)

	var stats MonitoringStats

	// With a tenant named (always the case for customer admins) every figure is
	// limited to it; without one the view is platform-wide, for operators.
	var tenantArg interface{}
	if t := h.explicitTenant(r); t != "" {
		tenantArg = t
	}

	// Queue depth from Redis (best-effort; 0 on error or memory queue).
	var queueKnown bool
	stats.QueueDepth, queueKnown = h.queueDepth(ctx)

	// Ingestion rate from raw_events received timestamp.
	_ = h.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM raw_events WHERE received_at >= $1 AND ($2::text IS NULL OR tenant_id = $2)`, ago60s, tenantArg).
		Scan(&stats.EventsLast60s)
	_ = h.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM raw_events WHERE received_at >= $1 AND ($2::text IS NULL OR tenant_id = $2)`, ago5m, tenantArg).
		Scan(&stats.EventsLast5m)

	// Processed events in last 5 minutes.
	_ = h.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM processed_events WHERE processed_at >= $1 AND status = 'processed' AND ($2::text IS NULL OR tenant_id = $2)`, ago5m, tenantArg).
		Scan(&stats.ProcessedLast5m)

	// Completed events measure actual processing time, not source event age.
	var lastAt sql.NullTime
	err := h.db.QueryRowContext(ctx, `SELECT MAX(processed_at) FROM processed_events WHERE status='processed' AND ($1::text IS NULL OR tenant_id=$1)`, tenantArg).Scan(&lastAt)
	stats.WorkerState = "unknown"
	if err == nil {
		if lastAt.Valid {
			stats.LastProcessedAt = &lastAt.Time
		}
		if lastAt.Valid && lastAt.Time.After(agoHealthy) {
			stats.WorkerState = "processing"
			stats.WorkerHealthy = true
		} else if queueKnown && stats.QueueDepth == 0 {
			stats.WorkerState = "idle"
			stats.WorkerHealthy = true
		} else if queueKnown && tenantArg == nil && stats.QueueDepth > 0 {
			stats.WorkerState = "stalled"
		}
	}

	// Dead-letter count.
	_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dead_letter_events WHERE replayed_at IS NULL AND ($1::text IS NULL OR tenant_id = $1)`, tenantArg).
		Scan(&stats.DeadLetterCount)

	// Query latency percentiles from op_query_latency (last 5 minutes).
	type latencyRow struct {
		p50, p95, p99 *float64
		count         int64
	}
	var lr latencyRow
	perr := h.db.QueryRowContext(ctx, `
		SELECT
			PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY duration_ms),
			PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY duration_ms),
			PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY duration_ms),
			COUNT(*)
		FROM op_query_latency
		WHERE timing_kind = 'db' AND recorded_at >= $1 AND ($2::text IS NULL OR tenant_id = $2)
	`, ago5m, tenantArg).Scan(&lr.p50, &lr.p95, &lr.p99, &lr.count)
	if perr == nil {
		stats.LatencyP50 = lr.p50
		stats.LatencyP95 = lr.p95
		stats.LatencyP99 = lr.p99
		stats.LatencyCount = lr.count
	}

	respondJSON(w, http.StatusOK, stats)
}

// queueDepth returns the number of events waiting in the Redis queue, or 0 on error.
func (h *Handler) queueDepth(ctx context.Context) (int64, bool) {
	// The queue interface doesn't expose depth; call the Redis LLEN directly
	// through the typed queue if it supports it, otherwise return 0.
	type depthed interface {
		Depth(ctx context.Context) (int64, error)
	}
	if d, ok := h.queue.(depthed); ok {
		n, err := d.Depth(ctx)
		if err == nil {
			return n, true
		}
	}
	return 0, false
}
