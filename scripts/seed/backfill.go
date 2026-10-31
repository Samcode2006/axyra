package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// backfillOrders generates fresh WAL updates for existing rows. Updating a
// column to itself preserves business timestamps while allowing CDC to capture
// the current row. Database UPDATE triggers still run, if any are installed.
func backfillOrders(ctx context.Context, pool *pgxpool.Pool, stores []string, batchSize int) (int64, error) {
	if batchSize <= 0 || len(stores) == 0 {
		return 0, fmt.Errorf("positive batch size and store IDs required")
	}
	for _, store := range stores {
		if store == "" {
			return 0, fmt.Errorf("empty store ID")
		}
	}
	var upper *string
	if err := pool.QueryRow(ctx, `SELECT max(id) FROM public.orders WHERE store_id=ANY($1)`, stores).Scan(&upper); err != nil {
		return 0, err
	}
	if upper == nil {
		return 0, nil
	}
	var cursor *string
	var count int64
	for {
		// The row locks prevent overwriting a concurrent update with an old snapshot.
		// One atomic statement commits each bounded batch; never use SKIP LOCKED,
		// which would leave holes behind the cursor.
		var next *string
		var n int64
		err := pool.QueryRow(ctx, `
   WITH batch AS (
    SELECT id FROM public.orders
    WHERE store_id=ANY($1) AND ($2::text IS NULL OR id>$2) AND id<=$3
    ORDER BY id LIMIT $4 FOR UPDATE
   ), touched AS (
    UPDATE public.orders o SET updated_at=o.updated_at FROM batch b
    WHERE o.id=b.id RETURNING o.id
   ) SELECT max(id),count(*) FROM touched`, stores, cursor, *upper, batchSize).Scan(&next, &n)
		if err != nil {
			return count, err
		}
		count += n
		if n == 0 {
			return count, nil
		}
		cursor = next
	}
}
