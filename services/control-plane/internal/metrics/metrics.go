// Package metrics defines and validates config-driven metric definitions.
//
// A definition is a small declarative document: an aggregation over a
// whitelisted field of the order model, optionally grouped and filtered.
// There is deliberately no free-form expression or SQL anywhere in the schema;
// every field name, aggregation and operator is checked against a fixed set so
// a definition can never be turned into arbitrary query text.
package metrics

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Source is the only data model metrics can aggregate today.
const SourceOrders = "orders"

var (
	aggregations = map[string]bool{"count": true, "sum": true, "avg": true, "min": true, "max": true, "count_distinct": true}

	// numericFields can be used with sum/avg/min/max.
	numericFields = map[string]bool{"amount": true}

	// distinctFields can be used with count_distinct.
	distinctFields = map[string]bool{"order_id": true, "product_id": true, "category": true, "status": true}

	// dimensionFields can be used in group_by and in filters.
	dimensionFields = map[string]bool{"status": true, "category": true, "product_id": true, "currency": true}

	filterOps     = map[string]bool{"eq": true, "neq": true, "in": true}
	granularities = map[string]bool{"hour": true, "day": true}

	idPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`)
)

const (
	maxGroupBy       = 2
	maxFilters       = 5
	maxFilterValues  = 20
	maxFilterValueLn = 64
	maxConfigBytes   = 8 * 1024
	MaxNameLen       = 255
	MaxDescriptionLn = 2000
)

// Filter restricts the rows a metric aggregates.
type Filter struct {
	Field string          `json:"field"`
	Op    string          `json:"op"`
	Value json.RawMessage `json:"value"` // string for eq/neq, []string for in
}

// Config is the declarative body of a metric definition.
type Config struct {
	Source      string   `json:"source"`
	Aggregation string   `json:"aggregation"`
	Field       string   `json:"field,omitempty"`
	GroupBy     []string `json:"group_by,omitempty"`
	Filters     []Filter `json:"filters,omitempty"`
	Granularity []string `json:"granularity,omitempty"`
}

// Marshal returns the canonical JSON form that is stored in the database.
func (c Config) Marshal() ([]byte, error) { return json.Marshal(c) }

// ValidID reports whether id is an acceptable metric identifier.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// ParseConfig strictly decodes and validates a definition body. Unknown keys,
// unsupported values and oversized documents are all rejected.
func ParseConfig(raw []byte) (Config, error) {
	var c Config
	if len(raw) == 0 {
		return c, errors.New("config is required")
	}
	if len(raw) > maxConfigBytes {
		return c, fmt.Errorf("config exceeds %d bytes", maxConfigBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, fmt.Errorf("config is not valid: %v", err)
	}
	if dec.More() {
		return c, errors.New("config must be a single JSON object")
	}
	return c, c.Validate()
}

// Validate checks every field against the fixed whitelists.
func (c *Config) Validate() error {
	if c.Source != SourceOrders {
		return fmt.Errorf("source must be %q", SourceOrders)
	}
	if !aggregations[c.Aggregation] {
		return fmt.Errorf("aggregation must be one of %s", keys(aggregations))
	}

	switch c.Aggregation {
	case "count":
		if c.Field != "" {
			return errors.New("field must not be set for aggregation \"count\"")
		}
	case "count_distinct":
		if !distinctFields[c.Field] {
			return fmt.Errorf("field for count_distinct must be one of %s", keys(distinctFields))
		}
	default: // sum, avg, min, max
		if !numericFields[c.Field] {
			return fmt.Errorf("field for %s must be one of %s", c.Aggregation, keys(numericFields))
		}
	}

	if len(c.GroupBy) > maxGroupBy {
		return fmt.Errorf("group_by supports at most %d fields", maxGroupBy)
	}
	seen := map[string]bool{}
	for _, g := range c.GroupBy {
		if !dimensionFields[g] {
			return fmt.Errorf("group_by field %q must be one of %s", g, keys(dimensionFields))
		}
		if seen[g] {
			return fmt.Errorf("group_by field %q is repeated", g)
		}
		seen[g] = true
	}

	if len(c.Filters) > maxFilters {
		return fmt.Errorf("at most %d filters are allowed", maxFilters)
	}
	for i := range c.Filters {
		if err := c.Filters[i].validate(); err != nil {
			return fmt.Errorf("filters[%d]: %w", i, err)
		}
	}

	seen = map[string]bool{}
	for _, g := range c.Granularity {
		if !granularities[g] {
			return fmt.Errorf("granularity %q must be one of %s", g, keys(granularities))
		}
		if seen[g] {
			return fmt.Errorf("granularity %q is repeated", g)
		}
		seen[g] = true
	}
	return nil
}

func (f *Filter) validate() error {
	if !dimensionFields[f.Field] {
		return fmt.Errorf("field %q must be one of %s", f.Field, keys(dimensionFields))
	}
	if !filterOps[f.Op] {
		return fmt.Errorf("op %q must be one of %s", f.Op, keys(filterOps))
	}
	if len(f.Value) == 0 {
		return errors.New("value is required")
	}

	if f.Op == "in" {
		var vals []string
		if err := json.Unmarshal(f.Value, &vals); err != nil {
			return errors.New("value for op \"in\" must be an array of strings")
		}
		if len(vals) == 0 || len(vals) > maxFilterValues {
			return fmt.Errorf("op \"in\" needs between 1 and %d values", maxFilterValues)
		}
		for _, v := range vals {
			if err := checkFilterValue(v); err != nil {
				return err
			}
		}
		return nil
	}

	var val string
	if err := json.Unmarshal(f.Value, &val); err != nil {
		return fmt.Errorf("value for op %q must be a string", f.Op)
	}
	return checkFilterValue(val)
}

func checkFilterValue(v string) error {
	if v == "" || len(v) > maxFilterValueLn {
		return fmt.Errorf("filter values must be 1-%d characters", maxFilterValueLn)
	}
	return nil
}

func keys(m map[string]bool) string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

// Builtin is a platform-provided definition installed at first start.
type Builtin struct {
	ID          string
	Name        string
	Description string
	Config      Config
}

// Builtins returns the definitions matching the analytics endpoints that exist today.
func Builtins() []Builtin {
	hd := []string{"hour", "day"}
	return []Builtin{
		{"total_orders", "Total orders", "Number of orders in the selected range.",
			Config{Source: SourceOrders, Aggregation: "count", Granularity: hd}},
		{"total_revenue", "Total revenue", "Sum of order amounts in the selected range.",
			Config{Source: SourceOrders, Aggregation: "sum", Field: "amount", Granularity: hd}},
		{"average_order_value", "Average order value", "Mean order amount in the selected range.",
			Config{Source: SourceOrders, Aggregation: "avg", Field: "amount", Granularity: hd}},
		{"orders_by_status", "Orders by status", "Order count broken down by status.",
			Config{Source: SourceOrders, Aggregation: "count", GroupBy: []string{"status"}}},
		{"revenue_by_category", "Revenue by category", "Revenue broken down by product category.",
			Config{Source: SourceOrders, Aggregation: "sum", Field: "amount", GroupBy: []string{"category"}}},
	}
}
