package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/axyra/analytics/internal/models"
)

func (h *Handler) TopProducts(w http.ResponseWriter, r *http.Request) {
	tenantID := h.resolveRequestTenant(r)

	done, ok := h.acquireQuery(w, r, tenantID, "products.top")
	if !ok {
		return
	}
	defer done()

	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	// Parse optional time range. When provided, scope the query to order_state
	// rows whose occurred_at falls in [from, to]. Without a range, use the
	// pre-computed product_aggregates table for maximum speed.
	var fromTime, toTime time.Time
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
	if hasRange && toTime.IsZero() {
		toTime = time.Now().UTC()
	}

	var products []models.TopProduct

	if hasRange {
		// Time-scoped: aggregate live from order_state
		rows, err := h.db.QueryContext(r.Context(), `
			SELECT
				product_id,
				MAX(product_name) AS product_name,
				MAX(category)     AS category,
				COUNT(*)          AS order_count,
				COALESCE(SUM(CASE WHEN status <> 'cancelled' THEN amount ELSE 0 END), 0) AS total_revenue
			FROM order_state
			WHERE tenant_id = $1
			  AND occurred_at >= $2
			  AND occurred_at <= $3
			  AND product_id <> ''
			GROUP BY product_id
			ORDER BY total_revenue DESC
			LIMIT $4
		`, tenantID, fromTime, toTime, limit)
		if err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("Query error: %v", err))
			return
		}
		defer rows.Close()

		products = make([]models.TopProduct, 0)
		for rows.Next() {
			var p models.TopProduct
			if err := rows.Scan(&p.ProductID, &p.ProductName, &p.Category, &p.OrderCount, &p.Revenue); err != nil {
				respondError(w, http.StatusInternalServerError, fmt.Sprintf("Scan error: %v", err))
				return
			}
			products = append(products, p)
		}
		if err := rows.Err(); err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("Row error: %v", err))
			return
		}
	} else {
		// All-time: use pre-computed aggregates for speed
		rows, err := h.db.QueryContext(r.Context(), `
			SELECT product_id, product_name, category, order_count, total_revenue
			FROM product_aggregates
			WHERE tenant_id = $1
			ORDER BY total_revenue DESC
			LIMIT $2
		`, tenantID, limit)
		if err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("Query error: %v", err))
			return
		}
		defer rows.Close()

		products = make([]models.TopProduct, 0)
		for rows.Next() {
			var p models.TopProduct
			if err := rows.Scan(&p.ProductID, &p.ProductName, &p.Category, &p.OrderCount, &p.Revenue); err != nil {
				respondError(w, http.StatusInternalServerError, fmt.Sprintf("Scan error: %v", err))
				return
			}
			products = append(products, p)
		}
	}

	respondJSON(w, http.StatusOK, models.TopProductsResponse{
		TenantID: tenantID,
		Data:     products,
	})
}
