package mapper

import (
	"testing"
	"time"

	"github.com/axyra/agent/internal/config"
	"github.com/axyra/agent/internal/connector"
)

func TestMapperOrderCreated(t *testing.T) {
	cfg := &config.Config{
		AgentID: "agent_test",
		Tables: []config.TableConfig{
			{
				Name:            "orders",
				TenantIDColumn:  "store_id",
				PKColumn:        "id",
				EventTypeColumn: "status",
			},
		},
	}

	m := New(cfg)

	ce := connector.ChangeEvent{
		Table:     "orders",
		Operation: "INSERT",
		LSN:       "0/16B3D88",
		Timestamp: time.Now().UTC(),
		Row: map[string]interface{}{
			"id":           "order_101",
			"store_id":     "store_001",
			"amount":       2499.50,
			"currency":     "INR",
			"status":       "placed",
			"product_id":   "prod_01",
			"product_name": "Running Shoes",
			"category":     "Footwear",
		},
	}

	event, err := m.Map(ce)
	if err != nil {
		t.Fatalf("map error: %v", err)
	}

	if event.AgentID != "agent_test" {
		t.Errorf("expected AgentID agent_test, got %s", event.AgentID)
	}
	if event.ExternalTenantID != "store_001" {
		t.Errorf("expected ExternalTenantID store_001, got %s", event.ExternalTenantID)
	}
	if event.EventType != "order.created" {
		t.Errorf("expected EventType order.created, got %s", event.EventType)
	}
	if event.LSN != "0/16B3D88" {
		t.Errorf("expected LSN 0/16B3D88, got %s", event.LSN)
	}

	// Idempotency: same change event produces identical EventID
	event2, _ := m.Map(ce)
	if event.EventID != event2.EventID {
		t.Errorf("expected deterministic EventID, got %s and %s", event.EventID, event2.EventID)
	}
}

func TestMapperOrderCancelled(t *testing.T) {
	cfg := &config.Config{
		AgentID: "agent_test",
		Tables: []config.TableConfig{
			{
				Name:            "orders",
				TenantIDColumn:  "store_id",
				PKColumn:        "id",
				EventTypeColumn: "status",
			},
		},
	}

	m := New(cfg)

	ce := connector.ChangeEvent{
		Table:     "orders",
		Operation: "UPDATE",
		LSN:       "0/16B3F00",
		Timestamp: time.Now().UTC(),
		Row: map[string]interface{}{
			"id":       "order_101",
			"store_id": "store_001",
			"status":   "cancelled",
		},
	}

	event, err := m.Map(ce)
	if err != nil {
		t.Fatalf("map error: %v", err)
	}

	if event.EventType != "order.cancelled" {
		t.Errorf("expected EventType order.cancelled, got %s", event.EventType)
	}
}

