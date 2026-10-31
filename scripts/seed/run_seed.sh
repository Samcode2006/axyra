#!/usr/bin/env bash
# run_seed.sh — Seeds all N isolated tenant databases in parallel.
#
# Usage:
#   # Seed all TENANT_COUNT databases (default 5), 1M rows each
#   ./run_seed.sh
#
#   # Custom row count per tenant DB
#   ./run_seed.sh --rows 500000
#
#   # Truncate existing data first
#   ./run_seed.sh --rows 200000 --truncate
#
#   # Override tenant count (without changing .env)
#   TENANT_COUNT=3 ./run_seed.sh
#
# Environment:
#   TENANT_COUNT  Number of tenant databases (default 5)
#   SEED_ROWS     Rows per tenant database (default 1000000)
#   SEED_WORKERS  Parallel COPY workers per DB (default 4)
#   SEED_BATCH    Rows per COPY batch (default 50000)
#   SEED_CANCEL_RATE  Fraction of orders cancelled, 0–1 (default 0.12)
#   DB_HOST       PostgreSQL host (default localhost)
#   DB_BASE_PORT  Port of the first tenant DB (default 5433)

set -euo pipefail
cd "$(dirname "$0")"

# ─── Defaults ─────────────────────────────────────────────────────────────────
TENANT_COUNT="${TENANT_COUNT:-5}"
ROWS="${SEED_ROWS:-1000000}"
WORKERS="${SEED_WORKERS:-4}"
BATCH="${SEED_BATCH:-50000}"
CANCEL_RATE="${SEED_CANCEL_RATE:-0.12}"
DB_HOST="${DB_HOST:-localhost}"
DB_BASE_PORT="${DB_BASE_PORT:-5433}"
EXTRA_FLAGS=()

# ─── Parse flags passed through to bulk_seed ──────────────────────────────────
i=1
while [[ $i -le $# ]]; do
  arg="${!i}"
  case "$arg" in
    --rows)    i=$((i+1)); ROWS="${!i}" ;;
    --workers) i=$((i+1)); WORKERS="${!i}" ;;
    --count)   i=$((i+1)); TENANT_COUNT="${!i}" ;;
    *)         EXTRA_FLAGS+=("$arg") ;;
  esac
  i=$((i+1))
done

# ─── Build binary if stale ────────────────────────────────────────────────────
if [[ ! -f bulk_seed ]] || [[ bulk_seed.go -nt bulk_seed ]]; then
  echo "▶ Building bulk_seed..."
  go build -o bulk_seed .
  echo "▶ Build complete."
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Axyra Multi-Tenant Bulk Seed"
echo "  Tenant databases : $TENANT_COUNT"
echo "  Rows per tenant  : $(printf "%'d" "$ROWS")"
echo "  Workers per DB   : $WORKERS"
echo "  Batch size       : $BATCH"
echo "  Cancel rate      : $(echo "$CANCEL_RATE * 100" | bc | cut -d. -f1)%"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# ─── Seed all tenant databases in parallel ────────────────────────────────────
PIDS=()
for i in $(seq 1 "$TENANT_COUNT"); do
  # Ports: reference-db-1 -> 5433, reference-db-2 -> 5435, ..., reference-db-5 -> 5438
  # But base port is 5433, and ports 5434 is reserved for platform-db, so:
  # DB 1 -> 5433, DB 2 -> 5435, DB 3 -> 5436, DB 4 -> 5437, DB 5 -> 5438
  if [[ $i -eq 1 ]]; then
    PORT=5433
  else
    PORT=$((5433 + i))
  fi

  STORE_ID="store_$(printf "%03d" "$i")"
  DSN="postgres://postgres:postgres@${DB_HOST}:${PORT}/reference_db"

  echo "  ▶ Seeding tenant DB $i ($STORE_ID) on port $PORT..."

  ./bulk_seed \
    -dsn     "$DSN" \
    -rows    "$ROWS" \
    -stores  "$STORE_ID" \
    -workers "$WORKERS" \
    -batch   "$BATCH" \
    -cancel  "$CANCEL_RATE" \
    "${EXTRA_FLAGS[@]+${EXTRA_FLAGS[@]}}" \
    > /tmp/seed_tenant_${i}.log 2>&1 &

  PIDS+=($!)
done

# ─── Wait for all parallel seeds ──────────────────────────────────────────────
echo ""
echo "  ⏳ Seeding $TENANT_COUNT databases in parallel, waiting..."
echo ""

FAILED=0
for i in $(seq 1 "$TENANT_COUNT"); do
  pid=${PIDS[$((i-1))]}
  STORE_ID="store_$(printf "%03d" "$i")"
  if wait "$pid"; then
    echo "  ✅ Tenant $i ($STORE_ID): done"
    tail -1 /tmp/seed_tenant_${i}.log
  else
    echo "  ❌ Tenant $i ($STORE_ID): FAILED — see /tmp/seed_tenant_${i}.log"
    FAILED=$((FAILED+1))
  fi
done

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
if [[ $FAILED -eq 0 ]]; then
  echo "  ✅ All $TENANT_COUNT tenant databases seeded successfully!"
else
  echo "  ❌ $FAILED tenant database(s) failed to seed."
  exit 1
fi
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
