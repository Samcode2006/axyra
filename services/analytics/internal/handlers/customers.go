package handlers

import (
	"net/http"
	"time"
)

// CustomerSummary is the body of GET /v1/analytics/customers/summary.
type CustomerSummary struct {
	TenantID             string    `json:"tenant_id"`
	From                 time.Time `json:"from"`
	To                   time.Time `json:"to"`
	TotalCustomers       int64     `json:"total_customers"`
	NewCustomers         int64     `json:"new_customers"`
	ReturningCustomers   int64     `json:"returning_customers"`
	NewCustomerRatePct   float64   `json:"new_customer_rate_pct"`
	AvgLTV               float64   `json:"avg_ltv"`
	AvgOrderFrequency    float64   `json:"avg_order_frequency"`
	AvgOrdersPerCustomer float64   `json:"avg_orders_per_customer"`
}

// CustomerSummaryHandler summarizes the customers who bought in a range.
//
// GET /v1/analytics/customers/summary?from=&to=
//
//   - total_customers: distinct customers with a non-cancelled order created in the range
//   - new_customers:   of those, customers whose first-ever order falls in the range
//   - avg_ltv:         their average lifetime spend (all time, from customer_aggregates)
//   - avg_order_frequency / avg_orders_per_customer: orders in the range per customer (same value)
//
// Distinct counts cannot be added up from daily aggregates, so this reads
// order_state for the range, joined to customer_aggregates for lifetime figures.
func (h *Handler) CustomerSummaryHandler(w http.ResponseWriter, r *http.Request) {
	tenantID := h.resolveRequestTenant(r)
	done, ok := h.acquireQuery(w, r, tenantID, "customers.summary")
	if !ok {
		return
	}
	defer done()

	win, ok := parseWindow(w, r)
	if !ok {
		return
	}

	var out CustomerSummary
	var orders int64
	err := h.db.QueryRowContext(r.Context(), `
		WITH buyers AS (
			SELECT customer_id, COUNT(*) AS n
			FROM order_state
			WHERE tenant_id = $1 AND has_extended AND customer_id <> '' AND status <> 'cancelled'
			  AND occurred_at >= $2 AND occurred_at < $3
			GROUP BY customer_id
		)
		SELECT COUNT(*), COALESCE(SUM(b.n), 0)::bigint,
		       COUNT(*) FILTER (WHERE ca.first_order_at >= $2),
		       COALESCE(AVG(ca.total_spend), 0)
		FROM buyers b
		LEFT JOIN customer_aggregates ca ON ca.tenant_id = $1 AND ca.customer_id = b.customer_id`,
		tenantID, win.Start, win.End).Scan(&out.TotalCustomers, &orders, &out.NewCustomers, &out.AvgLTV)
	if err != nil {
		queryFailed(w, r, err)
		return
	}
	out.TenantID, out.From, out.To = tenantID, win.Start, win.End
	out.ReturningCustomers = out.TotalCustomers - out.NewCustomers
	out.NewCustomerRatePct = pct(float64(out.NewCustomers), float64(out.TotalCustomers))
	out.AvgLTV = round2(out.AvgLTV)
	out.AvgOrderFrequency = ratio(float64(orders), float64(out.TotalCustomers))
	out.AvgOrdersPerCustomer = out.AvgOrderFrequency
	respondJSON(w, http.StatusOK, out)
}

// TopBuyer is one row of the top-buyers table.
type TopBuyer struct {
	CustomerID    string     `json:"customer_id"`
	CustomerTier  string     `json:"customer_tier"`
	OrderCount    int64      `json:"order_count"`
	TotalSpend    float64    `json:"total_spend"`
	TotalDiscount float64    `json:"total_discount"`
	AvgOrderValue float64    `json:"avg_order_value"`
	FirstOrderAt  *time.Time `json:"first_order_at,omitempty"`
	LastOrderAt   *time.Time `json:"last_order_at,omitempty"`
}

// TopBuyers lists the highest-spending customers.
//
// GET /v1/analytics/customers/top?limit=20[&from=&to=]
//
// Without from/to it ranks all-time lifetime spend from customer_aggregates.
// With a range it ranks spend on orders created in that range (and
// first/last_order_at are the earliest/latest order in the range).
func (h *Handler) TopBuyers(w http.ResponseWriter, r *http.Request) {
	tenantID := h.resolveRequestTenant(r)
	done, ok := h.acquireQuery(w, r, tenantID, "customers.top")
	if !ok {
		return
	}
	defer done()

	limit := intParam(r, "limit", 20, 1, 100)
	q := r.URL.Query()
	ranged := q.Get("from") != "" || q.Get("to") != ""

	var query string
	var args []interface{}
	if ranged {
		win, ok := parseWindow(w, r)
		if !ok {
			return
		}
		query = `
			SELECT s.customer_id, COALESCE(ca.customer_tier, 'regular'), COUNT(*), SUM(s.amount), SUM(s.discount_amount),
			       MIN(s.occurred_at), MAX(s.occurred_at)
			FROM order_state s
			LEFT JOIN customer_aggregates ca ON ca.tenant_id = s.tenant_id AND ca.customer_id = s.customer_id
			WHERE s.tenant_id = $1 AND s.has_extended AND s.customer_id <> '' AND s.status <> 'cancelled'
			  AND s.occurred_at >= $2 AND s.occurred_at < $3
			GROUP BY s.customer_id, ca.customer_tier
			ORDER BY SUM(s.amount) DESC, s.customer_id
			LIMIT $4`
		args = []interface{}{tenantID, win.Start, win.End, limit}
	} else {
		query = `
			SELECT customer_id, customer_tier, order_count, total_spend, total_discount, first_order_at, last_order_at
			FROM customer_aggregates
			WHERE tenant_id = $1 AND order_count > 0
			ORDER BY total_spend DESC, customer_id
			LIMIT $2`
		args = []interface{}{tenantID, limit}
	}

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		queryFailed(w, r, err)
		return
	}
	defer rows.Close()

	data := make([]TopBuyer, 0, limit)
	for rows.Next() {
		var b TopBuyer
		if err := rows.Scan(&b.CustomerID, &b.CustomerTier, &b.OrderCount, &b.TotalSpend, &b.TotalDiscount,
			&b.FirstOrderAt, &b.LastOrderAt); err != nil {
			queryFailed(w, r, err)
			return
		}
		b.AvgOrderValue = ratio(b.TotalSpend, float64(b.OrderCount))
		b.TotalSpend, b.TotalDiscount = round2(b.TotalSpend), round2(b.TotalDiscount)
		data = append(data, b)
	}
	if err := rows.Err(); err != nil {
		queryFailed(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{"tenant_id": tenantID, "data": data})
}

// CohortPoint is one bucket of the new-vs-returning chart.
type CohortPoint struct {
	Bucket           string  `json:"bucket"`
	NewOrders        int64   `json:"new_orders"`
	ReturningOrders  int64   `json:"returning_orders"`
	NewRevenue       float64 `json:"new_revenue"`
	ReturningRevenue float64 `json:"returning_revenue"`
}

// CustomerCohorts returns new-versus-returning orders per day or ISO week.
//
// GET /v1/analytics/customers/cohorts?from=&to=&interval=day|week
//
// Weeks start on Monday (UTC); the first and last week may be partial.
func (h *Handler) CustomerCohorts(w http.ResponseWriter, r *http.Request) {
	tenantID := h.resolveRequestTenant(r)
	done, ok := h.acquireQuery(w, r, tenantID, "customers.cohorts")
	if !ok {
		return
	}
	defer done()

	interval := r.URL.Query().Get("interval")
	if interval != "week" {
		interval = "day"
	}
	win, ok := parseWindow(w, r)
	if !ok {
		return
	}

	rows, err := h.db.QueryContext(r.Context(), `
		SELECT date_trunc($4, bucket::timestamp)::date::text,
		       COALESCE(SUM(order_count)   FILTER (WHERE customer_type = 'new'), 0)::bigint,
		       COALESCE(SUM(order_count)   FILTER (WHERE customer_type = 'returning'), 0)::bigint,
		       COALESCE(SUM(total_revenue) FILTER (WHERE customer_type = 'new'), 0),
		       COALESCE(SUM(total_revenue) FILTER (WHERE customer_type = 'returning'), 0)
		FROM cohort_daily
		WHERE tenant_id = $1 AND bucket >= $2::date AND bucket < $3::date
		GROUP BY 1
		ORDER BY 1`,
		tenantID, win.Start, win.End, interval)
	if err != nil {
		queryFailed(w, r, err)
		return
	}
	defer rows.Close()

	data := make([]CohortPoint, 0)
	for rows.Next() {
		var p CohortPoint
		if err := rows.Scan(&p.Bucket, &p.NewOrders, &p.ReturningOrders, &p.NewRevenue, &p.ReturningRevenue); err != nil {
			queryFailed(w, r, err)
			return
		}
		p.NewRevenue, p.ReturningRevenue = round2(p.NewRevenue), round2(p.ReturningRevenue)
		data = append(data, p)
	}
	if err := rows.Err(); err != nil {
		queryFailed(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{"tenant_id": tenantID, "interval": interval, "data": data})
}
