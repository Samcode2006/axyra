package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/axyra/control-plane/internal/config"
	"github.com/axyra/control-plane/internal/db"
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

// TestLogin needs a real database because login now verifies stored users.
// Set TEST_DATABASE_URL to a disposable Postgres to run it.
func TestLogin(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	database, err := db.Connect(dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer database.Close()
	if err := db.SeedDefaultData(database, db.SeedOptions{
		AgentToken: "axyra_agt_test", AdminUsername: "admin", AdminPassword: "test-password-123",
		OperatorUsername: "operator", OperatorPassword: "test-password-456",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cfg := &config.Config{JWTSecret: "test-secret-key-1234567890123456"}
	h := New(database, cfg)

	login := func(user, pass string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(models.LoginRequest{Username: user, Password: pass})
		w := httptest.NewRecorder()
		h.Login(w, httptest.NewRequest("POST", "/v1/auth/login", bytes.NewReader(body)))
		return w
	}

	if w := login("admin", "wrong-password"); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: expected 401, got %d", w.Code)
	}
	w := login("admin", "test-password-123")
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	var res models.LoginResponse
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if res.Token == "" || res.User.OrganizationID == nil || *res.User.OrganizationID != "org_demo" {
		t.Errorf("expected token for an admin bound to org_demo, got %+v", res.User)
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
