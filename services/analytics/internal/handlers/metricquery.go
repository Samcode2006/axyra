package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/lib/pq"
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

// Allowlists for config-driven metrics (schema expansion plan, section 8).
// Column names are only ever taken from these maps, never from the definition,
// so a metric can never put arbitrary text into the SQL. extended marks columns
// that exist only for schema v2 orders: a query that touches one is limited to
// those orders, because v1 orders hold column defaults there, not real data.
//
// order_id is accepted for count_distinct for compatibility: the control plane
// has always allowed it, so existing definitions that use it keep working.
type column struct{ extended bool }

var (
	metricNumeric = map[string]column{
		"amount": {}, "discount_amount": {extended: true}, "tax_amount": {extended: true},
		"shipping_cost": {extended: true}, "quantity": {extended: true},
	}
	metricDistinct = map[string]column{
		"order_id": {}, "status": {}, "category": {}, "product_id": {}, "currency": {},
		"payment_method": {extended: true}, "channel": {extended: true}, "country": {extended: true},
		"customer_tier": {extended: true}, "fulfillment_status": {extended: true}, "sku": {extended: true},
	}
	metricGroupBy = map[string]column{
		"status": {}, "category": {}, "product_id": {}, "currency": {},
		"payment_method": {extended: true}, "channel": {extended: true}, "country": {extended: true},
		"customer_tier": {extended: true}, "fulfillment_status": {extended: true}, "referrer_source": {extended: true},
	}
	metricFilter = map[string]column{
		"status": {}, "category": {}, "currency": {}, "product_id": {},
		"payment_method": {extended: true}, "channel": {extended: true}, "country": {extended: true},
		"customer_tier": {extended: true}, "fulfillment_status": {extended: true}, "payment_status": {extended: true},
		"referrer_source": {extended: true}, "return_reason": {extended: true},
	}
)

func keysOf(m map[string]column) string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

// executeMetric runs a safe aggregate query derived from the metric config.
// Only the order_state source is supported.
func (h *Handler) executeMetric(ctx context.Context, tenantID string, def *metricDefinitionResp, from, to time.Time) (interface{}, error) {
	if def.Config.Source != "orders" {
		return nil, fmt.Errorf("unsupported source %q (only 'orders' is supported)", def.Config.Source)
	}

	needsExtended := false
	field := def.Config.Field

	var aggExpr string
	switch def.Config.Aggregation {
	case "count":
		aggExpr = "COUNT(*)"
	case "sum", "avg", "min", "max":
		if field == "" {
			field = "amount" // historical default
		}
		col, ok := metricNumeric[field]
		if !ok {
			return nil, fmt.Errorf("%s supports fields: %s", def.Config.Aggregation, keysOf(metricNumeric))
		}
		needsExtended = needsExtended || col.extended
		aggExpr = fmt.Sprintf("COALESCE(%s(%s), 0)", strings.ToUpper(def.Config.Aggregation), field)
	case "count_distinct":
		col, ok := metricDistinct[field]
		if !ok {
			return nil, fmt.Errorf("count_distinct supports fields: %s", keysOf(metricDistinct))
		}
		needsExtended = needsExtended || col.extended
		aggExpr = fmt.Sprintf("COUNT(DISTINCT %s)", field)
	default:
		return nil, fmt.Errorf("unsupported aggregation %q", def.Config.Aggregation)
	}

	// WHERE: tenant + time range + allowlisted filters. An unknown filter is an
	// error: silently ignoring it would return a wrong number.
	args := []interface{}{tenantID, from, to}
	whereExtra := ""
	for _, f := range def.Config.Filters {
		col, ok := metricFilter[f.Field]
		if !ok {
			return nil, fmt.Errorf("filter field %q is not supported (allowed: %s)", f.Field, keysOf(metricFilter))
		}
		needsExtended = needsExtended || col.extended
		switch f.Op {
		case "eq", "neq":
			args = append(args, fmt.Sprintf("%v", f.Value))
			op := "="
			if f.Op == "neq" {
				op = "<>"
			}
			whereExtra += fmt.Sprintf(" AND %s %s $%d", f.Field, op, len(args))
		case "in":
			vals, ok := stringList(f.Value)
			if !ok || len(vals) == 0 {
				return nil, fmt.Errorf("filter on %q with op \"in\" needs a non-empty list of strings", f.Field)
			}
			args = append(args, pq.Array(vals))
			whereExtra += fmt.Sprintf(" AND %s = ANY($%d)", f.Field, len(args))
		default:
			return nil, fmt.Errorf("filter op %q is not supported (eq, neq, in)", f.Op)
		}
	}

	groupByClause := ""
	selectExtra := ""
	if len(def.Config.GroupBy) > 0 {
		cols := make([]string, 0, len(def.Config.GroupBy))
		for _, c := range def.Config.GroupBy {
			col, ok := metricGroupBy[c]
			if !ok {
				return nil, fmt.Errorf("group_by field %q is not supported (allowed: %s)", c, keysOf(metricGroupBy))
			}
			needsExtended = needsExtended || col.extended
			cols = append(cols, c)
		}
		for _, c := range cols {
			selectExtra += ", " + c
		}
		groupByClause = "GROUP BY " + joinCols(cols)
	}

	if needsExtended {
		whereExtra += " AND has_extended"
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
			// lib/pq returns NUMERIC and text as []byte; make them JSON-friendly.
			if b, ok := vals[i].([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = vals[i]
			}
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

// stringList converts a decoded JSON array of strings.
func stringList(v any) ([]string, bool) {
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		s, ok := x.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
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
