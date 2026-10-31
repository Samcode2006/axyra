package handlers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/axyra/analytics/internal/models"
)

func (h *Handler) OrderBreakdown(w http.ResponseWriter, r *http.Request) {
	tenantID := h.resolveRequestTenant(r)

	done, ok := h.acquireQuery(w, r, tenantID, "orders.breakdown")
	if !ok {
		return
	}
	defer done()

	groupBy := r.URL.Query().Get("group_by")
	if groupBy != "category" && groupBy != "status" {
		groupBy = "status"
	}

	// Parse optional time range; when absent, report across all order_state rows.
	now := time.Now().UTC()
	fromTime := now.Add(-7 * 24 * time.Hour)
	toTime := now
	hasRange := false

	if f := r.URL.Query().Get("from"); f != "" {
		if t, err := time.Parse(time.RFC3339, f); err == nil {
			fromTime = t.UTC()
			hasRange = true
		}
	}
	if ts := r.URL.Query().Get("to"); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			toTime = t.UTC()
		}
	}

	var query string
	var args []interface{}

	if hasRange {
		if groupBy == "status" {
			query = `
				SELECT status AS grp, COUNT(*) AS cnt, COALESCE(SUM(amount), 0) AS rev
				FROM order_state
				WHERE tenant_id = $1
				  AND occurred_at >= $2
				  AND occurred_at <= $3
				GROUP BY status
				ORDER BY cnt DESC`
		} else {
			query = `
				SELECT category AS grp, COUNT(*) AS cnt, COALESCE(SUM(amount), 0) AS rev
				FROM order_state
				WHERE tenant_id = $1
				  AND occurred_at >= $2
				  AND occurred_at <= $3
				GROUP BY category
				ORDER BY cnt DESC`
		}
		args = []interface{}{tenantID, fromTime, toTime}
	} else {
		// No date range: query full order_state
		if groupBy == "status" {
			query = `
				SELECT status AS grp, COUNT(*) AS cnt, COALESCE(SUM(amount), 0) AS rev
				FROM order_state
				WHERE tenant_id = $1
				GROUP BY status
				ORDER BY cnt DESC`
		} else {
			query = `
				SELECT category AS grp, COUNT(*) AS cnt, COALESCE(SUM(amount), 0) AS rev
				FROM order_state
				WHERE tenant_id = $1
				GROUP BY category
				ORDER BY cnt DESC`
		}
		args = []interface{}{tenantID}
	}

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Query error: %v", err))
		return
	}
	defer rows.Close()

	items := make([]models.BreakdownItem, 0)
	for rows.Next() {
		var item models.BreakdownItem
		if err := rows.Scan(&item.Group, &item.Count, &item.Revenue); err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("Scan error: %v", err))
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Row error: %v", err))
		return
	}

	respondJSON(w, http.StatusOK, models.BreakdownResponse{
		TenantID: tenantID,
		GroupBy:  groupBy,
		Data:     items,
	})
}
