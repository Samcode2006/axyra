package queue

import (
	"context"
	"testing"
	"time"

	"github.com/axyra/analytics/internal/models"
)

func TestMemoryQueuePushPop(t *testing.T) {
	q := NewMemoryQueue(10)
	defer q.Close()

	ctx := context.Background()
	events := []models.EventEnvelope{
		{
			EventID:          "evt_1",
			AgentID:          "agt_1",
			ExternalTenantID: "store_001",
			TenantID:         "tenant_001",
			EventType:        "order.created",
			SchemaVersion:    1,
			OccurredAt:       time.Now().UTC(),
			LSN:              "0/16B3D88",
			Data: map[string]interface{}{
				"order_id": "ord_101",
				"amount":   1500.0,
			},
		},
	}

	err := q.Push(ctx, events)
	if err != nil {
		t.Fatalf("push error: %v", err)
	}

	popped, err := q.Pop(ctx, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("pop error: %v", err)
	}
	if popped == nil {
		t.Fatalf("expected popped event, got nil")
	}

	if popped.EventID != "evt_1" {
		t.Errorf("expected EventID 'evt_1', got '%s'", popped.EventID)
	}
}
