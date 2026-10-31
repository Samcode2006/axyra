# Axyra — Updated Implementation Plan

**Updated:** 31 October 2026  
**Status:** Hackathon implementation plan  
**Core stack:** Go, PostgreSQL, Redis, React, FastAPI  
**Deployment:** Docker Compose

---

## 1. Product direction

**Axyra** is a platform-agnostic multi-tenant management and analytics infrastructure platform.

**Positioning:** The infrastructure layer that makes multi-tenant applications easier to manage, monitor and scale.

The platform provides tenant and application management, secure event ingestion, asynchronous aggregation, tenant-isolated analytics APIs, and operational monitoring through one reusable web console.

The reference e-commerce application demonstrates integration with Axyra. It is not a hardwired dependency of the platform.

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
- FastAPI sends order events to Axyra using a server-side integration credential.
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
| Reference backend | FastAPI | Business logic and event publishing |
| Reference source DB | PostgreSQL | Client-owned business data |
| Axyra control plane | Go | Organizations, applications, tenants, credentials, policies and configuration |
| Axyra data plane | Go | Ingestion, workers and analytics query APIs |
| Axyra web application | React | One application for management, analytics and monitoring pages |
| Platform metadata DB | PostgreSQL | Control-plane state |
| Analytics DB | PostgreSQL | Raw events, processing state and precomputed aggregates |
| Queue / coordination | Redis | Buffering, worker coordination and retry support |
| Local deployment | Docker Compose | Reproducible local environment |

Use separate logical databases or schemas for platform metadata and analytics data. Keep the client's source database separate from both.

**Deliberate scope choices:** PostgreSQL instead of ClickHouse; Redis instead of adding Kafka; one React platform web app instead of multiple frontend apps; custom monitoring UI rather than requiring Grafana/Prometheus. Keep the system small enough to finish and benchmark.

## 5. High-level architecture

```text
REFERENCE CLIENT
┌──────────────────────────────┐
│ React reference frontend     │
└──────────────┬───────────────┘
               │
┌──────────────▼───────────────┐
│ FastAPI business backend     │
└──────────────┬───────────────┘
               │ business data
┌──────────────▼───────────────┐
│ Client-owned PostgreSQL      │
└──────────────────────────────┘
               │ authenticated event API
               ▼
AXYRA PLATFORM
┌──────────────────────────────────────────────────────────┐
│ Go Control Plane                                          │
│ Organizations • Applications • Tenant registry/mappings  │
│ API keys • Policies • Quotas • Metric configuration      │
└──────────────────────────────┬───────────────────────────┘
                               │ trusted tenant scope
┌──────────────────────────────▼───────────────────────────┐
│ Go Analytics Data Plane                                   │
│ Ingestion API → Redis → Go aggregation workers           │
│                         ↓                                │
│                  PostgreSQL analytics store               │
│                         ↑                                │
│                 Tenant-scoped Query API                   │
└──────────────────────────────┬───────────────────────────┘
                               │ REST APIs
┌──────────────────────────────▼───────────────────────────┐
│ One React application: /web                               │
│ Overview • Tenants • Apps • Analytics • Monitoring       │
│ Metric configuration • Access policies • Operator pages  │
└──────────────────────────────────────────────────────────┘
```

The React platform web app talks to Axyra APIs. It must not connect directly to either PostgreSQL database.

## 6. Repository structure

```text
/
├── services/
│   ├── control-plane/              # Go: orgs, apps, tenants, keys, policies
│   └── analytics/                  # Go: ingestion, workers, query APIs
│
├── reference-client/
│   ├── frontend/                   # React reference e-commerce frontend
│   └── backend/                    # FastAPI business backend
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
│   └── docker-compose.yml
├── migrations/
│   ├── metadata/
│   └── analytics/
├── configs/
│   └── metrics/
├── scripts/
│   ├── seed/
│   ├── load-test/
│   └── benchmark/
└── docs/
    ├── architecture.md
    ├── api.md
    ├── event-schema.md
    └── implementation-plan.md
```

There are only two frontend projects: `reference-client/frontend` and `web`. The `web` feature folders are modules within one React app, not independently deployed applications.

## 7. Service responsibilities

### 7.1 Go control plane

Owns platform configuration and identity:
- Organization/customer registry.
- Connected application registration.
- Canonical tenant registry.
- Mapping from an application's external tenant ID to Axyra's internal tenant ID.
- API key creation, rotation, revocation and validation.
- Access policies, quotas and tenant configuration.
- Supported metric definitions.
- Audit records for administrative changes.

### 7.2 Go analytics data plane

Owns event processing and analytics:
- Ingestion endpoint and payload validation.
- Authentication of the source application.
- Trusted tenant resolution using the registered mapping.
- Event deduplication and queueing.
- Worker processing and retry handling.
- Raw event ledger and processing state.
- Precomputed aggregate maintenance.
- Tenant-scoped analytics query endpoints.
- Freshness and processing status.

### 7.3 React Axyra web app

Owns the platform's human-facing management and observability experience:
- Overview and tenant management.
- Application/integration management.
- Analytics dashboards.
- Monitoring and failed-event inspection.
- Metric configuration and access-policy pages.
- Operator-only platform operations.

Use a shared API client, typed request/response models, route guards and reusable components. Keep page modules in one build/deployment.

### 7.4 Reference client

Owns only its own demo business experience:
- Create and view demo orders.
- Store business records in its own PostgreSQL database.
- Publish events to Axyra from FastAPI.
- Provide a clear link or navigation entry to the Axyra console.

## 8. Tenant identity and security model

Do not trust a tenant ID supplied in an event body as proof of identity.

Use this flow:

1. A reference-client backend sends an event with its application credential.
2. Axyra authenticates the application.
3. Axyra resolves the event's external tenant ID using the registered application-to-tenant mapping.
4. Axyra attaches the canonical internal tenant scope to the processing context.
5. Workers, aggregate updates, query handlers and cache operations enforce that scope.

Keep these identities distinct:
- **Axyra organization:** the customer account using Axyra.
- **Connected application:** a product/system integrated with Axyra.
- **External tenant ID:** the tenant identifier used inside that application.
- **Canonical Axyra tenant ID:** Axyra's internal tenant scope.

Every query must be scoped by the authenticated principal's authorized tenant context. Never allow a browser user to choose an arbitrary internal tenant ID and thereby gain access to it.

### Admin access from the reference client

For the demo, the reference client's admin can open the Axyra web app through a secure integration flow:
- Admin signs in to the reference client.
- The reference backend validates the session and authorization.
- The backend performs a secure token exchange or issues a short-lived signed token for the Axyra web app.
- Axyra validates issuer, audience, signature, expiry and authorized organization/application scope.
- The React app calls Axyra APIs using the resulting short-lived session/token.

Do not put long-lived application API keys or service credentials in browser code. Do not blindly trust role or tenant claims supplied by the browser.

For the hackathon, if full SSO would delay the core aggregation demo, use a separate development login for the Axyra console and document the intended token-exchange integration. Do not weaken tenant authorization to make the demo work.

## 9. Event contract

Example event:

```json
{
  "event_id": "uuid",
  "source_application_id": "app_demo",
  "external_tenant_id": "store_001",
  "event_type": "order.created",
  "schema_version": 1,
  "occurred_at": "2026-10-31T10:00:00Z",
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

The source backend sends the external tenant ID. Axyra resolves it to a trusted internal tenant ID after authenticating the application.

### Event semantics

- `order.created`: add the order's contribution to relevant aggregates.
- `order.updated`: apply a correct delta or recompute the affected contribution.
- `order.cancelled`: reverse the active order contribution and update status/category counts.
- Duplicate `event_id`: do not apply the event twice.
- Invalid or unsupported event: reject it clearly or retain it in a visible failed-event/dead-letter workflow.

Persist enough order state or prior contribution information to reverse earlier aggregate effects accurately. Handle retries and out-of-order events deliberately.

## 10. API outline

Example endpoints (final paths and response schemas should be frozen in the API contract before parallel implementation).

### Control plane

```text
POST   /v1/organizations
POST   /v1/applications
GET    /v1/applications
POST   /v1/tenants
GET    /v1/tenants
POST   /v1/applications/{app_id}/tenant-mappings
POST   /v1/api-keys
POST   /v1/api-keys/{key_id}/rotate
DELETE /v1/api-keys/{key_id}
GET    /v1/metric-definitions
PUT    /v1/metric-definitions/{metric_id}
```

### Analytics data plane

```text
POST /v1/events
GET  /v1/analytics/orders/summary?from=...&to=...
GET  /v1/analytics/orders/trend?interval=day&from=...&to=...
GET  /v1/analytics/products/top?limit=10&from=...&to=...
GET  /v1/analytics/orders/breakdown?group_by=status&from=...&to=...
GET  /v1/analytics/status
```

The query API derives tenant scope from the authenticated user/application context; it does not accept an untrusted tenant ID as an authorization mechanism.

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

- Agree on event envelope, tenant identity model and API request/response contracts.
- Create monorepo structure and the two frontend projects only.
- Add Docker Compose, environment examples, migrations and health endpoints.
- Seed one Axyra organization, one application and two mapped tenants.
- Confirm all services start from a clean checkout.

### Phase 1 — Two-hour end-to-end vertical slice

**Goal:** create an order in the reference client, ingest it into Axyra, process it, query an aggregate and prove tenant isolation.

| Time | Developer A — Go Lead (all Go services) | Developer B — Everything else |
|---|---|---|
| 0–15 min | Agree contracts, API schemas and repo skeleton together | ← same |
| 15–75 min | Go control-plane skeleton; metadata schema; seed org, app and two tenants; API-key auth; external-to-internal tenant mapping. **Then:** Go analytics skeleton; analytics schema; `POST /v1/events`; Redis queue; worker; idempotent `order.created`; summary query | Scaffold `web/` Vite app + routing shell; FastAPI reference backend scaffold; Docker Compose wiring + health-checks |
| 75–100 min | Connect ingestion → Redis → worker → PostgreSQL; expose `GET /v1/analytics/orders/summary` and `last_processed_at` | Wire React reference client to FastAPI (create-order form); FastAPI publishes event to Axyra; verify all services start cleanly |
| 100–120 min | Integration fixes; tenant isolation smoke-test | Display summary response in `web/` Overview page; rehearse full demo flow |

**Checkpoint demo:**
1. Start everything with Docker Compose.
2. Show two tenants.
3. Create an order for tenant A in the reference React app.
4. Show the event accepted and processed by Axyra.
5. Query tenant A's count/revenue and freshness.
6. Attempt to access tenant A's data as tenant B and show that it is not exposed.

At this checkpoint, a polished dashboard, all four analytics query types, rebuild, full SSO and large-scale benchmarks are not expected.

### Phase 2 — Complete control plane

- Organization/application/tenant lifecycle.
- External tenant mappings.
- API key generation, rotation and revocation.
- Policies and quotas.
- Metric configuration for supported metrics.
- Admin APIs and audit records.
- React `web` pages for overview, tenants, applications and access/configuration.

### Phase 3 — Reliable ingestion and processing

- Strict schema/version validation.
- Application authentication and trusted tenant resolution.
- Idempotency and deduplication.
- Redis retries, failure visibility and dead-letter handling.
- Worker restart safety.
- Update/cancel semantics and out-of-order event handling.
- Raw event history and processing state.

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

Tune indexes, aggregation grain, worker batch size, worker concurrency and caching based on measurements. Do not report unmeasured targets as achieved.

### Phase 8 — Final demo and documentation

The final demo should show:
1. Register an application and tenants.
2. Connect the reference client.
3. Create an order and ingest its event.
4. View updated summary and trend metrics.
5. Cancel/update an order and show corrected aggregates.
6. Show Top-N and category/status breakdown.
7. Demonstrate tenant isolation.
8. Show freshness, failed events and monitoring.
9. Show benchmark results at 1M and 10M records.

README should include prerequisites, environment variables, Compose startup/reset, migrations/seeding, API examples, event schema, tenant security model, test commands, load generation, benchmark methodology and troubleshooting.

## 13. Two-developer ownership

> **Principle:** Developer A (Go Lead) owns **all Go code** and the complex cross-cutting platform concerns. Developer B owns everything else — React web app, FastAPI reference client, Docker Compose, and documentation.

### Developer A — Go Lead (all Go + complex platform work)

**Go services (exclusive owner)**
- Go control-plane service (`services/control-plane/`) and metadata migrations.
- Organization/application/tenant registry and external-to-internal tenant mappings.
- API key generation, rotation, revocation and validation.
- Policies, quotas and metric configuration endpoints.
- Audit records for administrative changes.
- Go analytics data plane (`services/analytics/`) and analytics migrations.
- Ingestion API, payload validation, application auth and trusted tenant resolution.
- Redis queue, worker lifecycle, deduplication and retry/dead-letter handling.
- Idempotent event processing and update/cancel correctness.
- Precomputed aggregate tables (hourly/daily roll-ups).
- All PRD analytics query endpoints (FR-1 through FR-7).
- Rebuild/reconciliation (B-2), per-tenant query concurrency limits (B-3).
- Benchmark scripts and performance tuning (`scripts/benchmark/`, `scripts/load-test/`).

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
- `POST /v1/events` publisher to Axyra using a server-side credential.
- Retry logic and OpenAPI spec / Swagger UI for the reference backend.
- React reference frontend (`reference-client/frontend/`): create/view demo orders.
- Seed demo data for analytics DB (`scripts/seed/`).
- End-to-end test harness for the full event flow.

**Infrastructure and DevOps**
- Docker Compose (`deploy/docker-compose.yaml`): networking, volumes, health-checks, env examples.
- CI pipeline: lint (Go + TypeScript), test, and build steps.
- Deployment scripts and release tagging (`v1.0.0`).
- Troubleshooting and README sections for Compose startup, reset and migrations.

### Shared ownership
- Event/API contract agreed before any parallel coding begins.
- One shared design system and routing structure in `web/` (Developer B leads, Developer A contributes types).
- End-to-end integration and tenant-isolation tests.
- Docker Compose demo rehearsal.
- Project README (`docs/` and root `README.md`).

Do not create separate React apps for individual pages. Developers can work in separate feature folders/branches, but all Axyra platform pages ship from the single `web/` application.

## 14. Scope guardrails

### Must deliver
- Trusted tenant mapping and isolation.
- Secure event ingestion.
- Idempotent asynchronous processing.
- PostgreSQL precomputed aggregates.
- All FR-1 through FR-7.
- Correct update/cancel behavior.
- Docker Compose and clear README.
- 1M and 10M benchmark runs with honest results.

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

Axyra's technical core is the aggregation engine: secure ingestion, reliable event processing, correct precomputed aggregates and measurable tenant-isolated query performance. The Go control plane makes that engine reusable across applications. The single React `web/` app provides the platform management and analytics experience. The separate React reference client demonstrates how an existing product integrates with Axyra.

**Frontend count is fixed at two:** `reference-client/frontend` and `web`. All Axyra management, analytics, monitoring and configuration experiences are pages inside `web`.
