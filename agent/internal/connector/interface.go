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
}

// Connector is the pluggable interface for consuming change events from any source database.
type Connector interface {
	Start(ctx context.Context, out chan<- ChangeEvent) error
	Close() error
}
