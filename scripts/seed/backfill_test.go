package main

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBackfillIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_BACKFILL_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_BACKFILL_DATABASE_URL to a disposable source database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	// Deliberately fail if this is not an empty test database.
	_, err = pool.Exec(ctx, `CREATE TABLE public.orders(id text PRIMARY KEY,store_id text,updated_at timestamptz,amount numeric);
 INSERT INTO public.orders VALUES ('a','one','2026-09-01',10),('b','one','2026-09-02',20),('c','one','2026-09-03',30),('d','two','2026-09-04',40)`)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DROP TABLE public.orders`)
	snapshot := func() string {
		t.Helper()
		var value string
		if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(o) ORDER BY id)::text FROM public.orders o`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := snapshot()
	var untouched string
	if err := pool.QueryRow(ctx, `SELECT xmin::text FROM public.orders WHERE id='d'`).Scan(&untouched); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		n, err := backfillOrders(ctx, pool, []string{"one"}, 2)
		if err != nil || n != 3 {
			t.Fatalf("count=%d err=%v", n, err)
		}
		if snapshot() != before {
			t.Fatal("business data changed")
		}
	}
	var after string
	if err := pool.QueryRow(ctx, `SELECT xmin::text FROM public.orders WHERE id='d'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != untouched {
		t.Fatal("another store was updated")
	}
	if n, err := backfillOrders(ctx, pool, []string{"missing"}, 2); err != nil || n != 0 {
		t.Fatalf("empty store count=%d err=%v", n, err)
	}
}
