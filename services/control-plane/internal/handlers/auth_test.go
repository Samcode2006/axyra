package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axyra/control-plane/internal/config"
	"github.com/axyra/control-plane/internal/models"
)

func TestHealth(t *testing.T) {
	cfg := &config.Config{JWTSecret: "test-secret"}
	h := New(nil, cfg)

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

func TestLogin(t *testing.T) {
	cfg := &config.Config{JWTSecret: "test-secret-key-1234567890123456"}
	h := New(nil, cfg)

	body, _ := json.Marshal(models.LoginRequest{
		Username: "admin",
		Password: "password",
	})

	req := httptest.NewRequest("POST", "/v1/auth/login", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.Login(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var res models.LoginResponse
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if res.Token == "" {
		t.Errorf("expected non-empty token")
	}
}

func TestLoginInvalidBody(t *testing.T) {
	cfg := &config.Config{JWTSecret: "test-secret"}
	h := New(nil, cfg)

	req := httptest.NewRequest("POST", "/v1/auth/login", bytes.NewReader([]byte("invalid json")))
	w := httptest.NewRecorder()

	h.Login(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 Bad Request, got %d", w.Code)
	}
}

func TestLoginMissingCredentials(t *testing.T) {
	cfg := &config.Config{JWTSecret: "test-secret"}
	h := New(nil, cfg)

	body, _ := json.Marshal(models.LoginRequest{Username: "", Password: ""})
	req := httptest.NewRequest("POST", "/v1/auth/login", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.Login(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 Bad Request, got %d", w.Code)
	}
}
