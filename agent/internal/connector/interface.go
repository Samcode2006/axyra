package connector

import (
	"context"
	"time"
)

// ChangeEvent represents a raw database change captured by any connector.
type ChangeEvent struct {
	Table     string                 `json:"table"`
	Operation string                 `json:"operation"` // "INSERT", "UPDATE", "DELETE"
	LSN       string                 `json:"lsn"`
	Timestamp time.Time              `json:"timestamp"`
	Row       map[string]interface{} `json:"row"`
	OldRow    map[string]interface{} `json:"old_row,omitempty"`
	// SourceName is the name of the source connector that produced this event.
	// Set to SourceConfig.Name by the multi-source runner.
	SourceName string `json:"source_name,omitempty"`
	// OverrideTenantID, when non-empty, pins all events from this source to
	// a specific external_tenant_id regardless of row contents. Used in the
	// database-per-tenant model where the DB identity IS the tenant identity.
	OverrideTenantID string `json:"override_tenant_id,omitempty"`
}

// Connector is the pluggable interface for consuming change events from any source database.
type Connector interface {
	Start(ctx context.Context, out chan<- ChangeEvent) error
	Close() error
}
