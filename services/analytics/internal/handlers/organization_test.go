package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/axyra/analytics/internal/config"
	"github.com/axyra/analytics/internal/db"
	"github.com/axyra/analytics/internal/models"
	"github.com/axyra/analytics/internal/worker"
)

func TestOrganizationDashboardIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_ORGANIZATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_ORGANIZATION_DATABASE_URL to disposable PostgreSQL")
	}
	database, err := db.Connect(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	_, err = database.Exec(`CREATE TABLE IF NOT EXISTS tenants(id text PRIMARY KEY,organization_id text,max_concurrent_queries int DEFAULT 5);
 CREATE TABLE IF NOT EXISTS agents(id text PRIMARY KEY,status text,last_seen_at timestamptz);`)
	if err != nil {
		t.Fatal(err)
	}
	org := fmt.Sprintf("org_%d", time.Now().UnixNano())
	writer := worker.New(database, nil)
	ctx := context.Background()
	for i, amounts := range [][]float64{{100}, {300, 300, 300}, {99999}} {
		tenant := fmt.Sprintf("%s_t%d", org, i)
		owner := org
		if i == 2 {
			owner = org + "_other"
		}
		if _, err = database.Exec(`INSERT INTO tenants(id,organization_id) VALUES($1,$2)`, tenant, owner); err != nil {
			t.Fatal(err)
		}
		for j, amount := range amounts {
			e := &models.EventEnvelope{EventID: fmt.Sprintf("%s_%d", tenant, j), AgentID: tenant, TenantID: tenant, ExternalTenantID: tenant, SchemaVersion: 2, EventType: "order.created", OccurredAt: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC), Data: map[string]interface{}{"order_id": fmt.Sprint(j), "amount": amount, "status": "placed", "customer_id": "same_customer", "product_id": "same_product", "quantity": 1, "payment_method": "card", "payment_status": "paid", "country": "IN", "channel": "web"}}
			if err = writer.ProcessEvent(ctx, e); err != nil {
				t.Fatal(err)
			}
		}
	}
	h := New(database, nil, &config.Config{MaxConcurrentQueries: 5})
	call := func(fn http.HandlerFunc, extra string) map[string]any {
		t.Helper()
		r := httptest.NewRequest("GET", "/?from=2026-10-31T00:00:00Z&to=2026-10-31T23:59:59Z"+extra, nil)
		r.Header.Set("X-Analytics-Organization", org)
		out := httptest.NewRecorder()
		fn(out, r)
		if out.Code != 200 {
			t.Fatalf("handler failed: %d %s", out.Code, out.Body.String())
		}
		var value map[string]any
		if err := json.Unmarshal(out.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	summary := call(h.OrderSummary, "")
	if summary["total_orders"] != float64(4) || summary["total_revenue"] != float64(1000) {
		t.Fatal(summary)
	}
	revenue := call(h.OrderRevenue, "")
	if revenue["avg_order_value"] != float64(250) {
		t.Fatal(revenue)
	}
	customers := call(h.CustomerSummaryHandler, "")
	if customers["total_customers"] != float64(2) || customers["avg_ltv"] != float64(500) || customers["avg_orders_per_customer"] != float64(2) {
		t.Fatal(customers)
	}
	for _, interval := range []string{"hour", "day"} {
		trend := call(h.OrderTrend, "&interval="+interval)
		rows := trend["data"].([]any)
		if len(rows) != 1 || rows[0].(map[string]any)["order_count"] != float64(4) {
			t.Fatal(trend)
		}
	}
	buyers := call(h.TopBuyers, "")["data"].([]any)
	if len(buyers) != 2 || buyers[0].(map[string]any)["customer_id"] == buyers[1].(map[string]any)["customer_id"] {
		t.Fatal(buyers)
	}
	for _, fn := range []http.HandlerFunc{h.TopProducts, h.CustomerCohorts, h.GeoBreakdown, h.ChannelBreakdown, h.PaymentBreakdown, h.ReturnsSummary, h.FulfillmentFunnel, h.DiscountSummary, h.OrderBreakdown, h.Status} {
		call(fn, "")
	}
	// Pagination never participates in scope expansion; an additional tenant is
	// picked up by the next query without rebuilding a client-side tenant list.
	if _, err = database.Exec(`INSERT INTO tenants(id,organization_id) SELECT $1||n,$2 FROM generate_series(1,205) n`, org+"_empty_", org); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = database.QueryRow(`SELECT COUNT(*) FROM analytics_scope_tenants($1)`, "org:"+org).Scan(&count); err != nil || count != 207 {
		t.Fatalf("scope count %d err %v", count, err)
	}
}
