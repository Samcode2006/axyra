# Live source SQL vs Axyra comparison

Runs real SQL against the reference PostgreSQL `public.orders` table and reads
the public control-plane product endpoint using a console session JWT. No direct
analytics database access or service token is needed. Read-only by default.

From `scripts/seed` in PowerShell:

```powershell
$env:SOURCE_DATABASE_URL = 'postgres://postgres:postgres@localhost:5433/reference_db'
$env:AXYRA_CONSOLE_TOKEN = '<console session JWT>'
go run ./cmd/compare -store store_001 -tenant <canonical-tenant-id>
```

To compare both precomputed all-time totals and the captured-order date query:

```powershell
go run ./cmd/compare -store store_001 -tenant <canonical-tenant-id> -from 2026-10-01T00:00:00Z -to 2026-10-31T23:59:59Z
```

The program compares the top 100 product IDs, their ranking, non-cancelled order
counts and revenue. Currency must be consistent within the store. Revenue uses
exact decimal comparisons. Date ranges use original order creation timestamps
and inclusive endpoints. Product metadata and schema-v2 metrics are outside this
comparison. This is not a full database reconciliation: differences outside the
top 100, including missing cancelled-only orders, can remain undetected.

## Full source-to-dashboard-API simulation

Register a **dedicated empty test store** and its canonical Axyra tenant, configure
the agent mapping and start replication first. Use the matching database DSN.
The command refuses to seed a nonempty store and requires the orders publication
and an active logical replication slot. Keep other writers stopped for this test.

```powershell
go run ./cmd/compare -simulate -store comparison_store -tenant <canonical-test-tenant-id> -timeout 120s -report comparison.json
```

Each stage waits for two stable matching samples in **both** API modes:

| Stage | Expected A revenue / orders | Expected B revenue / orders |
|---|---|---|
| Empty baseline | absent | absent |
| Create O1 (A, 1000) | 1000 / 1 | absent |
| Create O2 (A, 500) | 1500 / 2 | absent |
| Create O3 (B, 2000) | 1500 / 2 | 2000 / 1 |
| Update O2 to 750 | 1750 / 2 | 2000 / 1 |
| Cancel O2 | 1000 / 1 | 2000 / 1 |

Fixture IDs share a printed unique prefix. The three source rows are retained,
including after a failure, for dashboard inspection. No truncation, deletion,
automatic backfill or rebuild is performed. Use a fresh empty store for a new
simulation. This exercises source writes, WAL capture, ingestion, worker updates
and authenticated analytics reads; it does not automate the browser, duplicate
delivery, or crash recovery.

`comparison.json` contains expected/source and actual/API rows, pass status,
attempts, elapsed time and final SQL/HTTP request durations per stage and mode.
The process exits nonzero on mismatch timeout, authorization or query failure.
HTTP errors fail immediately. Default timeout is 90s per mode, polling every 1s.
`-api` selects a different control-plane URL; `-report` selects the report path.

Source results are checked before and after each API request, then required to
match twice. Stop concurrent writes for a reliable check; sampling cannot prove
that no intervening changes occurred. Timing includes different work: SQL time
is the source query, HTTP time includes authentication and network overhead, and
elapsed time includes polling. These are observations, not a controlled speed
benchmark or a precise replication-lag measurement. Missing historical capture
requires investigation/backfill, not a longer timeout.

Run checker unit tests with `go test ./cmd/compare`.
