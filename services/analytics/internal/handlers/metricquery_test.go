package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/axyra/analytics/internal/config"
)

func TestFetchMetricDefinition(t *testing.T) {
	secret := strings.Repeat("s", 32)
	for _, status := range []int{200, 401, 404, 500, 302} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+secret {
					t.Error("missing service authentication")
				}
				if r.URL.Path != "/internal/metric-definitions/custom_orders" || r.URL.Query().Get("tenant_id") != "tenant_002" {
					t.Errorf("wrong metric scope: %s", r.URL)
				}
				w.WriteHeader(status)
				if status == 200 {
					_, _ = w.Write([]byte(`{"id":"custom_orders","name":"Orders","enabled":true,"config":{"source":"orders","aggregation":"count"}}`))
				}
			}))
			defer upstream.Close()
			h := New(nil, nil, &config.Config{ControlPlaneURL: upstream.URL, ServiceToken: secret})
			def, err := h.fetchMetricDef(context.Background(), "custom_orders", "tenant_002")
			if status == 200 {
				if err != nil || def.Config.Aggregation != "count" {
					t.Fatalf("definition not loaded: %v", err)
				}
			} else if err == nil {
				t.Fatal("upstream failure accepted")
			}
			if errors.Is(err, errMetricNotFound) != (status == 404) {
				t.Fatalf("incorrect error classification: %v", err)
			}
		})
	}
}
