# Axyra Bulk Seed

High-throughput data generator for the reference client PostgreSQL database.  
Uses the PostgreSQL **COPY** protocol (`pgx`) to achieve **500K – 2M rows/second**, making it practical to seed 10M – 1B rows on a development machine in a few minutes.

---

## Quick start

```bash
# From the repo root — ensure the reference DB is running first
docker compose -f deploy/docker-compose.yaml up -d reference-db

# Default: 10 million rows across 5 stores
cd scripts/seed
./run_seed.sh

# 100 million rows, wipe first
./run_seed.sh --rows 100000000 --truncate

# 1 billion rows, 8 parallel workers
./run_seed.sh --rows 1000000000 --workers 8

# Custom store IDs (must match tenants registered in Axyra)
./run_seed.sh --rows 50000000 \
  --stores "acme_corp,globex,initech,umbrella,weyland"
```

---

## What it generates

### Historical chart data

Analytics buckets orders by their source `created_at` in UTC. Later updates and
cancellations adjust that original bucket. Missing or invalid creation timestamps
fall back to the first event timestamp. Monitoring still uses event timestamps.

From `scripts/seed`, for the database-per-store deployment:

```powershell
go run -mod=mod . -dsn "postgres://postgres:postgres@localhost:5433/reference_db" -rows 10000 -stores "store_001" -from "2026-10-01T00:00:00Z" -to "2026-10-31T23:59:59Z"
```

Store 2 uses port 5435 and `-stores "store_002"`. Select the matching Custom
range and Daily interval in Analytics, then refresh after ingestion completes.
Timestamps are sampled uniformly; for larger peaks and dips, run separate daily
windows with different `-rows` counts. These commands add records.

After upgrading from event-time bucketing, rebuild each affected tenant through
`POST /v1/analytics/rebuild?tenant_id=...` while ingestion is idle. This replays
stored raw events using the corrected dates; it does not import source rows that
were never captured. Ensure `orders` is in the agent's PostgreSQL publication
before seeding (restart the agent after initially creating the table).

| Field | Detail |
|---|---|
| `id` | Unique hex order ID |
| `store_id` | One of the configured store IDs (skewed: store 1 ≈ 50%, store 2 ≈ 25%, …) |
| `amount` | Realistic price per product (e.g. ₹199–₹39,999) |
| `currency` | `INR` |
| `status` | `placed / confirmed / shipped / delivered / cancelled / returned` — configurable cancel rate |
| `product_id` | 20 realistic products across Electronics, Footwear, Appliances, Fitness, Home, Accessories |
| `created_at` | Random within the configured time window (default: last 60 days) |

Store weight distribution is intentionally **skewed** (Zipf-like) to simulate uneven tenant sizes, matching the PRD benchmark requirement of "10M records / 100 tenants with uneven tenant sizes".

---

## All flags

```
-dsn      PostgreSQL DSN  (default: postgres://postgres:postgres@localhost:5433/reference_db)
-rows     Total rows to insert  (default: 10_000_000)
-stores   Comma-separated store IDs  (default: store_001,…,store_005)
-cancel   Fraction of orders to mark cancelled, 0–1  (default: 0.12)
-batch    Rows per COPY batch  (default: 50_000)
-workers  Parallel COPY workers  (default: 4)
-from     Earliest created_at, RFC3339  (default: 60 days ago)
-to       Latest created_at, RFC3339  (default: now)
-truncate Truncate orders table before seeding  (default: false)
```

Environment variable overrides (for `run_seed.sh`):

| Variable | Default |
|---|---|
| `SEED_DSN` | `postgres://postgres:postgres@localhost:5433/reference_db` |
| `SEED_ROWS` | `10000000` |
| `SEED_STORES` | `store_001,…,store_005` |
| `SEED_WORKERS` | `4` |
| `SEED_BATCH` | `50000` |
| `SEED_CANCEL_RATE` | `0.12` |

---

## Expected throughput

| Machine | Workers | Approx. rows/s |
|---|---|---|
| MacBook Pro M3 | 4 | 1.2M – 2M |
| Linux dev box (NVMe) | 4 | 800K – 1.5M |
| Linux dev box (HDD) | 2 | 200K – 500K |

At 1M rows/s, **10M rows ≈ 10 seconds**, **1B rows ≈ 17 minutes**.

Increase `-workers` and `-batch` for faster throughput; reduce them if the DB is on a slow disk.

---

## Notes

- The seeder writes **directly to the reference PostgreSQL DB**, bypassing the FastAPI API. This means the **Axyra Agent** will capture the WAL changes automatically if it is running — this is the intended behaviour for the benchmark.
- If the `orders` table does not exist yet (i.e. the FastAPI backend hasn't started), the seeder creates it automatically. The schema matches [`reference-client/backend/app/models.py`](../../reference-client/backend/app/models.py).
- Use `--truncate` to wipe existing rows before a fresh benchmark run.
- Successful COPY batches pause for 5 seconds so the database and WAL consumer can keep up.
- The seeder is idempotent — running it twice **adds** rows rather than replacing them unless `--truncate` is passed.

---

## Running against a different target

To seed against a remote DB or a different port:

```bash
./run_seed.sh \
  --dsn "postgres://user:pass@db-host:5432/reference_db?sslmode=require" \
  --rows 10000000 \
  --workers 8
```

Or using `go run` directly (no binary needed):

```bash
go run bulk_seed.go \
  -dsn  "postgres://postgres:postgres@localhost:5433/reference_db" \
  -rows 10000000
```
