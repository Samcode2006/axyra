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

	if interval == "hour" {
		rows, err := h.db.QueryContext(r.Context(), `
			SELECT bucket, total_orders, total_revenue
			FROM order_summary_hourly
			WHERE tenant_id = $1 AND bucket >= $2 AND bucket <= $3
			ORDER BY bucket ASC
		`, tenantID, fromTime.Truncate(time.Hour), toTime)
		if err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("Query error: %v", err))
			return
		}
		defer rows.Close()

		for rows.Next() {
			var p models.TrendPoint
			if err := rows.Scan(&p.Bucket, &p.OrderCount, &p.Revenue); err != nil {
				respondError(w, http.StatusInternalServerError, fmt.Sprintf("Scan error: %v", err))
				return
			}
			trendPoints = append(trendPoints, p)
		}
	} else {
		rows, err := h.db.QueryContext(r.Context(), `
			SELECT bucket, total_orders, total_revenue
			FROM order_summary_daily
			WHERE tenant_id = $1 AND bucket >= $2::date AND bucket <= $3::date
			ORDER BY bucket ASC
		`, tenantID, fromTime.Format("2006-01-02"), toTime.Format("2006-01-02"))
		if err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("Query error: %v", err))
			return
		}
		defer rows.Close()

		for rows.Next() {
			var p models.TrendPoint
			var dateStr string
			if err := rows.Scan(&dateStr, &p.OrderCount, &p.Revenue); err != nil {
				respondError(w, http.StatusInternalServerError, fmt.Sprintf("Scan error: %v", err))
				return
			}
			if t, err := time.Parse("2006-01-02", dateStr[:10]); err == nil {
				p.Bucket = t
			}
			trendPoints = append(trendPoints, p)
		}
	}

	respondJSON(w, http.StatusOK, models.TrendResponse{
		TenantID: tenantID,
		Interval: interval,
		Data:     trendPoints,
	})
}

