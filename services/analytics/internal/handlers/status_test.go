package handlers

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/axyra/analytics/internal/db"
)

func TestEventActivity(t *testing.T) {
	now := time.Now()
	recent, old := now.Add(-time.Second), now.Add(-time.Hour)
	fast, slow := 2.0, 600.0
	for _, tc := range []struct {
		name  string
		last  *time.Time
		delay *float64
		want  string
	}{
		{"no events", nil, nil, "waiting"},
		{"idle store", &old, &fast, "idle"},
		{"old delayed event is not current lag", &old, &slow, "idle"},
		{"active", &recent, &fast, "active"},
		{"recent delay", &recent, &slow, "delayed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := eventActivity(tc.last, tc.delay, now); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestFreshnessIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_REBUILD_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_REBUILD_DATABASE_URL to disposable PostgreSQL")
	}
	database, err := db.Connect(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	h := &Handler{db: database}
	ctx := context.Background()
	tenant := fmt.Sprintf("fresh-%d", time.Now().UnixNano())
	empty, err := h.tenantFreshness(ctx, tenant)
	if err != nil || empty.LastProcessedAt != nil || empty.LagSeconds != nil || empty.ActivityState != "waiting" {
		t.Fatalf("empty: %+v %v", empty, err)
	}
	// The store processed its last event an hour ago, with a two-second delay.
	_, err = database.Exec(`INSERT INTO raw_events(event_id,agent_id,tenant_id,external_tenant_id,event_type,schema_version,lsn,occurred_at,data)
 VALUES($1,$1,$1,$1,'order.updated',2,'0/1',NOW()-INTERVAL '1 hour 2 seconds','{}');`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.Exec(`INSERT INTO processed_events(event_id,tenant_id,processed_at,status)
 SELECT event_id,tenant_id,occurred_at+INTERVAL '2 seconds','processed' FROM raw_events WHERE tenant_id=$1`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	idle, err := h.tenantFreshness(ctx, tenant)
	if err != nil || idle.ActivityState != "idle" || !idle.IsHealthy || idle.LagSeconds == nil || *idle.LagSeconds != 2 {
		t.Fatalf("idle: %+v %v", idle, err)
	}
	// A genuinely delayed event just finished processing.
	_, err = database.Exec(`UPDATE processed_events SET processed_at=clock_timestamp() WHERE tenant_id=$1`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	delayed, err := h.tenantFreshness(ctx, tenant)
	if err != nil || delayed.ActivityState != "delayed" || delayed.IsHealthy || delayed.LagSeconds == nil || *delayed.LagSeconds < 3600 {
		t.Fatalf("delayed: %+v %v", delayed, err)
	}
}
