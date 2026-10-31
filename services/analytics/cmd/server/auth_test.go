package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/axyra/analytics/internal/config"
	"github.com/axyra/analytics/internal/handlers"
)

func TestQueryRoutesRejectUntrustedRequests(t *testing.T) {
	cfg := &config.Config{ServiceToken: strings.Repeat("s", 32)}
	router := newRouter(handlers.New(nil, nil, cfg), cfg)
	paths := []string{"orders/summary", "orders/trend", "products/top", "orders/breakdown", "status", "orders/revenue", "customers/summary", "customers/top", "customers/cohorts", "geo/breakdown", "channels/breakdown", "payments/breakdown", "returns/summary", "fulfillment/funnel", "discounts/summary", "tenant/health", "dead-letter", "monitoring", "monitoring/detail", "monitoring/ingestion-rate", "metrics/test", "rebuild"}
	for _, path := range paths {
		for _, token := range []string{"", "Bearer invalid", "Bearer agent-token"} {
			method := http.MethodGet
			if path == "rebuild" {
				method = http.MethodPost
			}
			req := httptest.NewRequest(method, "/v1/analytics/"+path+"?tenant_id=tenant_002", nil)
			req.Header.Set("Authorization", token)
			req.Header.Set("X-Tenant-ID", "tenant_001")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("%s: got %d", path, w.Code)
			}
		}
	}
	for _, path := range []string{"/health", "/v1/agent/ack"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		want := http.StatusOK
		if path != "/health" {
			want = http.StatusUnauthorized
		}
		if w.Code != want {
			t.Errorf("%s: got %d", path, w.Code)
		}
	}
}
