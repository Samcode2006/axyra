package handlers

import "net/http"

// ─── Geography ───────────────────────────────────────────────────────────────

// GeoItem is one row of the geographic breakdown.
type GeoItem struct {
	Group         string  `json:"group"`
	Country       string  `json:"country,omitempty"`        // set for state and city rows
	StateProvince string  `json:"state_province,omitempty"` // set for city rows
	OrderCount    int64   `json:"order_count"`
	TotalRevenue  float64 `json:"total_revenue"`
	SharePct      float64 `json:"share_pct"` // share of revenue
}

// GeoBreakdown groups revenue by country, state or city.
//
// GET /v1/analytics/geo/breakdown?from=&to=&group_by=country|state|city[&limit=50]
//
// States are grouped within their country and cities within their state, so
// two places with the same name in different parents stay separate.
func (h *Handler) GeoBreakdown(w http.ResponseWriter, r *http.Request) {
	tenantID := h.resolveRequestTenant(r)
	done, ok := h.acquireQuery(w, r, tenantID, "geo.breakdown")
	if !ok {
		return
	}
	defer done()

	win, ok := parseWindow(w, r)
	if !ok {
		return
	}
	groupBy := r.URL.Query().Get("group_by")
	if groupBy != "state" && groupBy != "city" {
		groupBy = "country"
	}
	limit := intParam(r, "limit", 50, 1, 500)

	// Column lists come from this fixed switch, never from the request.
	var cols string
	switch groupBy {
	case "state":
		cols = "country, state_province, ''::text"
	case "city":
		cols = "country, state_province, city"
	default:
		cols = "country, ''::text, ''::text"
	}
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT `+cols+`, SUM(order_count)::bigint, SUM(total_revenue)
		FROM geo_aggregates
		WHERE tenant_id IN (SELECT analytics_scope_tenants($1)) AND bucket >= $2::date AND bucket < $3::date
		GROUP BY 1, 2, 3
		HAVING SUM(order_count) <> 0
		ORDER BY SUM(total_revenue) DESC, 1, 2, 3
		LIMIT $4`, tenantID, win.Start, win.End, limit)
	if err != nil {
		queryFailed(w, r, err)
		return
	}
	defer rows.Close()

	data := make([]GeoItem, 0)
	var total float64
	for rows.Next() {
		var country, state, city string
		var it GeoItem
		if err := rows.Scan(&country, &state, &city, &it.OrderCount, &it.TotalRevenue); err != nil {
			queryFailed(w, r, err)
			return
		}
		switch groupBy {
		case "country":
			it.Group = country
		case "state":
			it.Group, it.Country = orUnknown(state), country
		case "city":
			it.Group, it.Country, it.StateProvince = orUnknown(city), country, state
		}
		total += it.TotalRevenue
		data = append(data, it)
	}
	if err := rows.Err(); err != nil {
		queryFailed(w, r, err)
		return
	}
	// Shares are of the revenue of the whole range, not just the rows returned.
	var all float64
	if err := h.db.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM(total_revenue), 0) FROM geo_aggregates
		WHERE tenant_id IN (SELECT analytics_scope_tenants($1)) AND bucket >= $2::date AND bucket < $3::date`,
		tenantID, win.Start, win.End).Scan(&all); err != nil {
		queryFailed(w, r, err)
		return
	}
	for i := range data {
		data[i].SharePct = pct(data[i].TotalRevenue, all)
		data[i].TotalRevenue = round2(data[i].TotalRevenue)
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{"tenant_id": tenantID, "group_by": groupBy, "data": data})
}

// ─── Channels ────────────────────────────────────────────────────────────────

// ChannelItem is one channel/referrer combination.
type ChannelItem struct {
	Channel        string  `json:"channel"`
	ReferrerSource string  `json:"referrer_source"`
	OrderCount     int64   `json:"order_count"`
	TotalRevenue   float64 `json:"total_revenue"`
	NewCustomers   int64   `json:"new_customers"`
}

// ChannelBreakdown attributes orders to acquisition channel and referrer.
//
// GET /v1/analytics/channels/breakdown?from=&to=
func (h *Handler) ChannelBreakdown(w http.ResponseWriter, r *http.Request) {
	tenantID := h.resolveRequestTenant(r)
	done, ok := h.acquireQuery(w, r, tenantID, "channels.breakdown")
	if !ok {
		return
	}
	defer done()

	win, ok := parseWindow(w, r)
	if !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT channel, referrer_source, SUM(order_count)::bigint, SUM(total_revenue), SUM(new_customers)::bigint
		FROM channel_aggregates
		WHERE tenant_id IN (SELECT analytics_scope_tenants($1)) AND bucket >= $2::date AND bucket < $3::date
		GROUP BY channel, referrer_source
		HAVING SUM(order_count) <> 0
		ORDER BY SUM(total_revenue) DESC, channel, referrer_source`, tenantID, win.Start, win.End)
	if err != nil {
		queryFailed(w, r, err)
		return
	}
	defer rows.Close()

	data := make([]ChannelItem, 0)
	for rows.Next() {
		var c ChannelItem
		if err := rows.Scan(&c.Channel, &c.ReferrerSource, &c.OrderCount, &c.TotalRevenue, &c.NewCustomers); err != nil {
			queryFailed(w, r, err)
			return
		}
		c.TotalRevenue = round2(c.TotalRevenue)
		data = append(data, c)
	}
	if err := rows.Err(); err != nil {
		queryFailed(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{"tenant_id": tenantID, "data": data})
}

// ─── Payments ────────────────────────────────────────────────────────────────

// PaymentItem is the payment health of one method.
type PaymentItem struct {
	PaymentMethod    string  `json:"payment_method"`
	TotalOrders      int64   `json:"total_orders"`
	PaidOrders       int64   `json:"paid_orders"`
	PendingOrders    int64   `json:"pending_orders"`
	FailedOrders     int64   `json:"failed_orders"`
	RefundedOrders   int64   `json:"refunded_orders"`
	ChargebackOrders int64   `json:"chargeback_orders"`
	TotalAmount      float64 `json:"total_amount"`
	FailureRatePct   float64 `json:"failure_rate_pct"`
}

// PaymentBreakdown reports payment outcomes per method.
//
// GET /v1/analytics/payments/breakdown?from=&to=
//
// Every order counts here, cancelled ones included: a failed payment is often
// exactly what led to the cancellation.
func (h *Handler) PaymentBreakdown(w http.ResponseWriter, r *http.Request) {
	tenantID := h.resolveRequestTenant(r)
	done, ok := h.acquireQuery(w, r, tenantID, "payments.breakdown")
	if !ok {
		return
	}
	defer done()

	win, ok := parseWindow(w, r)
	if !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT payment_method,
		       SUM(order_count)::bigint,
		       COALESCE(SUM(order_count) FILTER (WHERE payment_status = 'paid'), 0)::bigint,
		       COALESCE(SUM(order_count) FILTER (WHERE payment_status = 'pending'), 0)::bigint,
		       COALESCE(SUM(order_count) FILTER (WHERE payment_status = 'failed'), 0)::bigint,
		       COALESCE(SUM(order_count) FILTER (WHERE payment_status = 'refunded'), 0)::bigint,
		       COALESCE(SUM(order_count) FILTER (WHERE payment_status = 'chargeback'), 0)::bigint,
		       SUM(total_amount)
		FROM payment_aggregates
		WHERE tenant_id IN (SELECT analytics_scope_tenants($1)) AND bucket >= $2::date AND bucket < $3::date
		GROUP BY payment_method
		HAVING SUM(order_count) <> 0
		ORDER BY SUM(order_count) DESC, payment_method`, tenantID, win.Start, win.End)
	if err != nil {
		queryFailed(w, r, err)
		return
	}
	defer rows.Close()

	data := make([]PaymentItem, 0)
	for rows.Next() {
		var p PaymentItem
		if err := rows.Scan(&p.PaymentMethod, &p.TotalOrders, &p.PaidOrders, &p.PendingOrders, &p.FailedOrders,
			&p.RefundedOrders, &p.ChargebackOrders, &p.TotalAmount); err != nil {
			queryFailed(w, r, err)
			return
		}
		p.FailureRatePct = pct(float64(p.FailedOrders), float64(p.TotalOrders))
		p.TotalAmount = round2(p.TotalAmount)
		data = append(data, p)
	}
	if err := rows.Err(); err != nil {
		queryFailed(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{"tenant_id": tenantID, "data": data})
}

// ─── Returns ─────────────────────────────────────────────────────────────────

// ReturnReasonItem is one row of the top return reasons.
type ReturnReasonItem struct {
	Reason   string  `json:"reason"`
	Count    int64   `json:"count"`
	SharePct float64 `json:"share_pct"`
}

// ReturnsSummary reports return volume, refunds and reasons.
//
// GET /v1/analytics/returns/summary?from=&to=
//
// return_rate_pct is returns divided by non-cancelled orders created in the
// same range (both counted as of today, by order creation day).
// total_refund_amount is the order amount less any discount; tax and shipping
// are not included.
func (h *Handler) ReturnsSummary(w http.ResponseWriter, r *http.Request) {
	tenantID := h.resolveRequestTenant(r)
	done, ok := h.acquireQuery(w, r, tenantID, "returns.summary")
	if !ok {
		return
	}
	defer done()

	win, ok := parseWindow(w, r)
	if !ok {
		return
	}

	var totalReturns int64
	var refund float64
	if err := h.db.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM(return_count), 0)::bigint, COALESCE(SUM(refund_amount), 0)
		FROM return_aggregates
		WHERE tenant_id IN (SELECT analytics_scope_tenants($1)) AND bucket >= $2::date AND bucket < $3::date`,
		tenantID, win.Start, win.End).Scan(&totalReturns, &refund); err != nil {
		queryFailed(w, r, err)
		return
	}
	var orders int64
	if err := h.db.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM(order_count), 0)::bigint
		FROM fulfillment_aggregates
		WHERE tenant_id IN (SELECT analytics_scope_tenants($1)) AND bucket >= $2::date AND bucket < $3::date AND fulfillment_status <> 'cancelled'`,
		tenantID, win.Start, win.End).Scan(&orders); err != nil {
		queryFailed(w, r, err)
		return
	}

	rows, err := h.db.QueryContext(r.Context(), `
		SELECT return_reason, SUM(return_count)::bigint
		FROM return_aggregates
		WHERE tenant_id IN (SELECT analytics_scope_tenants($1)) AND bucket >= $2::date AND bucket < $3::date
		GROUP BY return_reason
		HAVING SUM(return_count) > 0
		ORDER BY SUM(return_count) DESC, return_reason
		LIMIT 5`, tenantID, win.Start, win.End)
	if err != nil {
		queryFailed(w, r, err)
		return
	}
	defer rows.Close()
	reasons := make([]ReturnReasonItem, 0, 5)
	for rows.Next() {
		var it ReturnReasonItem
		if err := rows.Scan(&it.Reason, &it.Count); err != nil {
			queryFailed(w, r, err)
			return
		}
		if it.Reason == "" {
			it.Reason = "unspecified"
		}
		it.SharePct = pct(float64(it.Count), float64(totalReturns))
		reasons = append(reasons, it)
	}
	if err := rows.Err(); err != nil {
		queryFailed(w, r, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"tenant_id":           tenantID,
		"total_returns":       totalReturns,
		"total_refund_amount": round2(refund),
		"return_rate_pct":     pct(float64(totalReturns), float64(orders)),
		"top_reasons":         reasons,
	})
}

// ─── Fulfilment funnel ───────────────────────────────────────────────────────

// FunnelStage is one stage of the fulfilment funnel.
type FunnelStage struct {
	Stage      string  `json:"stage"`
	OrderCount int64   `json:"order_count"`
	PctOfTotal float64 `json:"pct_of_total"`
}

// FulfillmentFunnel shows how many orders reached each stage.
//
// GET /v1/analytics/fulfillment/funnel?from=&to=
//
// Each stage counts the orders that reached it *or later*; a delivered order
// has also passed pending, processing and shipped, and a returned order was
// delivered first. Cancelled orders are not part of the funnel. pct_of_total
// is relative to the "pending" stage (every non-cancelled order). The
// "returned" stage is the orders now in the returned state.
func (h *Handler) FulfillmentFunnel(w http.ResponseWriter, r *http.Request) {
	tenantID := h.resolveRequestTenant(r)
	done, ok := h.acquireQuery(w, r, tenantID, "fulfillment.funnel")
	if !ok {
		return
	}
	defer done()

	win, ok := parseWindow(w, r)
	if !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT fulfillment_status, SUM(order_count)::bigint
		FROM fulfillment_aggregates
		WHERE tenant_id IN (SELECT analytics_scope_tenants($1)) AND bucket >= $2::date AND bucket < $3::date
		GROUP BY fulfillment_status`, tenantID, win.Start, win.End)
	if err != nil {
		queryFailed(w, r, err)
		return
	}
	defer rows.Close()
	now := map[string]int64{}
	for rows.Next() {
		var s string
		var n int64
		if err := rows.Scan(&s, &n); err != nil {
			queryFailed(w, r, err)
			return
		}
		now[s] = n
	}
	if err := rows.Err(); err != nil {
		queryFailed(w, r, err)
		return
	}

	// "Reached at least" counts, from the latest stage backwards.
	delivered := now["delivered"] + now["returned"]
	shipped := delivered + now["shipped"]
	processing := shipped + now["processing"]
	pending := processing + now["pending"]
	stages := []FunnelStage{
		{Stage: "pending", OrderCount: pending},
		{Stage: "processing", OrderCount: processing},
		{Stage: "shipped", OrderCount: shipped},
		{Stage: "delivered", OrderCount: delivered},
		{Stage: "returned", OrderCount: now["returned"]},
	}
	for i := range stages {
		stages[i].PctOfTotal = pct(float64(stages[i].OrderCount), float64(pending))
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{"tenant_id": tenantID, "data": stages})
}

// ─── Discounts ───────────────────────────────────────────────────────────────

// PromoCode is one row of the top promo codes.
type PromoCode struct {
	Code          string  `json:"code"`
	Uses          int64   `json:"uses"`
	TotalDiscount float64 `json:"total_discount"`
}

// DiscountSummary reports promo usage.
//
// GET /v1/analytics/discounts/summary?from=&to=
//
// Non-cancelled orders only. discount_rate_pct is orders that used a code
// divided by all non-cancelled orders created in the range.
func (h *Handler) DiscountSummary(w http.ResponseWriter, r *http.Request) {
	tenantID := h.resolveRequestTenant(r)
	done, ok := h.acquireQuery(w, r, tenantID, "discounts.summary")
	if !ok {
		return
	}
	defer done()

	win, ok := parseWindow(w, r)
	if !ok {
		return
	}

	var withDiscount int64
	var totalDiscount float64
	if err := h.db.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM(uses), 0)::bigint, COALESCE(SUM(total_discount), 0)
		FROM discount_aggregates
		WHERE tenant_id IN (SELECT analytics_scope_tenants($1)) AND bucket >= $2::date AND bucket < $3::date`,
		tenantID, win.Start, win.End).Scan(&withDiscount, &totalDiscount); err != nil {
		queryFailed(w, r, err)
		return
	}
	var orders int64
	if err := h.db.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM(order_count), 0)::bigint
		FROM fulfillment_aggregates
		WHERE tenant_id IN (SELECT analytics_scope_tenants($1)) AND bucket >= $2::date AND bucket < $3::date AND fulfillment_status <> 'cancelled'`,
		tenantID, win.Start, win.End).Scan(&orders); err != nil {
		queryFailed(w, r, err)
		return
	}

	rows, err := h.db.QueryContext(r.Context(), `
		SELECT discount_code, SUM(uses)::bigint, SUM(total_discount)
		FROM discount_aggregates
		WHERE tenant_id IN (SELECT analytics_scope_tenants($1)) AND bucket >= $2::date AND bucket < $3::date
		GROUP BY discount_code
		HAVING SUM(uses) > 0
		ORDER BY SUM(uses) DESC, SUM(total_discount) DESC, discount_code
		LIMIT 10`, tenantID, win.Start, win.End)
	if err != nil {
		queryFailed(w, r, err)
		return
	}
	defer rows.Close()
	codes := make([]PromoCode, 0, 10)
	for rows.Next() {
		var c PromoCode
		if err := rows.Scan(&c.Code, &c.Uses, &c.TotalDiscount); err != nil {
			queryFailed(w, r, err)
			return
		}
		c.TotalDiscount = round2(c.TotalDiscount)
		codes = append(codes, c)
	}
	if err := rows.Err(); err != nil {
		queryFailed(w, r, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"tenant_id":              tenantID,
		"orders_with_discount":   withDiscount,
		"discount_rate_pct":      pct(float64(withDiscount), float64(orders)),
		"total_discount_given":   round2(totalDiscount),
		"avg_discount_per_order": ratio(totalDiscount, float64(withDiscount)),
		"top_codes":              codes,
	})
}
