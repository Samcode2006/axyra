# Live comparison in the React console

Open **Live Comparison** in the sidebar (`/comparison`). Your existing console
session authenticates every request; no pasted token, DSN or tenant ID is needed.
The tenant dropdown lists only accessible tenants and indicates configured sources.

**Compare once** reads real source PostgreSQL and Axyra results. **Watch live**
repeats after each completed request plus 1.5 seconds. All time compares against
precomputed product aggregates. Date range compares captured order state by
original creation date, using inclusive UTC endpoints. Stop before changing scope.
Source results are rechecked after the analytics read to flag concurrent changes.

**Run full simulation** requires checking the explicit test-write checkbox.
It checks publication/replication readiness, samples the baseline, creates orders
for 1,000 and 500 on product A and 2,000 on product B, changes the second order
to 750, then cancels it. Each stage waits up to 90 seconds for two stable matches
and verifies the expected fixture amounts/counts. Final fixture revenue is
A = 1,000, B = 2,000. An active slot is a readiness hint, not proof of correct
agent mapping; bad mappings cause a visible timeout.

The run uses unique `cmp_...` IDs and a fixed one-second creation window, so
historical store data is excluded. Concurrent orders created in that second are
included in both queries. Three fixtures remain afterward, even if stopped or
failed, and affect normal analytics. No existing orders are updated or deleted.
Use a test store. Each start creates a new run; there is no automatic cleanup.
Stopping aborts browser requests but cannot undo a source commit already completed.

The page shows source/API query durations, ranked rows, mismatches, progress and
an exportable JSON report. It compares only top-100 product IDs, ranking,
non-cancelled counts and revenue, not every metric or every source row. Use a
single-currency store. Simulation verifies the dated path; use all-time comparison
separately to inspect precomputed totals. Timings are observations, not a controlled
performance benchmark or precise replication lag. Two stable samples cannot prove
there were no intervening source writes.

## Deployment

Rebuild the `web` and `control-plane` services to use this page. The reference
Compose configuration supplies `AXYRA_COMPARISON_REFERENCE_DSN`, expanding
`{index}` for seeded `tenant_001` through `TENANT_COUNT` to `reference-db-1`, etc.
The Compose generator preserves this setup. Credentials stay on the server.

For custom or managed tenants, set `AXYRA_COMPARISON_SOURCES` to a JSON object
mapping **canonical tenant IDs** to source PostgreSQL DSNs, then recreate the
control plane. An explicit map replaces reference defaults. Example `.env` entry:

```dotenv
AXYRA_COMPARISON_SOURCES='{"tenant_example":"postgres://user:password@source-host:5432/reference_db?sslmode=require"}'
```

Source databases must use the reference `public.orders` schema. Simulations also
require insert/update permission, `axyra_pub` publication membership and an active
logical replication slot. Read-only comparisons require only read access. There
is no browser endpoint accepting arbitrary SQL or database URLs. Every request
checks the signed-in user's organization access to the selected tenant; fixture
writes additionally require a tenant-bound, signed, expiring run ticket.

API routes (console authentication required):

- `GET /v1/comparison/sources`: available tenants and configuration status.
- `GET /v1/comparison/snapshot?tenant_id=...&from=...&to=...`: paired results;
  omit both timestamps for all time.
- `POST /v1/comparison/simulation`: `{ "tenant_id": "...", "step": 0 }`
  starts a run; steps 1–5 also supply the returned `ticket`.

An HTTP 500 on login is a separate control-plane/database problem. The comparison
page requires a working console login and does not bypass authentication.
