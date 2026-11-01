package handlers

import (
	"net/http"
	"time"
)

// IngestionPoint is one minute of processed-event throughput.
type IngestionPoint struct {
	Minute time.Time `json:"minute"`
	Events int64     `json:"events"`
}

// IngestionRate returns events processed per minute for the last 60 minutes.
//
// GET /v1/analytics/monitoring/ingestion-rate
//
// Minutes with no events are returned with 0 so the chart has no gaps. The
// counter is platform-wide (it is not kept per tenant).
func (h *Handler) IngestionRate(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT m.minute, COALESCE(o.event_count, 0)
		FROM generate_series(
		        date_trunc('minute', NOW()) - INTERVAL '59 minutes',
		        date_trunc('minute', NOW()),
		        INTERVAL '1 minute') AS m(minute)
		LEFT JOIN op_ingestion_rate o ON o.bucket = m.minute
		ORDER BY m.minute`)
	if err != nil {
		queryFailed(w, r, err)
		return
	}
	defer rows.Close()

	data := make([]IngestionPoint, 0, 60)
	var total int64
	for rows.Next() {
		var p IngestionPoint
		if err := rows.Scan(&p.Minute, &p.Events); err != nil {
			queryFailed(w, r, err)
			return
		}
		total += p.Events
		data = append(data, p)
	}
	if err := rows.Err(); err != nil {
		queryFailed(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{"data": data, "total_last_hour": total})
}

// ─── Monitoring detail ───────────────────────────────────────────────────────

type EventTypeCount struct {
	EventType string `json:"event_type"`
	Count     int64  `json:"count"`
}

type AgentLag struct {
	AgentID       string     `json:"agent_id"`
	AgentName     string     `json:"agent_name"`
	Application   string     `json:"application"`
	ConfirmedLSN  string     `json:"confirmed_lsn"`
	LastLSN       string     `json:"last_lsn"`
	LastSeenAt    *time.Time `json:"last_seen_at,omitempty"`
	LastProcessed *time.Time `json:"last_processed_at,omitempty"`
	LagSeconds    *float64   `json:"lag_seconds,omitempty"`
	Status        string     `json:"status"`
}

type DeadLetterTenant struct {
	TenantID      string    `json:"tenant_id"`
	Unresolved    int64     `json:"unresolved"`
	LatestReason  string    `json:"latest_reason"`
	LatestFailure time.Time `json:"latest_failure_at"`
}

type SchemaVersionStat struct {
	SchemaVersion int       `json:"schema_version"`
	Events        int64     `json:"events"`
	FirstSeen     time.Time `json:"first_seen"`
	LastSeen      time.Time `json:"last_seen"`
}

// schemaVersionWindow bounds the schema-version scan. raw_events is the
// largest table, and a GROUP BY over all of history would scan every row.
const schemaVersionWindow = 24 * time.Hour

// MonitoringDetail returns the monitoring page's breakdown panels.
//
// GET /v1/analytics/monitoring/detail[?tenant_id=]
//
// With a tenant (always the case for customer admins, enforced by the control
// plane) every figure is limited to that tenant and its organization's agents.
// Without one it is platform-wide, for operators.
func (h *Handler) MonitoringDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenant := h.explicitTenant(r) // "" => platform-wide
	var tenantArg interface{}
	if tenant != "" {
		tenantArg = tenant
	}
	now := time.Now().UTC()

	out := map[string]interface{}{"tenant_id": tenant}

	// Event types received in the last 5 minutes.
	types := make([]EventTypeCount, 0)
	rows, err := h.db.QueryContext(ctx, `
		SELECT event_type, COUNT(*) FROM raw_events
		WHERE received_at >= $1 AND ($2::text IS NULL OR tenant_id = $2)
		GROUP BY event_type ORDER BY COUNT(*) DESC, event_type`, now.Add(-5*time.Minute), tenantArg)
	if err != nil {
		queryFailed(w, r, err)
		return
	}
	for rows.Next() {
		var t EventTypeCount
		if err := rows.Scan(&t.EventType, &t.Count); err != nil {
			rows.Close()
			queryFailed(w, r, err)
			return
		}
		types = append(types, t)
	}
	rows.Close()
	out["event_types_5m"] = types

	// Schema versions seen recently (version skew during a rolling deploy).
	versions := make([]SchemaVersionStat, 0)
	rows, err = h.db.QueryContext(ctx, `
		SELECT schema_version, COUNT(*), MIN(received_at), MAX(received_at) FROM raw_events
		WHERE received_at >= $1 AND ($2::text IS NULL OR tenant_id = $2)
		GROUP BY schema_version ORDER BY schema_version`, now.Add(-schemaVersionWindow), tenantArg)
	if err != nil {
		queryFailed(w, r, err)
		return
	}
	for rows.Next() {
		var v SchemaVersionStat
		if err := rows.Scan(&v.SchemaVersion, &v.Events, &v.FirstSeen, &v.LastSeen); err != nil {
			rows.Close()
			queryFailed(w, r, err)
			return
		}
		versions = append(versions, v)
	}
	rows.Close()
	out["schema_versions_24h"] = versions

	// Unresolved dead-letter events, grouped by tenant.
	dead := make([]DeadLetterTenant, 0)
	rows, err = h.db.QueryContext(ctx, `
		SELECT tenant_id, COUNT(*),
		       (ARRAY_AGG(reason ORDER BY created_at DESC))[1], MAX(created_at)
		FROM dead_letter_events
		WHERE replayed_at IS NULL AND ($1::text IS NULL OR tenant_id = $1)
		GROUP BY tenant_id ORDER BY COUNT(*) DESC, tenant_id LIMIT 50`, tenantArg)
	if err != nil {
		queryFailed(w, r, err)
		return
	}
	for rows.Next() {
		var d DeadLetterTenant
		if err := rows.Scan(&d.TenantID, &d.Unresolved, &d.LatestReason, &d.LatestFailure); err != nil {
			rows.Close()
			queryFailed(w, r, err)
			return
		}
		if d.TenantID == "" {
			d.TenantID = "(unresolved)"
		}
		dead = append(dead, d)
	}
	rows.Close()
	out["dead_letter_by_tenant"] = dead

	// Per-agent replication lag. An agent belongs to the organization that owns
	// its application, so a tenant filter selects that organization's agents.
	agents := make([]AgentLag, 0)
	rows, err = h.db.QueryContext(ctx, `
		SELECT g.id, g.name, a.name, COALESCE(c.confirmed_lsn, '0/0'), COALESCE(w.last_lsn, '0/0'),
		       g.last_seen_at, e.processed_at, e.delay_seconds,
		       CASE WHEN g.status = 'error' THEN 'error'
		            WHEN g.last_seen_at IS NULL OR g.last_seen_at < NOW() - INTERVAL '5 minutes' THEN 'disconnected'
		            ELSE g.status END
		FROM agents g
		JOIN applications a ON a.id = g.application_id
		LEFT JOIN LATERAL (
			SELECT confirmed_lsn
			FROM agent_cursors
			WHERE agent_id = g.id
			ORDER BY updated_at DESC
			LIMIT 1
		) c ON TRUE
		LEFT JOIN processing_watermarks w ON w.agent_id = g.id
        LEFT JOIN LATERAL (
          SELECT p.processed_at, GREATEST(EXTRACT(EPOCH FROM (p.processed_at-r.occurred_at)),0) AS delay_seconds
          FROM raw_events r JOIN processed_events p ON p.event_id=r.event_id AND p.tenant_id=r.tenant_id
          WHERE r.agent_id=g.id AND p.status='processed' AND ($1::text IS NULL OR r.tenant_id=$1)
          ORDER BY r.id DESC LIMIT 1
        ) e ON TRUE
		WHERE ($1::text IS NULL OR a.organization_id = (SELECT organization_id FROM tenants WHERE id = $1))
		ORDER BY g.created_at, g.id LIMIT 200`, tenantArg)
	if err != nil {
		queryFailed(w, r, err)
		return
	}
	for rows.Next() {
		var a AgentLag
		if err := rows.Scan(&a.AgentID, &a.AgentName, &a.Application, &a.ConfirmedLSN, &a.LastLSN,
			&a.LastSeenAt, &a.LastProcessed, &a.LagSeconds, &a.Status); err != nil {
			rows.Close()
			queryFailed(w, r, err)
			return
		}
		if a.LagSeconds != nil {
			v := round1(*a.LagSeconds)
			a.LagSeconds = &v
		}
		agents = append(agents, a)
	}
	rows.Close()
	out["agents"] = agents

	respondJSON(w, http.StatusOK, out)
}

// ─── Tenant health (Overview) ────────────────────────────────────────────────

// alertThresholds are the plan's Overview alert levels.
const (
	alertPaymentFailurePct = 10.0
	alertReturnRatePct     = 15.0
	alertLagSeconds        = 5 * 60
)

type TenantAlert struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// TenantHealth is the Overview page's per-tenant summary.
type TenantHealth struct {
	TenantID          string        `json:"tenant_id"`
	ActivityState     string        `json:"activity_state"`
	LastEventAt       *time.Time    `json:"last_event_at,omitempty"`
	LagSeconds        *float64      `json:"lag_seconds,omitempty"`
	DeadLetterCount   int64         `json:"dead_letter_count"`
	Revenue24h        float64       `json:"revenue_24h"`
	Orders24h         int64         `json:"orders_24h"`
	PaymentFailurePct float64       `json:"payment_failure_rate_pct"`
	ReturnRatePct     float64       `json:"return_rate_pct"`
	Alerts            []TenantAlert `json:"alerts"`
}

// TenantHealthHandler summarizes one tenant for the Overview page and raises
// alerts when it crosses a threshold.
//
// GET /v1/analytics/tenant/health
//
// Revenue and orders are for the current and previous UTC day (a rolling
// ~24-48h window, since the aggregates are day-bucketed). Payment failure and
// return rates cover the last 7 UTC days.
func (h *Handler) TenantHealthHandler(w http.ResponseWriter, r *http.Request) {
	tenantID := h.resolveRequestTenant(r)
	ctx := r.Context()
	today := startOfDay(time.Now())

	out := TenantHealth{TenantID: tenantID, Alerts: []TenantAlert{}}

	freshness, err := h.tenantFreshness(ctx, tenantID)
	if err != nil {
		queryFailed(w, r, err)
		return
	}
	out.LastEventAt = freshness.LastProcessedAt
	out.LagSeconds = freshness.LagSeconds
	out.ActivityState = freshness.ActivityState

	if err := h.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM dead_letter_events WHERE tenant_id = $1 AND replayed_at IS NULL`, tenantID).
		Scan(&out.DeadLetterCount); err != nil {
		queryFailed(w, r, err)
		return
	}

	if err := h.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(total_revenue), 0), COALESCE(SUM(active_orders), 0)::bigint
		FROM order_summary_daily WHERE tenant_id = $1 AND bucket >= $2::date AND bucket < $3::date`,
		tenantID, today.AddDate(0, 0, -1), today.AddDate(0, 0, 1)).Scan(&out.Revenue24h, &out.Orders24h); err != nil {
		queryFailed(w, r, err)
		return
	}
	out.Revenue24h = round2(out.Revenue24h)

	weekStart, weekEnd := today.AddDate(0, 0, -6), today.AddDate(0, 0, 1)
	var payTotal, payFailed int64
	if err := h.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(order_count), 0)::bigint,
		       COALESCE(SUM(order_count) FILTER (WHERE payment_status = 'failed'), 0)::bigint
		FROM payment_aggregates WHERE tenant_id = $1 AND bucket >= $2::date AND bucket < $3::date`,
		tenantID, weekStart, weekEnd).Scan(&payTotal, &payFailed); err != nil {
		queryFailed(w, r, err)
		return
	}
	out.PaymentFailurePct = pct(float64(payFailed), float64(payTotal))

	var returns, live int64
	if err := h.db.QueryRowContext(ctx, `
		SELECT COALESCE((SELECT SUM(return_count) FROM return_aggregates
		                 WHERE tenant_id = $1 AND bucket >= $2::date AND bucket < $3::date), 0)::bigint,
		       COALESCE((SELECT SUM(order_count) FROM fulfillment_aggregates
		                 WHERE tenant_id = $1 AND bucket >= $2::date AND bucket < $3::date
		                   AND fulfillment_status <> 'cancelled'), 0)::bigint`,
		tenantID, weekStart, weekEnd).Scan(&returns, &live); err != nil {
		queryFailed(w, r, err)
		return
	}
	out.ReturnRatePct = pct(float64(returns), float64(live))

	if out.PaymentFailurePct > alertPaymentFailurePct && payTotal >= 20 {
		out.Alerts = append(out.Alerts, TenantAlert{"payment_failures", "warning", "Payment failure rate above 10% in the last 7 days"})
	}
	if out.ReturnRatePct > alertReturnRatePct && live >= 20 {
		out.Alerts = append(out.Alerts, TenantAlert{"high_returns", "warning", "Return rate above 15% in the last 7 days"})
	}
	if out.ActivityState == "delayed" {
		out.Alerts = append(out.Alerts, TenantAlert{"processing_delay", "warning", "The latest event took more than 5 minutes to reach analytics"})
	}
	if out.DeadLetterCount > 0 {
		out.Alerts = append(out.Alerts, TenantAlert{"dead_letter", "error", "Unresolved failed events in the dead-letter queue"})
	}
	respondJSON(w, http.StatusOK, out)
}
