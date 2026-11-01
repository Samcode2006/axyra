package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/axyra/analytics/internal/config"
	"github.com/axyra/analytics/internal/db"
	"github.com/axyra/analytics/internal/models"
)

func TestTenantTrendBreakdown(t *testing.T) {
	dsn := os.Getenv("TEST_METRIC_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_METRIC_DATABASE_URL for temporary PostgreSQL trend fixtures")
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
	_, err = sdb.Exec(`CREATE TEMP TABLE tenants(id text, organization_id text, max_concurrent_queries int);
CREATE TEMP TABLE op_query_latency(tenant_id text, endpoint text, duration_ms double precision, timing_kind text);
CREATE TEMP TABLE order_summary_daily(tenant_id text, bucket date, total_orders bigint, total_revenue numeric, total_discount numeric, new_customer_orders bigint);
CREATE TEMP TABLE order_summary_hourly(tenant_id text, bucket timestamptz, total_orders bigint, total_revenue numeric, total_discount numeric, new_customer_orders bigint);
INSERT INTO tenants VALUES ('a','own',5),('b','own',5),('foreign','other',5);
INSERT INTO order_summary_daily VALUES
('a','2026-11-01',10,100,10,2),('b','2026-11-01',20,300,30,3),
('foreign','2026-11-01',999,99999,0,99),('a','2026-10-01',999,99999,0,99);
INSERT INTO order_summary_hourly SELECT tenant_id,bucket::timestamp AT TIME ZONE 'UTC',total_orders,total_revenue,total_discount,new_customer_orders FROM order_summary_daily;
SET search_path TO pg_temp, public;` + strings.Replace(string(migration), "FUNCTION analytics_scope_tenants", "FUNCTION pg_temp.analytics_scope_tenants", 1))
	if err != nil {
		t.Fatal(err)
	}
	h := New(&db.DB{DB: sdb}, nil, &config.Config{MaxConcurrentQueries: 5})
	for _, interval := range []string{"hour", "day"} {
		for _, grouped := range []bool{false, true} {
			for _, scope := range []string{"organization", "tenant"} {
				t.Run(interval+"/"+scope+map[bool]string{true: "/grouped", false: "/combined"}[grouped], func(t *testing.T) {
					url := "/?interval=" + interval + "&from=2026-11-01T00:00:00Z&to=2026-11-01T23:59:59Z"
					if grouped {
						url += "&group_by=tenant"
					}
					if scope == "tenant" {
						url += "&tenant_id=a"
					}
					r := httptest.NewRequest("GET", url, nil)
					if scope == "organization" {
						r.Header.Set("X-Analytics-Organization", "own")
					}
					w := httptest.NewRecorder()
					h.OrderTrend(w, r)
					if w.Code != 200 {
						t.Fatalf("%d: %s", w.Code, w.Body)
					}
					var result models.TrendResponse
					if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
						t.Fatal(err)
					}
					wantRows, wantOrders, wantRevenue := 1, int64(30), float64(400)
					if grouped && scope == "organization" {
						wantRows = 2
					}
					if scope == "tenant" {
						wantOrders, wantRevenue = 10, 100
					}
					if len(result.Data) != wantRows {
						t.Fatal(result)
					}
					var orders int64
					var revenue float64
					for _, point := range result.Data {
						orders += point.OrderCount
						revenue += point.Revenue
						if grouped && point.TenantID != "a" && point.TenantID != "b" {
							t.Fatalf("unexpected tenant: %v", point)
						}
						if !grouped && point.TenantID != "" {
							t.Fatal("combined contract changed")
						}
						if point.NetRevenue != point.Revenue-point.Discount {
							t.Fatal(point)
						}
					}
					if orders != wantOrders || revenue != wantRevenue {
						t.Fatal(result)
					}
				})
			}
		}
	}
}
