package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/axyra/analytics/internal/models"
)

func (h *Handler) TopProducts(w http.ResponseWriter, r *http.Request) {
	tenantID := h.resolveRequestTenant(r)

	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

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

	products := make([]models.TopProduct, 0)
	for rows.Next() {
		var p models.TopProduct
		if err := rows.Scan(&p.ProductID, &p.ProductName, &p.Category, &p.OrderCount, &p.Revenue); err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("Scan error: %v", err))
			return
		}
		products = append(products, p)
	}

	respondJSON(w, http.StatusOK, models.TopProductsResponse{
		TenantID: tenantID,
		Data:     products,
	})
}

