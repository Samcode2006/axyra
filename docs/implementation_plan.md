# Axyra — Updated Implementation Plan

**Updated:** 31 October 2026  
**Status:** Hackathon implementation plan  
**Core stack:** Go, PostgreSQL, Redis, React, FastAPI  
**Deployment:** Docker Compose

---

## 1. Product direction

**Axyra** is a platform-agnostic multi-tenant management and analytics infrastructure platform.

**Positioning:** The infrastructure layer that makes multi-tenant applications easier to manage, monitor and scale — with **zero application-code instrumentation required**.

The platform provides tenant and application management, database-level change capture via the **Axyra Agent**, asynchronous aggregation, tenant-isolated analytics APIs, and operational monitoring through one reusable web console.

Instead of asking customers to call an event API from their application code, Axyra ships a lightweight Go agent binary that customers run inside their own infrastructure, alongside their database. The agent reads the PostgreSQL Write-Ahead Log (WAL) using logical replication, captures every committed row change, and streams structured events to the Axyra data plane. No application code changes are needed.

The reference e-commerce application demonstrates this integration. It is not a hardwired dependency of the platform.

## 2. Updated frontend decision

There will be **exactly two frontend applications**:

1. **`reference-client/`** — the demo/reference product frontend, built with React.
2. **`web/`** — the single Axyra platform web application, also built with React.

There will be no separate frontend applications for the platform console, monitoring, analytics, or configuration. Those are pages/sections inside the one `web/` React application.

The reference client's backend remains FastAPI. The Axyra backend services remain Go.

### `web/` React application pages

| Page / section | Purpose |
|---|---|
| Overview | High-level application and tenant status, key usage and health indicators |
| Tenant Management | List/register tenants, map external tenant IDs, configure tenant status and quotas |
| Applications & Integrations | Register connected applications, view integration details and manage credentials |
| Analytics | Summary metrics, trends, Top-N and category/status breakdowns |
| Monitoring | Ingestion throughput, processing lag, query latency, worker health and failed events |
| Metric Configuration | Configure supported metric definitions without arbitrary SQL |
| Access & Policies | Manage tenant-scoped policies, limits and integration access |
| Platform Operations (operator-only) | Platform-wide organizations, applications and service health |

The navigation and pages can be role-aware. A customer administrator sees only the organizations/applications/tenants they are authorized to manage. Axyra platform operators may have a separate operator-only view. Do not expose platform-wide operations to client administrators.

### Reference client

The reference client is a small e-commerce application used to demonstrate integration:
- React frontend for creating/viewing demo orders and opening the Axyra experience.
- FastAPI backend for business logic and its own source PostgreSQL database.
- **The Axyra Agent** runs alongside the reference PostgreSQL database, reads the WAL, and streams change events to Axyra automatically — no code change to FastAPI is required.
- The reference client does not host Axyra's tenant-management, analytics or monitoring pages.

## 3. Problem and requirements

The original PRD describes an aggregation layer that keeps dashboard queries fast and fresh without putting analytics load on the primary application database.

### Mandatory requirements

- **FR-1:** Counts and totals for a selected time range.
- **FR-2:** Trends over time, at hourly or daily granularity.
- **FR-3:** Top-N list.
- **FR-4:** Category/status breakdown.
- **FR-5:** Every analytics query is tenant-scoped.
- **FR-6:** New data appears, including updates such as placed → cancelled.
- **FR-7:** Simple REST API and clear README.

### Bonus requirements

- **B-1:** Show data freshness.
- **B-2:** Rebuild aggregates from the raw event history.
- **B-3:** Limit simultaneous heavy queries per tenant.
- **B-4:** Add supported metrics through configuration rather than code.
- **B-5:** Monitoring page for query time, data lag and related operational signals.

### PRD performance targets

| Measure | Good | Great |
|---|---:|---:|
| Analytics query latency | <1 second | <200 ms |
| Data freshness | <5 minutes | <30 seconds |
| Primary DB slowdown | <25% | <10% |
| Aggregate correctness | Most cases | All tested cases |
| Query slowdown from 1M to 10M records | <5x | <2x |

Test datasets:
- Small: 1M records / 10 tenants.
- Main: 10M records / 100 tenants, with uneven tenant sizes.

These are targets, not claims. Report measured benchmark results.

## 4. Technology decisions

| Layer | Technology | Responsibility |
|---|---|---|
| Reference frontend | React | Demo e-commerce UI |
| Reference backend | FastAPI | Business logic only — no event publishing code needed |
| Reference source DB | PostgreSQL | Client-owned business data; WAL logical replication enabled |
| **Axyra Agent** | **Go binary** | **Runs on-prem/in customer infra; reads PostgreSQL WAL via logical replication; streams events to Axyra data plane** |
| Axyra control plane | Go | Organizations, applications, tenants, credentials, agent registration, policies and configuration |
| Axyra data plane | Go | Event ingestion from agents, workers and analytics query APIs |
| Axyra web application | React | One application for management, analytics and monitoring pages |
| Platform metadata DB | PostgreSQL | Control-plane state |
| Analytics DB | PostgreSQL | Raw events, processing state and precomputed aggregates |
| Queue / coordination | Redis | Buffering, worker coordination and retry support |
| Local deployment | Docker Compose | Reproducible local environment |

Use separate logical databases or schemas for platform metadata and analytics data. Keep the client's source database separate from both.

**Deliberate scope choices:** PostgreSQL instead of ClickHouse; Redis instead of Kafka; WAL-based CDC instead of application-code instrumentation; one React platform web app instead of multiple frontend apps; custom monitoring UI rather than requiring Grafana/Prometheus. Keep the system small enough to finish and benchmark.

## 5. High-level architecture

```text
CUSTOMER / REFERENCE CLIENT ENVIRONMENT  (on-prem or customer cloud)
┌──────────────────────────────────────────────────────────────────┐
│  React reference frontend                                        │
│           │                                                      │
│  FastAPI business backend  ──────────►  Customer PostgreSQL      │
│                                               │                  │
│                                       [Axyra Agent]              │
│                                    reads WAL via logical         │
│                                    replication (no app changes)  │
└───────────────────────────────────────────────┼──────────────────┘
                                                │  secure TLS stream
                                                │  (structured change events)
                                                ▼
AXYRA PLATFORM
┌──────────────────────────────────────────────────────────────────┐
│ Go Control Plane                                                  │
│ Organizations • Applications • Agent registration                │
│ Tenant registry/mappings • API keys • Policies • Metric config   │
└──────────────────────────────────┬───────────────────────────────┘
                                   │ trusted tenant scope
┌──────────────────────────────────▼───────────────────────────────┐
│ Go Analytics Data Plane                                           │
│ Agent ingestion endpoint → Redis → Go aggregation workers        │
│                                    ↓                             │
│                             PostgreSQL analytics store            │
│                                    ↑                             │
│                        Tenant-scoped Query API                    │
└──────────────────────────────────┬───────────────────────────────┘
                                   │ REST APIs
┌──────────────────────────────────▼───────────────────────────────┐
│ One React application: /web                                       │
│ Overview • Tenants • Apps • Analytics • Monitoring               │
│ Metric configuration • Access policies • Operator pages          │
└──────────────────────────────────────────────────────────────────┘
```

**Key principle:** The Axyra Agent runs entirely inside the customer's environment. Only processed, structured events leave the customer's network over a secure TLS connection — raw database contents are never exposed to Axyra directly. The React platform web app talks to Axyra APIs only; it must not connect directly to any PostgreSQL database.

## 6. Repository structure

```text
/
├── agent/                          # Go: Axyra Agent binary (ships to customers)
│   ├── cmd/agent/                  # Entry point and CLI flags
│   ├── internal/
│   │   ├── replication/            # PostgreSQL WAL / logical replication reader
│   │   ├── mapper/                 # Row-change → Axyra event transformer
│   │   ├── sender/                 # TLS event stream to Axyra data plane
│   │   └── config/                 # Table watch-list, column mappings, tenant-id field
│   └── Dockerfile
│
├── services/
│   ├── control-plane/              # Go: orgs, apps, agent registration, tenants, keys, policies
│   └── analytics/                  # Go: agent ingestion endpoint, workers, query APIs
│
├── reference-client/
│   ├── frontend/                   # React reference e-commerce frontend
│   └── backend/                    # FastAPI business backend (no Axyra SDK needed)
│
├── web/                            # The ONLY Axyra platform frontend
│   ├── src/
│   │   ├── app/                    # Router, app shell and route definitions
│   │   ├── components/             # Shared UI components
│   │   ├── features/
│   │   │   ├── overview/
│   │   │   ├── tenants/
│   │   │   ├── applications/
│   │   │   ├── analytics/
│   │   │   ├── monitoring/
│   │   │   ├── metric-config/
│   │   │   ├── access-policies/
│   │   │   └── platform-operations/
│   │   ├── lib/                    # API client, auth and shared utilities
│   │   └── types/
│   └── ...
│
├── deploy/
│   └── docker-compose.yml          # Includes agent as a sidecar next to reference PostgreSQL
├── migrations/
│   ├── metadata/
│   └── analytics/
├── configs/
│   ├── metrics/
│   └── agent-example.yaml          # Example agent config: tables, tenant-id column, Axyra endpoint
├── scripts/
│   ├── seed/
│   ├── load-test/
│   └── benchmark/
└── docs/
    ├── architecture.md
    ├── api.md
    ├── agent-setup.md              # How to deploy and configure the agent
    ├── event-schema.md
    └── implementation-plan.md
```

There are only two frontend projects: `reference-client/frontend` and `web`. The `web` feature folders are modules within one React app, not independently deployed applications.

## 7. Service responsibilities

### 7.1 Axyra Agent (Go binary — runs in customer infra)

The agent is the **only component that touches the customer's database**. It:
- Connects to the customer's PostgreSQL using the logical replication protocol.
- Consumes the WAL stream for configured tables (e.g. `orders`, `products`).
- Translates row-level INSERT / UPDATE / DELETE changes into structured Axyra events using a declarative column mapping in `configs/agent-example.yaml`.
- Extracts the external tenant ID from a configured column (e.g. `store_id`).
- Authenticates to the Axyra data plane using a pre-registered agent credential.
- Streams events over TLS to the Axyra ingestion endpoint.
- Tracks replication slot position so events survive agent restarts without gaps or duplicates.
- Exposes a local health/status endpoint for monitoring.

The agent does **not** require any changes to customer application code.

### 7.2 Go control plane

Owns platform configuration and identity:
- Organization/customer registry.
- Connected application and agent registration.
- Canonical tenant registry.
- Mapping from a customer's external tenant ID column to Axyra's internal tenant ID.
- Agent credential (token) issuance, rotation, revocation and validation.
- Access policies, quotas and tenant configuration.
- Supported metric definitions (config-driven).
- Audit records for administrative changes.

### 7.3 Go analytics data plane

Owns event processing and analytics:
- Agent ingestion endpoint: receives event streams from agents, validates structure.
- Agent authentication and trusted tenant resolution using the registered mapping.
- Event deduplication and queueing into Redis.
- Worker processing and retry/dead-letter handling.
- Raw event ledger and processing state.
- Precomputed aggregate maintenance.
- Tenant-scoped analytics query endpoints.
- Freshness and processing status.

### 7.4 React Axyra web app

Owns the platform's human-facing management and observability experience:
- Overview and tenant management.
- Application/agent integration management.
- Analytics dashboards.
- Monitoring and failed-event inspection.
- Metric configuration and access-policy pages.
- Operator-only platform operations.

Use a shared API client, typed request/response models, route guards and reusable components. Keep page modules in one build/deployment.

### 7.5 Reference client

Owns only its own demo business experience:
- Create and view demo orders via FastAPI and its own PostgreSQL database.
- **No event publishing code** — the Axyra Agent handles that automatically.
- Provide a clear link or navigation entry to the Axyra console.

## 8. Tenant identity and security model

Do not trust a tenant ID value from the customer's application code or event body as proof of identity. The agent resolves tenant identity from the database row itself, using a pre-configured column mapping.

**Agent-based event flow:**

1. The Axyra Agent reads a committed row change from the customer's PostgreSQL WAL.
2. The agent extracts the external tenant ID from a pre-configured column (e.g. `store_id` in the `orders` table).
3. The agent authenticates to Axyra's data plane using its registered agent token.
4. Axyra resolves the external tenant ID to a canonical internal tenant ID using the registered mapping.
5. Axyra attaches the canonical tenant scope to the event processing context.
6. Workers, aggregate updates, query handlers and cache operations enforce that scope.

Keep these identities distinct:
- **Axyra organization:** the customer account using Axyra.
- **Connected application / agent:** the registered agent instance tied to a customer database.
- **External tenant ID:** the tenant identifier stored as a column value in the customer's database rows.
- **Canonical Axyra tenant ID:** Axyra's internal tenant scope, resolved after agent authentication.

The agent token is a long-lived server-side secret stored in the customer's environment only (e.g. as a Docker environment variable or secret). It is never placed in browser code.

Every analytics query must be scoped by the authenticated principal's authorized tenant context. Never allow a browser user to supply an arbitrary internal tenant ID.

### Admin access from the reference client

For the demo, the reference client's admin can open the Axyra web app through a secure integration flow:
- Admin signs in to the reference client.
- The reference backend validates the session and authorization.
- The backend issues a short-lived signed token scoped to the authorized organization.
- Axyra validates issuer, audience, signature, expiry and authorized organization scope.
- The React app calls Axyra APIs using the resulting short-lived session/token.

For the hackathon, if full SSO would delay the core aggregation demo, use a separate development login for the Axyra console and document the intended token-exchange integration. Do not weaken tenant authorization to make the demo work.

## 9. Event contract

The Axyra Agent generates events from WAL row changes and sends them to the data plane. The event structure is the same regardless of source — the agent assembles it from the column mapping config.

Example event (generated by the agent from an INSERT on the `orders` table):

```json
{
  "event_id": "<deterministic uuid derived from replication LSN + table + pk>",
  "agent_id": "agent_ref_demo",
  "external_tenant_id": "store_001",
  "event_type": "order.created",
  "schema_version": 1,
  "occurred_at": "2026-10-31T10:00:00Z",
  "lsn": "0/1A2B3C4",
  "data": {
    "order_id": "order_1001",
    "amount": 1299.00,
    "currency": "INR",
    "status": "placed",
    "product_id": "product_42",
    "product_name": "Demo product",
    "category": "Accessories"
  }
}
```

**Key fields:**
- `event_id`: derived from the WAL LSN + table + primary key, ensuring natural idempotency across agent restarts.
- `agent_id`: the registered agent instance; Axyra resolves the trusted organization/application scope from this.
- `external_tenant_id`: extracted from the configured tenant-ID column in the source row.
- `lsn`: the PostgreSQL Log Sequence Number; used to track replication position and rebuild from history.

### Agent config (example `configs/agent-example.yaml`)

```yaml
axyra_endpoint: https://axyra.example.com
agent_token: "${AXYRA_AGENT_TOKEN}"
postgres_dsn: "${POSTGRES_DSN}"
replication_slot: axyra_slot

tables:
  - name: orders
    tenant_id_column: store_id
    event_type_column: status          # INSERT → order.created; UPDATE → order.updated
    columns: [order_id, amount, currency, status, product_id, product_name, category]
```

### Event semantics

- WAL `INSERT` on `orders`: mapped to `order.created` — add the order's contribution to relevant aggregates.
- WAL `UPDATE` on `orders`: mapped to `order.updated` — apply a correct delta or recompute the affected contribution.
- WAL `UPDATE` with `status = cancelled`: mapped to `order.cancelled` — reverse the active order contribution and update status/category counts.
- Duplicate `event_id` (same LSN replayed): do not apply the event twice — idempotency is guaranteed by the LSN-derived ID.
- Invalid or unsupported event: reject it clearly or retain it in a visible failed-event/dead-letter workflow.

Persist enough order state or prior contribution information to reverse earlier aggregate effects accurately. Handle retries and out-of-order events deliberately.

## 10. API outline

Example endpoints (final paths and response schemas should be frozen in the API contract before parallel implementation).

### Control plane

```text
POST   /v1/organizations
POST   /v1/applications
GET    /v1/applications
POST   /v1/agents                        # Register a new agent instance
GET    /v1/agents
POST   /v1/agents/{agent_id}/tokens      # Issue / rotate agent token
DELETE /v1/agents/{agent_id}/tokens/{token_id}
POST   /v1/tenants
GET    /v1/tenants
POST   /v1/agents/{agent_id}/tenant-mappings   # Map external tenant ID column → internal tenant
GET    /v1/metric-definitions
PUT    /v1/metric-definitions/{metric_id}
```

### Analytics data plane

```text
# Agent-facing (mTLS / agent token auth)
POST /v1/agent/events                   # Agent streams batched WAL events to Axyra
GET  /v1/agent/ack                      # Agent confirms last processed LSN

# Dashboard-facing (user session auth)
GET  /v1/analytics/orders/summary?from=...&to=...
GET  /v1/analytics/orders/trend?interval=day&from=...&to=...
GET  /v1/analytics/products/top?limit=10&from=...&to=...
GET  /v1/analytics/orders/breakdown?group_by=status&from=...&to=...
GET  /v1/analytics/status               # Freshness, lag, last processed LSN
```

The query API derives tenant scope from the authenticated user's session context; it does not accept an untrusted tenant ID as an authorization mechanism. The agent endpoint uses a separate agent token; the two auth paths are distinct.

## 11. Analytics data design

Start with a small, explicit schema:
- `raw_events`: immutable event envelope, resolved tenant, event ID, event type, occurred/received timestamps and payload.
- `processed_events`: deduplication and processing status, error/retry metadata.
- `order_state`: current canonical order state or enough prior contribution data to apply corrections.
- `order_summary_hourly` / `order_summary_daily`: precomputed count and revenue aggregates.
- `order_status_aggregates`: counts grouped by status and time grain.
- `product_aggregates`: product/category totals used by Top-N queries.
- `processing_watermarks`: last successfully processed timestamp/offset per relevant scope.

Index for tenant-scoped access, especially combinations such as `(tenant_id, created_at)` and tenant/product/time dimensions. Select indexes based on query plans and measured workloads.

Do not run every dashboard query against the raw event ledger. The query API should primarily read precomputed aggregates.

## 12. Implementation phases

### Phase 0 — Contract and runnable skeleton

- Agree on agent event envelope, tenant identity model, column mapping config format and API contracts.
- Create monorepo structure including `agent/` directory and the two frontend projects.
- Enable PostgreSQL logical replication on the reference DB (`wal_level = logical`).
- Add Docker Compose (including the agent as a sidecar service), environment examples, migrations and health endpoints.
- Seed one Axyra organization, one registered agent and two mapped tenants.
- Confirm all services — including the agent — start from a clean checkout.

### Phase 1 — Two-hour end-to-end vertical slice

**Goal:** create an order in the reference client, have the Axyra Agent capture it automatically from the WAL, process it into aggregates, query the result and prove tenant isolation.

| Time | Developer A — Go Lead (all Go services + agent) | Developer B — Everything else |
|---|---|---|
| 0–15 min | Agree event schema, column-mapping config format and repo skeleton together | ← same |
| 15–75 min | Go control-plane skeleton; metadata schema; agent registration + token issuance; external-to-internal tenant mapping. **Then:** Go agent binary MVP: connect to PostgreSQL WAL slot, read INSERT on `orders`, emit event over TLS to analytics ingestion endpoint; Redis queue; worker; idempotent `order.created`; summary query | Scaffold `web/` Vite app + routing shell; FastAPI reference backend + Docker Compose wiring + health-checks; enable `wal_level=logical` on reference Postgres |
| 75–100 min | Connect agent stream → ingestion endpoint → Redis → worker → PostgreSQL analytics; expose `GET /v1/analytics/orders/summary` and `last_processed_at` | Wire React reference client to FastAPI (create-order form); verify all services including agent start cleanly |
| 100–120 min | Integration fixes; replication slot restart-safety test; tenant isolation smoke-test | Display summary response in `web/` Overview page; rehearse full demo flow |

**Checkpoint demo:**
1. Start everything with Docker Compose (including the Axyra Agent sidecar).
2. Show two tenants registered in Axyra.
3. Create an order for tenant A via the reference React app — **no API call from FastAPI to Axyra**.
4. Show the agent automatically captured the WAL event and Axyra processed it.
5. Query tenant A's count/revenue and freshness (LSN watermark).
6. Attempt to access tenant A's data as tenant B and show that it is not exposed.

At this checkpoint, a polished dashboard, all four analytics query types, rebuild, full SSO and large-scale benchmarks are not expected.

### Phase 2 — Complete control plane + agent management

- Organization/application/tenant lifecycle.
- Agent registration, token issuance, rotation and revocation.
- External tenant column mappings per agent.
- Policies and quotas.
- Metric configuration for supported metrics (config-driven, not SQL).
- Admin APIs and audit records.
- React `web` pages for overview, tenants, agent integrations and access/configuration.

### Phase 3 — Reliable agent ingestion and processing

- Strict event schema/version validation on the ingestion endpoint.
- Agent token authentication and trusted tenant resolution.
- Replication slot position tracking and agent restart safety (no gaps, no duplicates).
- LSN-derived idempotency and deduplication.
- Redis retries, failure visibility and dead-letter handling.
- Worker restart safety.
- Update/cancel semantics and out-of-order event handling.
- Raw event ledger storing LSN, WAL table, change type and payload.

### Phase 4 — Complete PRD analytics

- FR-1 summary counts/totals.
- FR-2 hourly/daily trends.
- FR-3 Top-N.
- FR-4 category/status breakdown.
- FR-5 enforce tenant scope on every query.
- FR-6 reflect new, updated and cancelled records.
- FR-7 REST API and README.

Build the React Analytics pages against these APIs. Keep date/timezone boundaries consistent across all endpoints.

### Phase 5 — Bonus features and monitoring

- B-1 freshness and processing lag.
- B-2 safe rebuild/reconciliation from raw events.
- B-3 per-tenant heavy-query concurrency limits.
- B-4 config-driven supported metric definitions (never arbitrary SQL execution).
- B-5 React Monitoring page: ingestion rate, queue depth/lag, worker health, failed events, query latency and throughput.

Implement custom instrumentation in Go. Persist operational measurements initially in PostgreSQL and expose them through APIs to the React Monitoring page. Avoid unbounded high-cardinality time-series labels; tenant-level drill-down can be queried from stored operational records.

### Phase 6 — Correctness, security and resilience

Test:
- Two tenants with overlapping external IDs across different applications.
- Missing, invalid, rotated and revoked API keys.
- Attempts to spoof tenant identity.
- Duplicate, delayed, retried and out-of-order events.
- Order placed → updated → cancelled transitions.
- Worker restart during processing.
- Redis outage/recovery and queue backlog.
- Rebuild result compared with incrementally maintained aggregates.
- Cache isolation and invalidation, if caching is enabled.
- Every analytics endpoint under cross-tenant access attempts.

### Phase 7 — Benchmark and tune

Run:
- 1M records / 10 tenants.
- 10M records / 100 tenants with skewed tenant sizes.

Measure:
- p50, p95 and p99 analytics query latency.
- Ingestion throughput and processing lag.
- End-to-end freshness.
- Primary source DB query latency with and without event publishing.
- Correctness against direct source-data calculations.
- Query latency growth from 1M to 10M records.

Also measure the overhead of the Axyra Agent on the customer's PostgreSQL (replication slot lag, WAL read latency). This directly maps to the PRD target of < 25% primary DB slowdown.

Tune indexes, aggregation grain, worker batch size, worker concurrency, agent batch flush interval and caching based on measurements. Do not report unmeasured targets as achieved.

### Phase 8 — Final demo and documentation

The final demo should show:
1. Register an organization, application and tenants in the Axyra console.
2. Download and configure the Axyra Agent with a token and column mapping config.
3. Start everything with Docker Compose — agent connects to reference PostgreSQL automatically.
4. Create an order in the reference React app (no Axyra code in FastAPI).
5. Show the agent captured the WAL event and Axyra processed it — view updated summary and trend metrics.
6. Cancel/update an order; show corrected aggregates.
7. Show Top-N and category/status breakdown.
8. Demonstrate tenant isolation.
9. Show freshness (LSN watermark), failed events and monitoring.
10. Show benchmark results at 1M and 10M records, including agent overhead on source DB.

README should include prerequisites, WAL configuration (`wal_level = logical`), environment variables, Compose startup/reset, migrations/seeding, agent config format, API examples, event schema, tenant security model, test commands, load generation, benchmark methodology and troubleshooting.

## 13. Two-developer ownership

> **Principle:** Developer A (Go Lead) owns **all Go code** and the complex cross-cutting platform concerns. Developer B owns everything else — React web app, FastAPI reference client, Docker Compose, and documentation.

### Developer A — Go Lead (all Go + complex platform work)

**Go services (exclusive owner)**
- **Axyra Agent** (`agent/`): PostgreSQL WAL reader, logical replication slot management, column→event mapper, TLS event sender, restart-safe LSN tracking, local health endpoint.
- Go control-plane service (`services/control-plane/`) and metadata migrations.
- Organization/application/agent registration and external-to-internal tenant column mappings.
- Agent token issuance, rotation, revocation and validation.
- Policies, quotas and metric configuration endpoints.
- Audit records for administrative changes.
- Go analytics data plane (`services/analytics/`) and analytics migrations.
- Agent ingestion endpoint, event validation, agent auth and trusted tenant resolution.
- Redis queue, worker lifecycle, deduplication and retry/dead-letter handling.
- Idempotent event processing and update/cancel correctness.
- Precomputed aggregate tables (hourly/daily roll-ups).
- All PRD analytics query endpoints (FR-1 through FR-7).
- Rebuild/reconciliation (B-2), per-tenant query concurrency limits (B-3).
- Benchmark scripts and performance tuning (`scripts/benchmark/`, `scripts/load-test/`).
- Agent overhead measurement on source PostgreSQL.

**Cross-cutting platform responsibilities**
- Define and freeze the API contract (`docs/api.md`, `docs/event-schema.md`) before parallel coding.
- Design the tenant-scope security model and review all authentication paths.
- Provide typed response schemas for the React API client (`web/src/types/`).
- Review all pull requests that touch auth, tenant isolation or the event pipeline.
- Config-driven metric definitions in `configs/metrics/` (B-4).

**`web/` React pages owned by Developer A**
- Monitoring page (B-5): ingestion rate, queue depth, worker health, failed events, query latency.
- Metric Configuration page.
- Platform Operations (operator-only) page.

### Developer B — Everything else

**`web/` React platform app**
- Scaffold and maintain the `web/` Vite + React project (routing shell, design system, shared components).
- Overview page: high-level tenant status, usage indicators.
- Tenant Management page: list/register tenants, quotas.
- Applications & Integrations page: credentials display.
- Analytics pages: summary, trend charts, Top-N, breakdown, data-freshness indicator (B-1).
- Access & Policies page.
- Shared `web/src/lib/api.ts` API client and route guards.
- UI unit tests and accessibility audit.

**Reference client**
- FastAPI reference backend (`reference-client/backend/`): Dockerfile, DB migrations, order APIs.
- **No Axyra event-publishing code** — the agent handles that; enable `wal_level = logical` in the reference PostgreSQL Dockerfile.
- OpenAPI spec / Swagger UI for the reference backend.
- React reference frontend (`reference-client/frontend/`): create/view demo orders.
- Seed demo data for the reference DB to generate realistic WAL traffic (`scripts/seed/`).
- End-to-end test harness: create order → agent picks up WAL → verify aggregate updated.

**Infrastructure and DevOps**
- Docker Compose (`deploy/docker-compose.yaml`): networking, volumes, health-checks, env examples — includes `axyra-agent` as a sidecar service next to reference PostgreSQL.
- CI pipeline: lint (Go + TypeScript), test, and build steps.
- Deployment scripts and release tagging (`v1.0.0`).
- `docs/agent-setup.md`: how to configure and run the agent (WAL setup, config file, token).
- Troubleshooting and README sections for Compose startup, reset, migrations and WAL slot management.

### Shared ownership
- Event/API contract agreed before any parallel coding begins.
- One shared design system and routing structure in `web/` (Developer B leads, Developer A contributes types).
- End-to-end integration and tenant-isolation tests.
- Docker Compose demo rehearsal.
- Project README (`docs/` and root `README.md`).

Do not create separate React apps for individual pages. Developers can work in separate feature folders/branches, but all Axyra platform pages ship from the single `web/` application.

## 14. Scope guardrails

### Must deliver
- Working Axyra Agent that reads PostgreSQL WAL and streams events — no application code changes.
- Trusted tenant mapping and isolation (resolved from agent token + column config, not app code).
- Secure agent-to-platform event ingestion over TLS.
- Idempotent asynchronous processing (LSN-derived event IDs).
- PostgreSQL precomputed aggregates.
- All FR-1 through FR-7.
- Correct update/cancel behavior captured from WAL UPDATE events.
- Docker Compose (including agent sidecar) and clear README with WAL setup instructions.
- 1M and 10M benchmark runs with honest results including agent overhead on source DB.

### Aim to deliver
- Freshness display.
- Rebuild/reconciliation.
- Per-tenant query concurrency limits.
- Config-driven supported metrics.
- Custom monitoring page.
- Useful tenant/application management pages.

### Defer unless core is complete
- Enterprise identity provider integration and complex RBAC.
- Automatic provisioning of customer databases/infrastructure.
- Kubernetes/cloud deployment.
- Multiple analytics database engines.
- Kafka.
- Arbitrary SQL metric definitions.
- Multiple language SDKs.
- ML predictions and anomaly detection.

## 15. Final architecture principle

Axyra's technical core is the **CDC agent + aggregation engine**: a lightweight Go agent that captures database changes at the WAL level (zero application instrumentation), reliable event streaming, correct precomputed aggregates and measurable tenant-isolated query performance. The Go control plane makes that engine reusable across any PostgreSQL-backed application. The single React `web/` app provides the platform management and analytics experience. The separate React reference client demonstrates how an existing product integrates with Axyra — without modifying a single line of its business logic.

**The agent is the differentiator.** It solves the database consistency problem (events are guaranteed to reflect committed state), eliminates integration friction (no SDK or API calls in customer code) and enables on-prem/private-cloud deployments where data never leaves the customer's network.

**Frontend count is fixed at two:** `reference-client/frontend` and `web`. All Axyra management, analytics, monitoring and configuration experiences are pages inside `web`.
