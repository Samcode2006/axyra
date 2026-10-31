package handlers

import (
	"log"
	"net/http"
	"time"
)

// queryFailed logs the real cause and returns a generic 500, so database
// details never reach API clients.
func queryFailed(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("[analytics] %s %s: %v", r.Method, r.URL.Path, err)
	respondError(w, http.StatusInternalServerError, "Query failed")
}

// RevenueResponse is the body of GET /v1/analytics/orders/revenue.
type RevenueResponse struct {
	TenantID      string    `json:"tenant_id"`
	From          time.Time `json:"from"`
	To            time.Time `json:"to"` // exclusive
	Currency      string    `json:"currency"`
	GrossRevenue  float64   `json:"gross_revenue"`
	TotalDiscount float64   `json:"total_discount"`
	NetRevenue    float64   `json:"net_revenue"`
	TotalTax      float64   `json:"total_tax"`
	TotalShipping float64   `json:"total_shipping"`
	ActiveOrders  int64     `json:"active_orders"`
	AvgOrderValue float64   `json:"avg_order_value"`
	TotalQuantity int64     `json:"total_quantity"`
}

// OrderRevenue returns the revenue breakdown for a range.
//
// GET /v1/analytics/orders/revenue?from=&to=
//
// It reads order_summary_hourly, the same table as /orders/summary, so the two
// agree. avg_order_value divides by active (non-cancelled) orders: cancelled
// orders carry no revenue, so counting them would understate the average.
func (h *Handler) OrderRevenue(w http.ResponseWriter, r *http.Request) {
	tenantID := h.resolveRequestTenant(r)
	done, ok := h.acquireQuery(w, r, tenantID, "orders.revenue")
	if !ok {
		return
	}
	defer done()

	win, ok := parseWindow(w, r)
	if !ok {
		return
	}

	var out RevenueResponse
	err := h.db.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM(total_revenue), 0), COALESCE(SUM(total_discount), 0),
		       COALESCE(SUM(total_tax), 0), COALESCE(SUM(total_shipping), 0),
		       COALESCE(SUM(active_orders), 0)::bigint, COALESCE(SUM(total_quantity), 0)::bigint
		FROM order_summary_hourly
		WHERE tenant_id = $1 AND bucket >= $2 AND bucket < $3`,
		tenantID, win.Start, win.End).
		Scan(&out.GrossRevenue, &out.TotalDiscount, &out.TotalTax, &out.TotalShipping, &out.ActiveOrders, &out.TotalQuantity)
	if err != nil {
		queryFailed(w, r, err)
		return
	}
	out.TenantID, out.From, out.To, out.Currency = tenantID, win.Start, win.End, "INR"
	out.NetRevenue = round2(out.GrossRevenue - out.TotalDiscount)
	out.AvgOrderValue = ratio(out.GrossRevenue, float64(out.ActiveOrders))
	out.GrossRevenue, out.TotalDiscount = round2(out.GrossRevenue), round2(out.TotalDiscount)
	out.TotalTax, out.TotalShipping = round2(out.TotalTax), round2(out.TotalShipping)
	respondJSON(w, http.StatusOK, out)
}
