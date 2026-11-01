package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/axyra/analytics/internal/config"
	"github.com/axyra/analytics/internal/db"
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

func TestFetchOrganizationMetricDefinition(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("tenant_id") != "" || r.Header.Get("X-Analytics-Organization") != "orga" {
			t.Errorf("wrong organization scope: %s %v", r.URL, r.Header)
		}
		_, _ = w.Write([]byte(`{"id":"orders","name":"Orders","enabled":true,"config":{"source":"orders","aggregation":"count"}}`))
	}))
	defer upstream.Close()
	h := New(nil, nil, &config.Config{ControlPlaneURL: upstream.URL, ServiceToken: strings.Repeat("s", 32)})
	if _, err := h.fetchMetricDef(context.Background(), "orders", "org:orga"); err != nil {
		t.Fatal(err)
	}
}

// Temporary tables and a session-local copy of the scope function leave existing data untouched.
func TestConfiguredMetricScopeIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_METRIC_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_METRIC_DATABASE_URL for PostgreSQL metric tests")
	}
	sdb, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer sdb.Close()
	sdb.SetMaxOpenConns(1)
	migration, err := os.ReadFile("../db/006_organization_scope.sql")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sdb.Exec(`CREATE TEMP TABLE tenants(id text, organization_id text);
CREATE TEMP TABLE order_state(tenant_id text, order_id text, product_id text, sku text, category text, status text, amount numeric, occurred_at timestamptz, quantity int, has_extended bool);
INSERT INTO tenants VALUES ('ta','orga'),('tb','orga'),('tc','orgb');
INSERT INTO order_state VALUES
('ta','same','same','same','retail','placed',100,'2026-10-31T12:00:00Z',2,true),
('tb','same','same','same','retail','placed',300,'2026-10-31T12:00:00Z',3,true),
('tb','two','same','same','retail','placed',300,'2026-10-31T12:00:00Z',1,true),
('tb','three','same','same','retail','shipped',300,'2026-10-31T12:00:00Z',9,false),
('tc','other','same','same','retail','placed',99999,'2026-10-31T12:00:00Z',1,true),
('ta','old','same','same','retail','placed',55555,'2026-09-01T12:00:00Z',1,true);
SET search_path TO pg_temp, public;` + strings.Replace(string(migration), "FUNCTION analytics_scope_tenants", "FUNCTION pg_temp.analytics_scope_tenants", 1))
	if err != nil {
		t.Fatal(err)
	}
	h := New(&db.DB{DB: sdb}, nil, &config.Config{})
	from := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	to := from.Add(24*time.Hour - time.Nanosecond)
	for _, tc := range []struct{ name, scope, cfg, want string }{
		{"tenant", "ta", `{"aggregation":"sum","field":"amount"}`, "100"},
		{"organization sum", "org:orga", `{"aggregation":"sum","field":"amount"}`, "1000"},
		{"weighted average", "org:orga", `{"aggregation":"avg","field":"amount"}`, "250"},
		{"count", "org:orga", `{"aggregation":"count"}`, "4"},
		{"distinct orders", "org:orga", `{"aggregation":"count_distinct","field":"order_id"}`, "4"},
		{"distinct products", "org:orga", `{"aggregation":"count_distinct","field":"product_id"}`, "2"},
		{"global dimension", "org:orga", `{"aggregation":"count_distinct","field":"status"}`, "2"},
		{"extended only", "org:orga", `{"aggregation":"sum","field":"quantity"}`, "6"},
		{"filtered", "org:orga", `{"aggregation":"sum","field":"amount","filters":[{"field":"status","op":"eq","value":"placed"}]}`, "700"},
		{"empty", "org:unknown", `{"aggregation":"count"}`, "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg metricConfigShape
			if err := json.Unmarshal([]byte(tc.cfg), &cfg); err != nil {
				t.Fatal(err)
			}
			cfg.Source = "orders"
			result, err := h.executeMetric(context.Background(), tc.scope, &metricDefinitionResp{Config: cfg}, from, to)
			if err != nil {
				t.Fatal(err)
			}
			row := result.(map[string]interface{})
			for _, v := range row {
				// NUMERIC averages include scale. Compare numerically through PostgreSQL.
				var equal bool
				if err := sdb.QueryRow("SELECT $1::numeric = $2::numeric", fmt.Sprint(v), tc.want).Scan(&equal); err != nil || !equal {
					t.Fatalf("got %v want %s (%v)", result, tc.want, err)
				}
			}
		})
	}
	for _, tc := range []struct {
		group string
		rows  int
	}{{"product_id", 2}, {"category", 1}} {
		result, err := h.executeMetric(context.Background(), "org:orga", &metricDefinitionResp{Config: metricConfigShape{Source: "orders", Aggregation: "sum", Field: "amount", GroupBy: []string{tc.group}}}, from, to)
		if err != nil {
			t.Fatal(err)
		}
		rows := result.([]map[string]interface{})
		if len(rows) != tc.rows {
			t.Fatalf("%s: %v", tc.group, rows)
		}
		if tc.group == "product_id" && (rows[0]["tenant_id"] != "ta" || rows[1]["tenant_id"] != "tb") {
			t.Fatalf("tenant-local products merged: %v", rows)
		}
	}
}
