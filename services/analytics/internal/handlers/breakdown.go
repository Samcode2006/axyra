package handlers

import (
	"net/http"

	"github.com/axyra/analytics/internal/models"
)

// breakdownDims maps every accepted group_by value to its order_state column.
// This map is the allowlist: only the column names below ever reach the SQL
// text, never the request value. extended marks dimensions that only exist for
// schema v2 orders; those queries skip v1 orders, whose extended columns hold
// defaults and not real data.
var breakdownDims = map[string]struct {
	column   string
	extended bool
	// nonEmpty drops rows where the column is blank (for example orders that
	// were never returned have no return_reason).
	nonEmpty bool
}{
	"status":             {column: "status"},
	"category":           {column: "category"},
	"payment_method":     {column: "payment_method", extended: true},
	"channel":            {column: "channel", extended: true},
	"country":            {column: "country", extended: true},
	"customer_tier":      {column: "customer_tier", extended: true},
	"fulfillment_status": {column: "fulfillment_status", extended: true},
	"return_reason":      {column: "return_reason", extended: true, nonEmpty: true},
	"referrer_source":    {column: "referrer_source", extended: true},
}

// OrderBreakdown counts orders and revenue grouped by one dimension.
//
// GET /v1/analytics/orders/breakdown?group_by=...[&from=&to=]
//
// Rows are all orders (cancelled included), as this endpoint has always done,
// so group_by=status can show the cancelled bucket.
func (h *Handler) OrderBreakdown(w http.ResponseWriter, r *http.Request) {
	tenantID := h.resolveRequestTenant(r)

	done, ok := h.acquireQuery(w, r, tenantID, "orders.breakdown")
	if !ok {
		return
	}
	defer done()

	groupBy := r.URL.Query().Get("group_by")
	if groupBy == "" {
		groupBy = "status"
	}
	dim, ok := breakdownDims[groupBy]
	if !ok {
		respondError(w, http.StatusBadRequest,
			"group_by must be one of status, category, payment_method, channel, country, customer_tier, fulfillment_status, return_reason, referrer_source")
		return
	}

	query := `SELECT ` + dim.column + ` AS grp, COUNT(*) AS cnt, COALESCE(SUM(amount), 0) AS rev
		FROM order_state
		WHERE tenant_id = $1`
	args := []interface{}{tenantID}

	// With no range the whole order_state is reported, as before.
	if q := r.URL.Query(); q.Get("from") != "" || q.Get("to") != "" {
		win, ok := parseRangeExact(w, r)
		if !ok {
			return
		}
		query += ` AND occurred_at >= $2 AND occurred_at < $3`
		args = append(args, win.Start, win.End)
	}
	if dim.extended {
		query += ` AND has_extended`
	}
	if dim.nonEmpty {
		query += ` AND ` + dim.column + ` <> ''`
	}
	query += ` GROUP BY ` + dim.column + ` ORDER BY cnt DESC, grp`

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		queryFailed(w, r, err)
		return
	}
	defer rows.Close()

	items := make([]models.BreakdownItem, 0)
	for rows.Next() {
		var item models.BreakdownItem
		if err := rows.Scan(&item.Group, &item.Count, &item.Revenue); err != nil {
			queryFailed(w, r, err)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		queryFailed(w, r, err)
		return
	}

	respondJSON(w, http.StatusOK, models.BreakdownResponse{
		TenantID: tenantID,
		GroupBy:  groupBy,
		Data:     items,
	})
}
