package handlers

import (
	"fmt"
	"net/http"

	"github.com/axyra/analytics/internal/models"
)

func (h *Handler) OrderBreakdown(w http.ResponseWriter, r *http.Request) {
	tenantID := h.resolveRequestTenant(r)

	groupBy := r.URL.Query().Get("group_by")
	if groupBy != "category" && groupBy != "status" {
		groupBy = "status"
	}

	query := `
		SELECT status AS grp, COUNT(*) AS cnt, COALESCE(SUM(amount), 0) AS rev
		FROM order_state
		WHERE tenant_id = $1
		GROUP BY status
		ORDER BY cnt DESC
	`
	if groupBy == "category" {
		query = `
			SELECT category AS grp, COUNT(*) AS cnt, COALESCE(SUM(amount), 0) AS rev
			FROM order_state
			WHERE tenant_id = $1
			GROUP BY category
			ORDER BY cnt DESC
		`
	}

	rows, err := h.db.QueryContext(r.Context(), query, tenantID)
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

	respondJSON(w, http.StatusOK, models.BreakdownResponse{
		TenantID: tenantID,
		GroupBy:  groupBy,
		Data:     items,
	})
}

