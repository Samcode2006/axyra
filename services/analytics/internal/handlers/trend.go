package handlers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/axyra/analytics/internal/models"
)

func (h *Handler) OrderTrend(w http.ResponseWriter, r *http.Request) {
	tenantID := h.resolveRequestTenant(r)

	done, ok := h.acquireQuery(w, r, tenantID, "orders.trend")
	if !ok {
		return
	}
	defer done()

	interval := r.URL.Query().Get("interval")
	if interval != "hour" && interval != "day" {
		interval = "day"
	}

	now := time.Now().UTC()
	fromTime := now.Add(-7 * 24 * time.Hour)
	toTime := now

	if f := r.URL.Query().Get("from"); f != "" {
		if t, err := time.Parse(time.RFC3339, f); err == nil {
			fromTime = t
		}
	}
	if tStr := r.URL.Query().Get("to"); tStr != "" {
		if t, err := time.Parse(time.RFC3339, tStr); err == nil {
			toTime = t
		}
	}

	trendPoints := make([]models.TrendPoint, 0)
	// The organization scope is still enforced by analytics_scope_tenants.
	// All SQL fragments here are fixed identifiers, never caller-provided SQL.
	tenantColumn, grouping := "''::text", "bucket"
	if r.URL.Query().Get("group_by") == "tenant" {
		tenantColumn, grouping = "tenant_id", "bucket, tenant_id"
	}

	if interval == "hour" {
		rows, err := h.db.QueryContext(r.Context(), fmt.Sprintf(`
			SELECT %s, bucket, SUM(total_orders)::bigint, SUM(total_revenue), SUM(total_discount), SUM(new_customer_orders)::bigint
			FROM order_summary_hourly
			WHERE tenant_id IN (SELECT analytics_scope_tenants($1)) AND bucket >= $2 AND bucket <= $3
			GROUP BY %s
            ORDER BY %s
		`, tenantColumn, grouping, grouping), tenantID, fromTime.Truncate(time.Hour), toTime)
		if err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("Query error: %v", err))
			return
		}
		defer rows.Close()

		for rows.Next() {
			var p models.TrendPoint
			if err := rows.Scan(&p.TenantID, &p.Bucket, &p.OrderCount, &p.Revenue, &p.Discount, &p.NewCustomers); err != nil {
				respondError(w, http.StatusInternalServerError, fmt.Sprintf("Scan error: %v", err))
				return
			}
			trendPoints = append(trendPoints, p)
		}
		if err := rows.Err(); err != nil {
			respondError(w, http.StatusInternalServerError, "Could not read trend data")
			return
		}
	} else {
		rows, err := h.db.QueryContext(r.Context(), fmt.Sprintf(`
			SELECT %s, bucket, SUM(total_orders)::bigint, SUM(total_revenue), SUM(total_discount), SUM(new_customer_orders)::bigint
			FROM order_summary_daily
			WHERE tenant_id IN (SELECT analytics_scope_tenants($1)) AND bucket >= $2::date AND bucket <= $3::date
			GROUP BY %s
            ORDER BY %s
		`, tenantColumn, grouping, grouping), tenantID, fromTime.Format("2006-01-02"), toTime.Format("2006-01-02"))
		if err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("Query error: %v", err))
			return
		}
		defer rows.Close()

		for rows.Next() {
			var p models.TrendPoint
			var dateStr string
			if err := rows.Scan(&p.TenantID, &dateStr, &p.OrderCount, &p.Revenue, &p.Discount, &p.NewCustomers); err != nil {
				respondError(w, http.StatusInternalServerError, fmt.Sprintf("Scan error: %v", err))
				return
			}
			if t, err := time.Parse("2006-01-02", dateStr[:10]); err == nil {
				p.Bucket = t
			}
			trendPoints = append(trendPoints, p)
		}
		if err := rows.Err(); err != nil {
			respondError(w, http.StatusInternalServerError, "Could not read trend data")
			return
		}
	}

	for i := range trendPoints {
		trendPoints[i].NetRevenue = round2(trendPoints[i].Revenue - trendPoints[i].Discount)
		trendPoints[i].Discount = round2(trendPoints[i].Discount)
	}

	respondJSON(w, http.StatusOK, models.TrendResponse{
		TenantID: tenantID,
		Interval: interval,
		Data:     trendPoints,
	})
}
