package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axyra/analytics/internal/config"
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
