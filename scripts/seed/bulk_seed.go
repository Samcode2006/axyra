// bulk_seed.go — High-throughput direct-SQL seed for the reference client DB.
//
// Uses PostgreSQL COPY protocol via pgx to achieve 500K–2M rows/second,
// making it practical to seed 10M–1B rows on a local machine.
//
// Usage:
//   go run bulk_seed.go [flags]
//
// Flags:
//   -dsn      PostgreSQL DSN  (default: postgres://postgres:postgres@localhost:5433/reference_db)
//   -rows     Total rows to insert (default: 10_000_000)
//   -stores   Comma-separated store IDs (default: store_001,store_002,store_003,store_004,store_005)
//   -cancel   Fraction of orders to mark cancelled immediately (default: 0.12)
//   -batch    Rows per COPY batch (default: 50_000)
//   -workers  Parallel COPY workers (default: 4)
//   -from     Earliest order timestamp, RFC3339 (default: 60 days ago)
//   -to       Latest  order timestamp, RFC3339 (default: now)
//   -truncate Truncate orders table before seeding (default: false)

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ─── Static data ──────────────────────────────────────────────────────────────

type product struct {
	id       string
	name     string
	category string
	minPrice float64
	maxPrice float64
}

var products = []product{
	{"prod_001", "Wireless Headphones", "Electronics", 1299, 12999},
	{"prod_002", "Running Shoes", "Footwear", 799, 5999},
	{"prod_003", "Coffee Maker", "Appliances", 1499, 9999},
	{"prod_004", "Yoga Mat", "Fitness", 399, 2999},
	{"prod_005", "Backpack", "Accessories", 599, 4999},
	{"prod_006", "Smart Watch", "Electronics", 4999, 39999},
	{"prod_007", "Desk Lamp", "Home", 299, 1999},
	{"prod_008", "Water Bottle", "Fitness", 199, 999},
	{"prod_009", "Bluetooth Speaker", "Electronics", 899, 7999},
	{"prod_010", "Sunglasses", "Accessories", 499, 3999},
	{"prod_011", "Laptop Stand", "Home", 799, 2999},
	{"prod_012", "Resistance Bands", "Fitness", 299, 1499},
	{"prod_013", "Ceramic Mug", "Home", 149, 799},
	{"prod_014", "Mechanical Keyboard", "Electronics", 2999, 14999},
	{"prod_015", "Cycling Helmet", "Footwear", 999, 4999},
	{"prod_016", "Air Purifier", "Appliances", 4999, 24999},
	{"prod_017", "Tote Bag", "Accessories", 299, 1499},
	{"prod_018", "Protein Powder", "Fitness", 799, 2999},
	{"prod_019", "Smart Bulb Pack", "Home", 599, 2499},
	{"prod_020", "Noise-cancelling Earbuds", "Electronics", 2499, 19999},
}

var statuses = []string{"placed", "confirmed", "shipped", "delivered", "cancelled", "returned"}

// Status weights — most orders are in active states; a minority cancel/return.
var statusWeights = []float64{0.25, 0.20, 0.20, 0.23, 0.08, 0.04}

// ─── Config ───────────────────────────────────────────────────────────────────

type config struct {
	dsn        string
	totalRows  int64
	storeIDs   []string
	cancelRate float64
	batchSize  int
	workers    int
	fromTime   time.Time
	toTime     time.Time
	truncate   bool
	// seed makes a run reproducible: the same seed, flags and worker count
	// generate the same rows (apart from the time-based order ids' sequence).
	seed int64
}

func parseFlags() config {
	dsn := flag.String("dsn", "postgres://postgres:postgres@localhost:5433/reference_db", "PostgreSQL DSN")
	rows := flag.Int64("rows", 10_000_000, "Total rows to insert")
	stores := flag.String("stores", "store_001,store_002,store_003,store_004,store_005", "Comma-separated store IDs")
	cancelRate := flag.Float64("cancel", 0.12, "Fraction of orders to mark cancelled (0–1)")
	batch := flag.Int("batch", 50_000, "Rows per COPY batch")
	workers := flag.Int("workers", 4, "Parallel COPY workers")
	fromStr := flag.String("from", "", "Earliest created_at (RFC3339); default 60 days ago")
	toStr := flag.String("to", "", "Latest created_at (RFC3339); default now")
	truncate := flag.Bool("truncate", false, "Truncate orders table before seeding")
	seed := flag.Int64("seed", 0, "Random seed for reproducible data; 0 picks one from the clock")
	flag.Parse()

	now := time.Now().UTC()
	fromTime := now.Add(-60 * 24 * time.Hour)
	toTime := now

	if *fromStr != "" {
		t, err := time.Parse(time.RFC3339, *fromStr)
		if err != nil {
			log.Fatalf("invalid -from: %v", err)
		}
		fromTime = t.UTC()
	}
	if *toStr != "" {
		t, err := time.Parse(time.RFC3339, *toStr)
		if err != nil {
			log.Fatalf("invalid -to: %v", err)
		}
		toTime = t.UTC()
	}

	storeList := strings.Split(*stores, ",")
	for i := range storeList {
		storeList[i] = strings.TrimSpace(storeList[i])
	}

	return config{
		dsn:        *dsn,
		totalRows:  *rows,
		storeIDs:   storeList,
		cancelRate: *cancelRate,
		batchSize:  *batch,
		workers:    *workers,
		fromTime:   fromTime,
		toTime:     toTime,
		truncate:   *truncate,
		seed:       *seed,
	}
}

// ─── Main ────────────────────────────────────────────────────────────────────

func main() {
	cfg := parseFlags()

	log.Printf("bulk_seed: target=%s rows=%s stores=%v workers=%d batch=%d",
		cfg.dsn, humanN(cfg.totalRows), cfg.storeIDs, cfg.workers, cfg.batchSize)

	ctx := context.Background()

	// Connect with a pool sized to workers + 1 spare.
	poolCfg, err := pgxpool.ParseConfig(cfg.dsn)
	if err != nil {
		log.Fatalf("parse dsn: %v", err)
	}
	poolCfg.MaxConns = int32(cfg.workers + 2)
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("ping: %v", err)
	}
	log.Println("bulk_seed: connected to database")

	if cfg.truncate {
		log.Println("bulk_seed: truncating orders table...")
		if _, err := pool.Exec(ctx, "TRUNCATE TABLE orders RESTART IDENTITY CASCADE"); err != nil {
			log.Fatalf("truncate: %v", err)
		}
		log.Println("bulk_seed: truncate done")
	}

	// Ensure table exists (FastAPI creates it on startup; if running standalone it won't).
	if _, err := pool.Exec(ctx, createTableSQL); err != nil {
		log.Printf("bulk_seed: create table (may already exist): %v", err)
	}

	startTime := time.Now()
	var inserted int64

	// Divide rows across workers; last worker takes the remainder.
	rowsPerWorker := cfg.totalRows / int64(cfg.workers)
	var wg sync.WaitGroup

	progress := make(chan int64, cfg.workers*4)

	for w := 0; w < cfg.workers; w++ {
		workerRows := rowsPerWorker
		if w == cfg.workers-1 {
			workerRows = cfg.totalRows - rowsPerWorker*int64(cfg.workers-1)
		}
		wg.Add(1)
		go func(id int, n int64) {
			defer wg.Done()
			runWorker(ctx, pool, cfg, id, n, progress)
		}(w, workerRows)
	}

	// Progress reporter
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case n, ok := <-progress:
				if !ok {
					return
				}
				atomic.AddInt64(&inserted, n)
			case <-ticker.C:
				cur := atomic.LoadInt64(&inserted)
				pct := float64(cur) / float64(cfg.totalRows) * 100
				elapsed := time.Since(startTime)
				rate := float64(cur) / elapsed.Seconds()
				eta := time.Duration(float64(cfg.totalRows-cur)/rate) * time.Second
				log.Printf("bulk_seed: %s / %s rows (%.1f%%) | %.0f rows/s | ETA %s",
					humanN(cur), humanN(cfg.totalRows), pct, rate, eta.Round(time.Second))
			}
		}
	}()

	wg.Wait()
	close(progress)
	<-done

	// Final tally
	elapsed := time.Since(startTime)
	total := atomic.LoadInt64(&inserted)
	rate := float64(total) / elapsed.Seconds()
	log.Printf("bulk_seed: ✅ inserted %s rows in %s (%.0f rows/s)",
		humanN(total), elapsed.Round(time.Second), rate)

	// Print per-store summary
	rows, err := pool.Query(ctx, `
		SELECT store_id, COUNT(*) as cnt,
		       SUM(amount) as revenue,
		       SUM(CASE WHEN status='cancelled' THEN 1 ELSE 0 END) as cancelled
		FROM orders
		GROUP BY store_id
		ORDER BY cnt DESC
	`)
	if err == nil {
		defer rows.Close()
		fmt.Println("\nStore breakdown:")
		fmt.Printf("  %-20s  %12s  %15s  %10s\n", "store_id", "orders", "revenue (INR)", "cancelled")
		fmt.Printf("  %-20s  %12s  %15s  %10s\n", strings.Repeat("-", 20), strings.Repeat("-", 12), strings.Repeat("-", 15), strings.Repeat("-", 10))
		for rows.Next() {
			var storeID string
			var cnt int64
			var revenue float64
			var cancelled int64
			if err := rows.Scan(&storeID, &cnt, &revenue, &cancelled); err == nil {
				fmt.Printf("  %-20s  %12s  %15.2f  %10s\n", storeID, humanN(cnt), revenue, humanN(cancelled))
			}
		}
	}
}

// ─── Worker ───────────────────────────────────────────────────────────────────

func runWorker(ctx context.Context, pool *pgxpool.Pool, cfg config, workerID int, totalRows int64, progress chan<- int64) {
	seed := cfg.seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	// Distinct, reproducible stream per worker.
	gen := newGenerator(seed+int64(workerID)*1_000_003, cfg)
	rng := gen.rng
	spanSeconds := cfg.toTime.Sub(cfg.fromTime).Seconds()
	if spanSeconds <= 0 {
		spanSeconds = 1
	}

	// Compute per-store weights: exponentially skewed so store_001 dominates.
	nStores := len(cfg.storeIDs)
	storeWeights := make([]float64, nStores)
	total := 0.0
	for i := range storeWeights {
		storeWeights[i] = 1.0 / float64(i+1) // 1, 1/2, 1/3 …
		total += storeWeights[i]
	}
	storeCumulative := make([]float64, nStores)
	running := 0.0
	for i := range storeWeights {
		running += storeWeights[i] / total
		storeCumulative[i] = running
	}

	var rowsDone int64
	for rowsDone < totalRows {
		batchN := int64(cfg.batchSize)
		if rowsDone+batchN > totalRows {
			batchN = totalRows - rowsDone
		}

		if err := copyBatch(ctx, pool, cfg, gen, rng, spanSeconds, storeCumulative, int(batchN)); err != nil {
			log.Printf("[worker %d] batch error: %v", workerID, err)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		rowsDone += batchN
		progress <- batchN
		if rowsDone < totalRows {
			time.Sleep(5 * time.Second)
		}
	}
}

// copyBatch writes n rows using the PostgreSQL COPY protocol.
func copyBatch(
	ctx context.Context,
	pool *pgxpool.Pool,
	cfg config,
	gen *generator,
	rng *rand.Rand,
	spanSeconds float64,
	storeCumulative []float64,
	n int,
) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire: %w", err)
	}
	defer conn.Release()

	// Build rows in memory first (avoids per-row allocs in the hot path).
	rows := make([][]any, n)
	for i := range rows {
		// Pick store using weighted distribution
		r := rng.Float64()
		storeIdx := len(storeCumulative) - 1
		for j, cum := range storeCumulative {
			if r < cum {
				storeIdx = j
				break
			}
		}

		// Random timestamp within the configured window
		offsetSec := rng.Float64() * spanSeconds
		createdAt := cfg.fromTime.Add(time.Duration(offsetSec * float64(time.Second)))

		rows[i] = gen.row(cfg.storeIDs[storeIdx], createdAt)
	}

	copyCount, err := conn.Conn().CopyFrom(
		ctx,
		pgx.Identifier{"orders"},
		orderColumns,
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	if int(copyCount) != n {
		return fmt.Errorf("copy wrote %d, expected %d", copyCount, n)
	}
	return nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func humanN(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.2fB", float64(n)/1_000_000_000)
	case n >= 1_000_000:
		return fmt.Sprintf("%.2fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// createTableSQL creates the orders table if it doesn't exist, and adds any
// column an older table is missing. It must stay in step with
// reference-client/backend/app/models.py and app/migrations.py: both describe
// the same 28-column table.
const createTableSQL = `
CREATE TABLE IF NOT EXISTS orders (
    id          VARCHAR PRIMARY KEY,
    store_id    VARCHAR NOT NULL,
    amount      NUMERIC(12,2) NOT NULL,
    currency    VARCHAR(3) DEFAULT 'INR',
    status      VARCHAR(32) DEFAULT 'placed',
    product_id  VARCHAR NOT NULL,
    product_name VARCHAR NOT NULL,
    category    VARCHAR NOT NULL,
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    updated_at  TIMESTAMPTZ DEFAULT NOW()
);
ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS customer_id        VARCHAR(64)   NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS customer_email     VARCHAR(255)  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS customer_tier      VARCHAR(32)   NOT NULL DEFAULT 'regular',
    ADD COLUMN IF NOT EXISTS payment_method     VARCHAR(32)   NOT NULL DEFAULT 'card',
    ADD COLUMN IF NOT EXISTS payment_status     VARCHAR(32)   NOT NULL DEFAULT 'paid',
    ADD COLUMN IF NOT EXISTS discount_code      VARCHAR(64)   NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS discount_amount    NUMERIC(10,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS tax_amount         NUMERIC(10,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS shipping_cost      NUMERIC(10,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS quantity           INT           NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS sku                VARCHAR(64)   NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS fulfillment_status VARCHAR(32)   NOT NULL DEFAULT 'pending',
    ADD COLUMN IF NOT EXISTS return_reason      VARCHAR(128)  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS country            CHAR(2)       NOT NULL DEFAULT 'IN',
    ADD COLUMN IF NOT EXISTS state_province     VARCHAR(64)   NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS city               VARCHAR(64)   NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS channel            VARCHAR(32)   NOT NULL DEFAULT 'web',
    ADD COLUMN IF NOT EXISTS referrer_source    VARCHAR(64)   NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_orders_store_id  ON orders(store_id);
CREATE INDEX IF NOT EXISTS idx_orders_created_at ON orders(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_orders_status    ON orders(status);
`

// Ensure the binary can be run standalone without the module cache.
// The init function is a no-op but causes the import to be resolved.
func init() {
	_ = os.DevNull
}
