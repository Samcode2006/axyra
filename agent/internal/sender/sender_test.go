package sender

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/axyra/agent/internal/config"
	"github.com/axyra/agent/internal/mapper"
)

func TestSenderBatchDelivery(t *testing.T) {
	var mu sync.Mutex
	receivedEvents := make([]mapper.AxyraEvent, 0)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agent/events" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}

		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Events []mapper.AxyraEvent `json:"events"`
		}
		_ = json.Unmarshal(body, &payload)

		mu.Lock()
		receivedEvents = append(receivedEvents, payload.Events...)
		mu.Unlock()

		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	cfg := &config.Config{
		AxyraEndpoint: server.URL,
		AgentToken:    "test-token",
		BatchSize:     2,
		FlushInterval: 50 * time.Millisecond,
	}

	snd := New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	snd.Start(ctx)

	// Send 2 events
	snd.In() <- mapper.AxyraEvent{
		EventID:          "evt_01",
		ExternalTenantID: "store_001",
		EventType:        "order.created",
	}
	snd.In() <- mapper.AxyraEvent{
		EventID:          "evt_02",
		ExternalTenantID: "store_001",
		EventType:        "order.created",
	}

	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	count := len(receivedEvents)
	mu.Unlock()

	if count != 2 {
		t.Fatalf("expected 2 received events, got %d", count)
	}

	_ = snd.Close()
}
