package models

import "time"

type EventEnvelope struct {
	EventID          string                 `json:"event_id"`
	AgentID          string                 `json:"agent_id"`
	ExternalTenantID string                 `json:"external_tenant_id"`
	TenantID         string                 `json:"tenant_id,omitempty"` // resolved canonical tenant
	EventType        string                 `json:"event_type"`          // order.created, order.updated, order.cancelled
	SchemaVersion    int                    `json:"schema_version"`
	OccurredAt       time.Time              `json:"occurred_at"`
	LSN              string                 `json:"lsn"`
	Data             map[string]interface{} `json:"data"`
}

type IngestRequest struct {
	Events []EventEnvelope `json:"events"`
}

type IngestResponse struct {
	Status   string `json:"status"`
	Count    int    `json:"count"`
	Rejected int    `json:"rejected,omitempty"`
	LastLSN  string `json:"last_lsn,omitempty"`
}

type OrderSummary struct {
	TenantID        string    `json:"tenant_id"`
	From            time.Time `json:"from"`
	To              time.Time `json:"to"`
	TotalOrders     int64     `json:"total_orders"`
	TotalRevenue    float64   `json:"total_revenue"`
	Currency        string    `json:"currency"`
	CancelledOrders int64     `json:"cancelled_orders"`
	ActiveOrders    int64     `json:"active_orders"`
	LastProcessedAt time.Time `json:"last_processed_at"`
}

type TrendPoint struct {
	TenantID   string    `json:"tenant_id,omitempty"`
	Bucket     time.Time `json:"bucket"`
	OrderCount int64     `json:"order_count"`
	Revenue    float64   `json:"revenue"`
	// Extended series, from the same summary row. They are 0 for orders that
	// predate schema v2, which carry no discount or customer information.
	Discount     float64 `json:"discount"`
	NetRevenue   float64 `json:"net_revenue"`
	NewCustomers int64   `json:"new_customers"`
}

type TrendResponse struct {
	TenantID string       `json:"tenant_id"`
	Interval string       `json:"interval"` // 'hour' | 'day'
	Data     []TrendPoint `json:"data"`
}

type TopProduct struct {
	ProductID   string `json:"product_id"`
	ProductName string `json:"product_name"`
	Category    string `json:"category"`
	// OrderCount counts non-cancelled orders.
	OrderCount int64 `json:"order_count"`
	// Revenue is kept for existing clients; TotalRevenue carries the same value
	// under the name used by the schema expansion plan.
	Revenue       float64 `json:"revenue"`
	TotalRevenue  float64 `json:"total_revenue"`
	QuantitySold  int64   `json:"quantity_sold"`
	TotalDiscount float64 `json:"total_discount"`
	ReturnCount   int64   `json:"return_count"`
	ReturnRatePct float64 `json:"return_rate_pct"`
	AvgUnitPrice  float64 `json:"avg_unit_price"`
}

type TopProductsResponse struct {
	TenantID string       `json:"tenant_id"`
	Data     []TopProduct `json:"data"`
}

type BreakdownItem struct {
	Group   string  `json:"group"`
	Count   int64   `json:"count"`
	Revenue float64 `json:"revenue"`
}

type BreakdownResponse struct {
	TenantID string          `json:"tenant_id"`
	GroupBy  string          `json:"group_by"`
	Data     []BreakdownItem `json:"data"`
}

type AnalyticsStatus struct {
	TenantID        string     `json:"tenant_id"`
	LastProcessedAt *time.Time `json:"last_processed_at"`
	LastLSN         string     `json:"last_lsn"`
	LagSeconds      *float64   `json:"lag_seconds"`
	ActivityState   string     `json:"activity_state"`
	IsHealthy       bool       `json:"is_healthy"`
}
