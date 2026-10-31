package handlers

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/axyra/analytics/internal/aggregate"
	"github.com/axyra/analytics/internal/db"
	"github.com/axyra/analytics/internal/models"
	"github.com/axyra/analytics/internal/worker"
)

// Use a disposable database: migrations and fixture tables are created here.
func TestRebuildIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_REBUILD_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_REBUILD_DATABASE_URL to a disposable PostgreSQL database")
	}
	database, err := db.Connect(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`CREATE TABLE IF NOT EXISTS agents (id text PRIMARY KEY, status text, last_seen_at timestamptz)`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	tenant := fmt.Sprintf("replay-%d", time.Now().UnixNano())
	h := &Handler{db: database}
	w := worker.New(database, nil)
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	event := func(n int, order, status string, amount float64) *models.EventEnvelope {
		return &models.EventEnvelope{EventID: fmt.Sprintf("%s-%d", tenant, n), TenantID: tenant, AgentID: tenant, ExternalTenantID: tenant, EventType: "order.updated", SchemaVersion: 2,
			OccurredAt: base.Add(-time.Duration(n) * time.Hour), Data: map[string]interface{}{"order_id": order, "amount": amount, "status": status, "customer_id": "customer", "product_id": "product", "created_at": base.Format(time.RFC3339), "discount_amount": 5.0, "payment_status": "paid", "channel": "web", "city": "Delhi"}}
	}
	for _, e := range []*models.EventEnvelope{event(1, "a", "placed", 100), event(2, "b", "placed", 200), event(3, "a", "cancelled", 100), event(4, "b", "delivered", 250), event(4, "b", "delivered", 250)} {
		if err := w.ProcessEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := func() map[string]string {
		t.Helper()
		result := map[string]string{}
		for _, table := range aggregate.TenantTables {
			var value string
			query := fmt.Sprintf(`SELECT COALESCE(jsonb_agg(v ORDER BY v::text), '[]'::jsonb)::text FROM (SELECT to_jsonb(t) - 'updated_at' AS v FROM %s t WHERE tenant_id=$1) s`, table)
			if err := database.QueryRow(query, tenant).Scan(&value); err != nil {
				t.Fatal(err)
			}
			result[table] = value
		}
		return result
	}
	before := snapshot()
	for i := 0; i < 2; i++ {
		count, err := h.rebuildTenant(ctx, tenant)
		if err != nil || count != 4 {
			t.Fatalf("rebuild count=%d err=%v", count, err)
		}
		if after := snapshot(); !reflect.DeepEqual(before, after) {
			t.Fatalf("replay differs: before=%v after=%v", before, after)
		}
	}
	var total, active, cancelled int
	var revenue float64
	if err := database.QueryRow(`SELECT total_orders,active_orders,cancelled_orders,total_revenue FROM order_summary_daily WHERE tenant_id=$1`, tenant).Scan(&total, &active, &cancelled, &revenue); err != nil {
		t.Fatal(err)
	}
	if total != 2 || active != 1 || cancelled != 1 || revenue != 250 {
		t.Fatalf("unexpected totals: %d %d %d %.2f", total, active, cancelled, revenue)
	}

	// Both entry points must wait for the same lock, and cancellation must release
	// their transactions without damaging the existing aggregates.
	lock, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback()
	if err := db.LockTenant(ctx, lock, tenant); err != nil {
		t.Fatal(err)
	}
	for _, run := range []func(context.Context) error{
		func(c context.Context) error { return w.ProcessEvent(c, event(5, "c", "placed", 50)) },
		func(c context.Context) error { _, err := h.rebuildTenant(c, tenant); return err },
	} {
		c, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		err := run(c)
		cancel()
		if err == nil || c.Err() != context.DeadlineExceeded {
			t.Fatalf("expected lock wait cancellation, got %v", err)
		}
	}
	other, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	c, cancel := context.WithTimeout(ctx, time.Second)
	err = db.LockTenant(c, other, tenant+"-other")
	cancel()
	other.Rollback()
	if err != nil {
		t.Fatalf("unrelated tenant blocked: %v", err)
	}
	if err := lock.Commit(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("cancelled work changed aggregates")
	}
	if err := w.ProcessEvent(ctx, event(5, "c", "placed", 50)); err != nil {
		t.Fatal(err)
	}
	before = snapshot()
	if _, err := h.rebuildTenant(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("post-lock ingestion differs from replay")
	}

	// Race multiple rebuilds against duplicate deliveries of the same new event.
	start := make(chan struct{})
	done := make(chan error, 6)
	for i := 0; i < 6; i++ {
		go func(i int) {
			<-start
			if i%2 == 0 {
				_, err := h.rebuildTenant(ctx, tenant)
				done <- err
			} else {
				done <- w.ProcessEvent(ctx, event(6, "d", "placed", 75))
			}
		}(i)
	}
	close(start)
	for i := 0; i < 6; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if err := database.QueryRow(`SELECT sum(total_orders),sum(total_revenue) FROM order_summary_daily WHERE tenant_id=$1`, tenant).Scan(&total, &revenue); err != nil {
		t.Fatal(err)
	}
	if total != 4 || revenue != 375 {
		t.Fatalf("concurrent ingestion/replay totals: %d %.2f", total, revenue)
	}
	before = snapshot()
	if _, err := h.rebuildTenant(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("concurrent replay differs")
	}

	// Malformed ledger entries must roll back the deletion and all partial replay.
	_, err = database.Exec(`INSERT INTO raw_events(event_id,agent_id,tenant_id,external_tenant_id,event_type,schema_version,lsn,occurred_at,data) VALUES($1,$2,$2,$2,'order.updated',2,'',NOW(),'[]')`, tenant+"-bad", tenant)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.rebuildTenant(ctx, tenant); err == nil {
		t.Fatal("malformed ledger was silently accepted")
	}
	if !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("failed replay changed aggregates")
	}
}
