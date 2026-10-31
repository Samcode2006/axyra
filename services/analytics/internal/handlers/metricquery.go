package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// metricConfigShape mirrors the MetricConfig type in the control plane
// and React frontend. Only fields needed for query execution are parsed.
type metricConfigShape struct {
	Source      string   `json:"source"`
	Aggregation string   `json:"aggregation"`
	Field       string   `json:"field,omitempty"`
	GroupBy     []string `json:"group_by,omitempty"`
	Filters     []struct {
		Field string `json:"field"`
		Op    string `json:"op"` // "eq", "neq", "in"
		Value any    `json:"value"`
	} `json:"filters,omitempty"`
}

// metricDefinitionResp is the minimal shape we need from the control plane.
type metricDefinitionResp struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Enabled bool              `json:"enabled"`
	Config  metricConfigShape `json:"config"`
}

// QueryMetric executes a config-driven metric query (B-4).
//
// GET /v1/analytics/metrics/{metric_id}?from=...&to=...
//
// The metric definition is fetched from the control plane and the aggregation
// is computed against order_state or the pre-computed aggregate tables.
// Arbitrary SQL is never accepted.
func (h *Handler) QueryMetric(w http.ResponseWriter, r *http.Request) {
	// Extract metric_id from URL: path is /v1/analytics/metrics/{metric_id}
	metricID := r.PathValue("metric_id")
	if metricID == "" {
		respondError(w, http.StatusBadRequest, "metric_id is required")
		return
	}

	tenantID := h.resolveRequestTenant(r)

	done, ok := h.acquireQuery(w, r, tenantID, "metric."+metricID)
	if !ok {
		return
	}
	defer done()

	// Parse time range
	now := time.Now().UTC()
	fromTime := now.Add(-7 * 24 * time.Hour)
	toTime := now
	if f := r.URL.Query().Get("from"); f != "" {
		if t, err := time.Parse(time.RFC3339, f); err == nil {
			fromTime = t.UTC()
		}
	}
	if ts := r.URL.Query().Get("to"); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			toTime = t.UTC()
		}
	}

	// Fetch metric definition from control plane
	def, err := h.fetchMetricDef(r.Context(), metricID)
	if err != nil {
		respondError(w, http.StatusNotFound, fmt.Sprintf("metric definition not found: %v", err))
		return
	}
	if !def.Enabled {
		respondError(w, http.StatusGone, "metric definition is disabled")
		return
	}

	result, err := h.executeMetric(r.Context(), tenantID, def, fromTime, toTime)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("metric query error: %v", err))
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"metric_id": metricID,
		"name":      def.Name,
		"tenant_id": tenantID,
		"from":      fromTime,
		"to":        toTime,
		"result":    result,
	})
}

// fetchMetricDef retrieves the metric definition from the control plane.
func (h *Handler) fetchMetricDef(ctx context.Context, metricID string) (*metricDefinitionResp, error) {
	url := fmt.Sprintf("%s/v1/metric-definitions/%s", h.cfg.ControlPlaneURL, metricID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("control plane unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("not found")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("control plane returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Config comes as a raw JSON blob; unmarshal it separately
	var raw struct {
		ID      string          `json:"id"`
		Name    string          `json:"name"`
		Enabled bool            `json:"enabled"`
		Config  json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode definition: %w", err)
	}

	var cfg metricConfigShape
	if err := json.Unmarshal(raw.Config, &cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	return &metricDefinitionResp{
		ID:      raw.ID,
		Name:    raw.Name,
		Enabled: raw.Enabled,
		Config:  cfg,
	}, nil
}

// executeMetric runs a safe aggregate query derived from the metric config.
// Only the order_state source is supported; aggregations are count/sum/avg/min/max.
func (h *Handler) executeMetric(ctx context.Context, tenantID string, def *metricDefinitionResp, from, to time.Time) (interface{}, error) {
	if def.Config.Source != "orders" {
		return nil, fmt.Errorf("unsupported source %q (only 'orders' is supported)", def.Config.Source)
	}

	// Build an allowlisted aggregate expression
	var aggExpr string
	field := def.Config.Field
	switch def.Config.Aggregation {
	case "count":
		aggExpr = "COUNT(*)"
	case "sum":
		if field == "" || field == "amount" {
			aggExpr = "COALESCE(SUM(amount), 0)"
		} else {
			return nil, fmt.Errorf("sum only supports field=amount")
		}
	case "avg":
		if field == "" || field == "amount" {
			aggExpr = "COALESCE(AVG(amount), 0)"
		} else {
			return nil, fmt.Errorf("avg only supports field=amount")
		}
	case "min":
		if field == "" || field == "amount" {
			aggExpr = "COALESCE(MIN(amount), 0)"
		} else {
			return nil, fmt.Errorf("min only supports field=amount")
		}
	case "max":
		if field == "" || field == "amount" {
			aggExpr = "COALESCE(MAX(amount), 0)"
		} else {
			return nil, fmt.Errorf("max only supports field=amount")
		}
	case "count_distinct":
		// Only allow counting distinct values for allowlisted columns
		allowedDistinct := map[string]bool{"status": true, "category": true, "product_id": true, "currency": true}
		if field == "" || !allowedDistinct[field] {
			return nil, fmt.Errorf("count_distinct only supports fields: status, category, product_id, currency")
		}
		aggExpr = fmt.Sprintf("COUNT(DISTINCT %s)", field)
	default:
		return nil, fmt.Errorf("unsupported aggregation %q", def.Config.Aggregation)
	}

	// Build WHERE clause: tenant + time range + allowlisted filters
	args := []interface{}{tenantID, from, to}
	whereExtra := ""
	for _, f := range def.Config.Filters {
		// Only allow filtering on allowlisted columns
		allowedFilters := map[string]bool{"status": true, "category": true, "currency": true, "product_id": true}
		if !allowedFilters[f.Field] {
			continue
		}
		switch f.Op {
		case "eq":
			args = append(args, fmt.Sprintf("%v", f.Value))
			whereExtra += fmt.Sprintf(" AND %s = $%d", f.Field, len(args))
		case "neq":
			args = append(args, fmt.Sprintf("%v", f.Value))
			whereExtra += fmt.Sprintf(" AND %s <> $%d", f.Field, len(args))
		}
	}

	groupByClause := ""
	selectExtra := ""
	if len(def.Config.GroupBy) > 0 {
		allowedGroupBy := map[string]bool{"status": true, "category": true, "currency": true, "product_id": true}
		var cols []string
		for _, col := range def.Config.GroupBy {
			if allowedGroupBy[col] {
				cols = append(cols, col)
			}
		}
		if len(cols) > 0 {
			for _, col := range cols {
				selectExtra += ", " + col
			}
			groupByClause = "GROUP BY " + joinCols(cols)
		}
	}

	query := fmt.Sprintf(`
		SELECT %s%s
		FROM order_state
		WHERE tenant_id = $1
		  AND occurred_at >= $2
		  AND occurred_at <= $3
		  %s
		%s
	`, aggExpr, selectExtra, whereExtra, groupByClause)

	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("execute query: %w", err)
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	var results []map[string]interface{}
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		row := make(map[string]interface{}, len(cols))
		for i, col := range cols {
			row[col] = vals[i]
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	if len(results) == 0 {
		return nil, nil
	}
	if len(results) == 1 && groupByClause == "" {
		return results[0], nil
	}
	return results, nil
}

func joinCols(cols []string) string {
	s := ""
	for i, c := range cols {
		if i > 0 {
			s += ", "
		}
		s += c
	}
	return s
}
