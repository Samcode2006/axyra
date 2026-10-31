package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/axyra/analytics/internal/models"
)

func (h *Handler) OrderSummary(w http.ResponseWriter, r *http.Request) {
	tenantID := h.resolveRequestTenant(r)

	done, ok := h.acquireQuery(w, r, tenantID, "orders.summary")
	if !ok {
		return
	}
	defer done()

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

	var totalOrders int64
	var activeOrders int64
	var cancelledOrders int64
	var totalRevenue float64

	// Read from precomputed aggregates for the requested tenant
	err := h.db.QueryRowContext(r.Context(), `
		SELECT 
			COALESCE(SUM(total_orders), 0),
			COALESCE(SUM(active_orders), 0),
			COALESCE(SUM(cancelled_orders), 0),
			COALESCE(SUM(total_revenue), 0)
		FROM order_summary_hourly
		WHERE tenant_id = $1 AND bucket >= $2 AND bucket <= $3
	`, tenantID, fromTime.Truncate(time.Hour), toTime).Scan(&totalOrders, &activeOrders, &cancelledOrders, &totalRevenue)
	if err != nil && err != sql.ErrNoRows {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Query error: %v", err))
		return
	}

	// Fetch last processed time for this tenant
	var lastProcessedAt time.Time
	err = h.db.QueryRowContext(r.Context(), `
		SELECT COALESCE(MAX(last_processed_at), NOW())
		FROM processing_watermarks
		WHERE tenant_id = $1
	`, tenantID).Scan(&lastProcessedAt)
	if err != nil || lastProcessedAt.IsZero() {
		lastProcessedAt = time.Now().UTC()
	}

	respondJSON(w, http.StatusOK, models.OrderSummary{
		TenantID:        tenantID,
		From:            fromTime,
		To:              toTime,
		TotalOrders:     totalOrders,
		TotalRevenue:    totalRevenue,
		Currency:        "INR",
		CancelledOrders: cancelledOrders,
		ActiveOrders:    activeOrders,
		LastProcessedAt: lastProcessedAt,
	})
}

