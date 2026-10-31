package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axyra/analytics/internal/config"
	"github.com/axyra/analytics/internal/models"
	"github.com/axyra/analytics/internal/queue"
)

func TestHealth(t *testing.T) {
	cfg := &config.Config{Port: "8082"}
	q := queue.NewMemoryQueue(10)
	defer q.Close()

	h := New(nil, q, cfg)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	h.Health(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var res map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if res["status"] != "ok" {
		t.Errorf("expected status 'ok', got %s", res["status"])
	}
}

func TestIngestWithoutAuth(t *testing.T) {
	cfg := &config.Config{Port: "8082"}
	q := queue.NewMemoryQueue(10)
	defer q.Close()

	h := New(nil, q, cfg)

	req := httptest.NewRequest("POST", "/v1/agent/events", nil)
	w := httptest.NewRecorder()

	h.IngestEvents(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 Unauthorized, got %d", w.Code)
	}
}

func TestAgentAckWithoutAuth(t *testing.T) {
	cfg := &config.Config{Port: "8082"}
	q := queue.NewMemoryQueue(10)
	defer q.Close()

	h := New(nil, q, cfg)

	req := httptest.NewRequest("GET", "/v1/agent/ack", nil)
	w := httptest.NewRecorder()

	h.AgentAck(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 Unauthorized, got %d", w.Code)
	}
}

// TestValidateEvent exercises the schema validation helper directly.
func TestValidateEvent(t *testing.T) {
	tests := []struct {
		name   string
		event  models.EventEnvelope
		wantOK bool
	}{
		{
			name: "valid event",
			event: models.EventEnvelope{
				EventID: "evt_1", ExternalTenantID: "store_001",
				EventType: "order.created", LSN: "0/1A2B", SchemaVersion: 1,
			},
			wantOK: true,
		},
		{
			name:   "missing event_id",
			event:  models.EventEnvelope{ExternalTenantID: "store_001", EventType: "order.created", LSN: "0/1", SchemaVersion: 1},
			wantOK: false,
		},
		{
			name:   "missing external_tenant_id",
			event:  models.EventEnvelope{EventID: "evt_2", EventType: "order.created", LSN: "0/2", SchemaVersion: 1},
			wantOK: false,
		},
		{
			name:   "missing event_type",
			event:  models.EventEnvelope{EventID: "evt_3", ExternalTenantID: "store_001", LSN: "0/3", SchemaVersion: 1},
			wantOK: false,
		},
		{
			name:   "missing lsn",
			event:  models.EventEnvelope{EventID: "evt_4", ExternalTenantID: "store_001", EventType: "order.created", SchemaVersion: 1},
			wantOK: false,
		},
		{
			name:   "unsupported schema_version",
			event:  models.EventEnvelope{EventID: "evt_5", ExternalTenantID: "store_001", EventType: "order.created", LSN: "0/5", SchemaVersion: 99},
			wantOK: false,
		},
		{
			name:   "zero schema_version treated as v1",
			event:  models.EventEnvelope{EventID: "evt_6", ExternalTenantID: "store_001", EventType: "order.created", LSN: "0/6", SchemaVersion: 0},
			wantOK: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := tc.event
			reason := validateEvent(&e)
			if tc.wantOK && reason != "" {
				t.Errorf("expected valid event, got rejection reason: %s", reason)
			}
			if !tc.wantOK && reason == "" {
				t.Errorf("expected rejection but event was accepted")
			}
		})
	}
}
