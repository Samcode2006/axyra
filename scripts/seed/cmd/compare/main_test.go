package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestComparisonDetectsDifferences(t *testing.T) {
	base := []product{{"A", 2, "1500.00"}, {"B", 1, "100.01"}}
	for _, tc := range []struct {
		name string
		rows []product
		want bool
	}{
		{"decimal formatting", []product{{"A", 2, "1500"}, {"B", 1, "100.010"}}, true},
		{"cent mismatch", []product{{"A", 2, "1500"}, {"B", 1, "100.02"}}, false},
		{"count mismatch", []product{{"A", 3, "1500"}, {"B", 1, "100.01"}}, false},
		{"missing product", []product{{"A", 2, "1500"}}, false},
		{"wrong product", []product{{"C", 2, "1500"}, {"B", 1, "100.01"}}, false},
		{"invalid money", []product{{"A", 2, "invalid"}, {"B", 1, "100.01"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if equal(base, tc.rows) != tc.want {
				t.Fatal("unexpected comparison result")
			}
		})
	}
}

func TestAnalyticsRequestAndScope(t *testing.T) {
	response := `{"tenant_id":"tenant_test","data":[{"product_id":"A","order_count":1,"revenue":1000.01}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing console authentication")
		}
		if req.URL.Path != "/v1/analytics/products/top" || req.URL.Query().Get("tenant_id") != "tenant_test" || req.URL.Query().Get("limit") != "100" || req.URL.Query().Get("from") != "2026-11-01T00:00:00Z" {
			t.Error("incorrect comparison scope")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()
	r := runner{base: server.URL, client: server.Client(), token: "test-token", tenant: "tenant_test", from: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), to: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)}
	rows, err := r.analytics(context.Background(), true)
	if err != nil || !equal(rows, []product{{"A", 1, "1000.01"}}) {
		t.Fatalf("unexpected response: %v %v", rows, err)
	}
	response = `{"tenant_id":"another_tenant","data":[]}`
	if _, err := r.analytics(context.Background(), true); err == nil {
		t.Fatal("accepted wrong tenant")
	}
	response = `{"tenant_id":"tenant_test"}`
	if _, err := r.analytics(context.Background(), true); err == nil {
		t.Fatal("accepted missing data")
	}
}
