#!/usr/bin/env bash
# run_seed.sh — Convenience wrapper around bulk_seed
#
# Builds the Go binary if needed, then seeds the reference client DB.
# Adjust DSN and flags to match your environment.
#
# Quick-start examples:
#
#   # 10 million rows across 5 stores (default)
#   ./run_seed.sh
#
#   # 100 million rows, truncate existing data first
#   ./run_seed.sh --rows 100000000 --truncate
#
#   # 1 billion rows, 8 workers, custom DSN
#   ./run_seed.sh --rows 1000000000 --workers 8 \
#                 --dsn "postgres://postgres:postgres@localhost:5433/reference_db"
#
#   # Custom store IDs to match existing Axyra tenant registrations
#   ./run_seed.sh --rows 50000000 \
#                 --stores "acme_corp,globex,initech,umbrella,weyland"
#
# All flags are passed through to bulk_seed. Run with -h for full flag list:
#   go run bulk_seed.go -h

set -euo pipefail
cd "$(dirname "$0")"

# ─── Defaults ────────────────────────────────────────────────────────────────
DSN="${SEED_DSN:-postgres://postgres:postgres@localhost:5433/reference_db}"
STORES="${SEED_STORES:-store_001,store_002,store_003,store_004,store_005}"
ROWS="${SEED_ROWS:-10000000}"
WORKERS="${SEED_WORKERS:-4}"
BATCH="${SEED_BATCH:-50000}"
CANCEL_RATE="${SEED_CANCEL_RATE:-0.12}"
EXTRA_FLAGS=()

# ─── Parse override flags ────────────────────────────────────────────────────
# Any flags passed on the command line are forwarded directly to bulk_seed.
# Recognised convenience overrides:
#   --rows N        override SEED_ROWS
#   --stores S      override SEED_STORES
#   --workers N     override SEED_WORKERS
#   --dsn DSN       override SEED_DSN
#   --truncate      passed through as-is
#   everything else forwarded verbatim
i=1
while [[ $i -le $# ]]; do
  arg="${!i}"
  case "$arg" in
    --rows)    i=$((i+1)); ROWS="${!i}" ;;
    --stores)  i=$((i+1)); STORES="${!i}" ;;
    --workers) i=$((i+1)); WORKERS="${!i}" ;;
    --dsn)     i=$((i+1)); DSN="${!i}" ;;
    *)         EXTRA_FLAGS+=("$arg") ;;
  esac
  i=$((i+1))
done

# ─── Build if binary is stale ────────────────────────────────────────────────
if [[ ! -f bulk_seed ]] || [[ bulk_seed.go -nt bulk_seed ]]; then
  echo "▶ Building bulk_seed..."
  go build -o bulk_seed .
  echo "▶ Build complete."
fi

# ─── Run ─────────────────────────────────────────────────────────────────────
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Axyra bulk seed"
echo "  DSN     : $DSN"
echo "  Rows    : $(printf "%'d" "$ROWS")"
echo "  Stores  : $STORES"
echo "  Workers : $WORKERS"
echo "  Batch   : $BATCH"
echo "  Cancel% : $(echo "$CANCEL_RATE * 100" | bc | cut -d. -f1)%"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

./bulk_seed \
  -dsn     "$DSN" \
  -rows    "$ROWS" \
  -stores  "$STORES" \
  -workers "$WORKERS" \
  -batch   "$BATCH" \
  -cancel  "$CANCEL_RATE" \
  "${EXTRA_FLAGS[@]+"${EXTRA_FLAGS[@]}"}"
