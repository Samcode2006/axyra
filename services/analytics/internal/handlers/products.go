package handlers

import (
	"net/http"
	"time"

	"github.com/axyra/analytics/internal/models"
)

// productSort maps the sort_by parameter to a fixed ORDER BY expression.
// Only values from this map ever reach the SQL text.
var productSort = map[string]string{
	"revenue":  "total_revenue DESC",
	"quantity": "quantity_sold DESC",
	"returns":  "return_count DESC",
}

// TopProducts returns the top products by revenue, units sold or returns.
//
// GET /v1/analytics/products/top?limit=20&sort_by=revenue|quantity|returns[&from=&to=]
//
// Without a range it reads the precomputed product_aggregates (all time). With
// from/to it aggregates order_state for orders created in the range. Both
// count only non-cancelled orders, so the two modes agree where they overlap.
func (h *Handler) TopProducts(w http.ResponseWriter, r *http.Request) {
	tenantID := h.resolveRequestTenant(r)

	done, ok := h.acquireQuery(w, r, tenantID, "products.top")
	if !ok {
		return
	}
	defer done()

	limit := intParam(r, "limit", 10, 1, 100)

	sortBy := r.URL.Query().Get("sort_by")
	if sortBy == "" {
		sortBy = "revenue"
	}
	orderBy, ok := productSort[sortBy]
	if !ok {
		respondError(w, http.StatusBadRequest, "sort_by must be one of revenue, quantity, returns")
		return
	}

	q := r.URL.Query()
	var fromTime, toTime time.Time
	hasRange := q.Get("from") != "" || q.Get("to") != ""
	if hasRange {
		win, ok := parseRangeExact(w, r)
		if !ok {
			return
		}
		fromTime, toTime = win.Start, win.End
	}

	var query string
	var args []interface{}
	if hasRange {
		// Aggregate live from order_state. Quantity, discount and returns are
		// only known for extended (schema v2) orders.
		query = `
			SELECT product_id, MAX(product_name), MAX(category),
			       COUNT(*) FILTER (WHERE status <> 'cancelled'),
			       COALESCE(SUM(amount) FILTER (WHERE status <> 'cancelled'), 0) AS total_revenue,
			       COALESCE(SUM(quantity) FILTER (WHERE status <> 'cancelled' AND has_extended), 0)::bigint AS quantity_sold,
			       COALESCE(SUM(discount_amount) FILTER (WHERE status <> 'cancelled' AND has_extended), 0),
			       COUNT(*) FILTER (WHERE status <> 'cancelled' AND has_extended
			                          AND (status = 'returned' OR fulfillment_status = 'returned')) AS return_count
			FROM order_state
			WHERE tenant_id = $1 AND occurred_at >= $2 AND occurred_at < $3 AND product_id <> ''
			GROUP BY product_id
			ORDER BY ` + orderBy + `, product_id
			LIMIT $4`
		args = []interface{}{tenantID, fromTime, toTime, limit}
	} else {
		query = `
			SELECT product_id, product_name, category, order_count, total_revenue, quantity_sold, total_discount, return_count
			FROM product_aggregates
			WHERE tenant_id = $1
			ORDER BY ` + orderBy + `, product_id
			LIMIT $2`
		args = []interface{}{tenantID, limit}
	}

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		queryFailed(w, r, err)
		return
	}
	defer rows.Close()

	products := make([]models.TopProduct, 0)
	for rows.Next() {
		var p models.TopProduct
		if err := rows.Scan(&p.ProductID, &p.ProductName, &p.Category, &p.OrderCount, &p.Revenue,
			&p.QuantitySold, &p.TotalDiscount, &p.ReturnCount); err != nil {
			queryFailed(w, r, err)
			return
		}
		p.TotalRevenue = p.Revenue
		p.ReturnRatePct = pct(float64(p.ReturnCount), float64(p.OrderCount))
		p.AvgUnitPrice = ratio(p.Revenue, float64(p.QuantitySold))
		p.TotalDiscount = round2(p.TotalDiscount)
		products = append(products, p)
	}
	if err := rows.Err(); err != nil {
		queryFailed(w, r, err)
		return
	}

	respondJSON(w, http.StatusOK, models.TopProductsResponse{TenantID: tenantID, Data: products})
}

// parseRangeExact is parseWindow for handlers that have always filtered
// order_state by the exact timestamps given (not whole days): [from, to].
// `to` is made exclusive by a nanosecond so the semantics stay inclusive.
func parseRangeExact(w http.ResponseWriter, r *http.Request) (window, bool) {
	now := time.Now().UTC()
	from, to := now.Add(-7*24*time.Hour), now
	if s := r.URL.Query().Get("from"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "from must be an RFC 3339 timestamp")
			return window{}, false
		}
		from = t.UTC()
	}
	if s := r.URL.Query().Get("to"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "to must be an RFC 3339 timestamp")
			return window{}, false
		}
		to = t.UTC()
	}
	if from.After(to) {
		respondError(w, http.StatusBadRequest, "from must not be after to")
		return window{}, false
	}
	return window{Start: from, End: to.Add(time.Nanosecond)}, true
}
