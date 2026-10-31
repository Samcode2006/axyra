# Axyra REST API Reference

**Base URL:** All endpoints are served from the control plane at the configured host (default `http://localhost:8081`).  
**Auth:** Every endpoint except `/health` and `POST /v1/auth/login` requires a valid `Authorization: Bearer <token>` console session JWT.  
**Analytics endpoints** are proxied through the control plane; tenant scope is enforced server-side before forwarding.

Analytics port 8082 is internal to the Compose network and is not published to
the host. Every `/v1/analytics/*` endpoint, including rebuild and monitoring,
requires the private `AXYRA_SERVICE_TOKEN` bearer credential. Only the control
plane supplies it, after authenticating the console user and checking tenant
access. Console JWTs and agent tokens do not authorize direct analytics queries.
`/health` remains available for health checks; `/v1/agent/*` retains agent-token
authentication.

Set the same random `AXYRA_SERVICE_TOKEN` (at least 32 characters) for Analytics
and the control plane in `deploy/.env`. Both services refuse to start without it.
Generate one locally with `python -c "import secrets; print(secrets.token_urlsafe(32))"`.
Keep this value out of frontend environment variables and source control. After
changing it, recreate both services. Use port 8081 and a console session for
queries and rebuilds; do not publish port 8082 for troubleshooting.

For configured metric execution, Analytics uses the same private service
credential to fetch `GET /internal/metric-definitions/{metric_id}?tenant_id=...`
from the control plane. This service-only endpoint derives organization scope
from the tenant; an `organization_id` supplied by a caller cannot override it.
It returns that organization's definition or a built-in definition. The public
`/v1/metric-definitions` management routes still require a console JWT.
Metric execution returns 404 for an unavailable definition, 410 when disabled,
and 502 if the definition service cannot be reached or authenticated.

---

## Authentication

### POST /v1/auth/login

Authenticate a console user and receive a session JWT.

**Request body**
```json
{ "username": "admin", "password": "..." }
```

**Response 200**
```json
{
  "token": "<jwt>",
  "user": { "id": "usr_...", "username": "admin", "role": "admin", "organization_id": "org_..." }
}
```

---

### GET /v1/auth/me

Return the authenticated caller's identity.

**Response 200** — `User` object (see above).

---

## Organizations

### GET /v1/organizations

List organizations. Operators see all; admins see only their own.

**Response 200** — `Organization[]`
```json
[{ "id": "org_...", "name": "Acme Corp", "created_at": "2026-10-31T00:00:00Z" }]
```

---

### POST /v1/organizations

Create an organization (operators only).

**Request body**
```json
{ "name": "Acme Corp" }
```

**Response 201** — `Organization`

---

### GET /v1/organizations/{id}

Get a single organization.

---

### PATCH /v1/organizations/{id}

Rename an organization.

**Request body**
```json
{ "name": "New Name" }
```

---

### DELETE /v1/organizations/{id}

Delete an organization and all its resources.

---

### GET /v1/organizations/{id}/users

List console users in the organization.

**Response 200** — `User[]`

---

### POST /v1/organizations/{id}/users

Create a console user in the organization.

**Request body**
```json
{ "username": "alice", "password": "s3cr3t" }
```

**Response 201** — `User`

---

### DELETE /v1/organizations/{id}/users/{user_id}

Remove a console user.

---

## Applications

### GET /v1/applications

List connected applications visible to the caller.

**Response 200** — `Application[]`

---

### POST /v1/applications

Register a new connected application.

**Request body**
```json
{ "name": "My Shop", "description": "Optional", "organization_id": "org_..." }
```

**Response 201** — `Application`

---

### GET /v1/applications/{id}

Get a single application.

---

### PATCH /v1/applications/{id}

Update an application's name or description.

---

### DELETE /v1/applications/{id}

Delete an application (cascades to its agents).

---

## Agents

### GET /v1/agents

List agents visible to the caller. Optional `?application_id=` filter.

**Response 200** — `Agent[]`
```json
[{
  "id": "agent_ref_demo",
  "application_id": "app_...",
  "name": "Reference demo agent",
  "status": "connected",
  "last_seen_at": "2026-10-31T13:00:00Z",
  "created_at": "2026-10-31T00:00:00Z"
}]
```

**Agent status values:** `connected` · `disconnected` · `error`

---

### POST /v1/agents

Register a new agent instance.

**Request body**
```json
{ "application_id": "app_...", "name": "Production agent" }
```

**Response 201** — `Agent`

---

### GET /v1/agents/{agent_id}

Get a single agent.

---

### PATCH /v1/agents/{agent_id}

Rename an agent.

**Request body**
```json
{ "name": "New name" }
```

---

### DELETE /v1/agents/{agent_id}

Delete an agent and all its tokens and tenant mappings.

---

### GET /v1/agents/{agent_id}/tokens

List credential metadata for an agent. The secret is never returned after issue.

**Response 200** — `AgentToken[]`
```json
[{
  "id": "tok_...",
  "agent_id": "agent_...",
  "name": "Token created at ...",
  "token_prefix": "axyra_agt_a1b2c3d4",
  "status": "active",
  "created_at": "...",
  "expires_at": null,
  "revoked_at": null
}]
```

**Token status values:** `active` · `expired` · `revoked`

---

### POST /v1/agents/{agent_id}/tokens

Issue a new agent token. The plaintext is in the response **once only**.

**Request body** (optional)
```json
{ "name": "Production token", "expires_in_days": 365 }
```

**Response 201**
```json
{
  "token_id": "tok_...",
  "token": "axyra_agt_<hex>",
  "token_prefix": "axyra_agt_a1b2c3d4",
  "expires_at": "2027-10-31T00:00:00Z"
}
```

---

### POST /v1/agents/{agent_id}/tokens/{token_id}/rotate

Replace a token with a new one. An optional grace period lets the old token remain valid briefly during agent redeployment.

**Request body** (optional)
```json
{ "grace_period_seconds": 300, "expires_in_days": 365 }
```

**Response 201** — `RotateTokenResponse` (includes `previous_token_id` and `previous_token_expires_at`)

---

### DELETE /v1/agents/{agent_id}/tokens/{token_id}

Immediately revoke a token.

**Response 200** — `{ "status": "revoked" }`

---

## Tenant Mappings

### GET /v1/agents/{agent_id}/tenant-mappings

List external-to-canonical tenant mappings for an agent.

**Response 200** — `TenantMapping[]`
```json
[{
  "id": "map_...",
  "agent_id": "agent_...",
  "external_tenant_id": "store_001",
  "tenant_id": "tenant_...",
  "created_at": "..."
}]
```

---

### POST /v1/agents/{agent_id}/tenant-mappings

Create or update a mapping. Re-posting an existing `external_tenant_id` updates the target.

**Request body**
```json
{ "external_tenant_id": "store_001", "tenant_id": "tenant_..." }
```

**Response 201** (new) or **200** (updated) — `TenantMapping`

---

### DELETE /v1/agents/{agent_id}/tenant-mappings/{mapping_id}

Remove a tenant mapping.

---

## Tenants

### GET /v1/tenants

List tenants visible to the caller. Optional query params: `?page=1&per_page=50&status=active`.

**Response 200** — paginated `Tenant[]`
```json
{
  "data": [{
    "id": "tenant_...",
    "organization_id": "org_...",
    "external_tenant_id": "store_001",
    "name": "Store 1",
    "status": "active",
    "quota": { "max_events_per_day": 100000, "max_concurrent_queries": 5 },
    "created_at": "..."
  }],
  "total": 42,
  "page": 1,
  "per_page": 50
}
```

**Tenant status values:** `active` · `suspended` · `inactive`

---

### POST /v1/tenants

Register a tenant.

**Request body**
```json
{
  "external_tenant_id": "store_002",
  "name": "Store 2",
  "organization_id": "org_...",
  "quota": { "max_events_per_day": 50000, "max_concurrent_queries": 3 }
}
```

**Response 201** — `Tenant`

---

### GET /v1/tenants/{id}

Get a single tenant.

---

### PATCH /v1/tenants/{id}

Update tenant name, status or quota.

**Request body** (all fields optional)
```json
{ "name": "New name", "status": "suspended", "quota": { "max_events_per_day": 10000 } }
```

---

### DELETE /v1/tenants/{id}

Delete a tenant.

---

## Policies

### GET /v1/policies

List access policies.

**Response 200** — `Policy[]`

---

### POST /v1/policies

Create a policy.

**Request body**
```json
{
  "tenant_id": "tenant_...",
  "name": "Rate limit store_001",
  "type": "rate_limit",
  "config": { "requests_per_minute": 100 }
}
```

**Policy type values:** `rate_limit` · `query_limit` · `data_access`

---

### PATCH /v1/policies/{id}

Update a policy.

---

### DELETE /v1/policies/{id}

Delete a policy.

---

## Metric Definitions

### GET /v1/metric-definitions

List supported metric definitions (built-in and organization-specific).

**Response 200** — `MetricDefinition[]`

---

### GET /v1/metric-definitions/{metric_id}

Get a single metric definition.

---

### PUT /v1/metric-definitions/{metric_id}

Create or replace a metric definition.

**Request body**
```json
{
  "name": "Gross Revenue",
  "description": "Sum of non-cancelled order amounts",
  "config": {
    "source": "orders",
    "aggregation": "sum",
    "field": "amount",
    "filters": [{ "field": "status", "op": "neq", "value": "cancelled" }],
    "granularity": ["hour", "day"]
  },
  "enabled": true,
  "organization_id": "org_..."
}
```

---

### DELETE /v1/metric-definitions/{metric_id}

Delete a metric definition (organization-owned only).

---

## Audit Logs

### GET /v1/audit-logs

Paginated audit log. Optional: `?limit=50&before_id=<n>&resource_type=agent&action=agent_token.revoke`.

**Response 200**
```json
{
  "data": [{
    "id": 42,
    "organization_id": "org_...",
    "actor_id": "usr_...",
    "actor_name": "admin",
    "actor_role": "admin",
    "action": "agent_token.revoke",
    "resource_type": "agent_token",
    "resource_id": "tok_...",
    "details": { "agent_id": "agent_...", "token_prefix": "axyra_agt_a1b2c3d4" },
    "created_at": "..."
  }],
  "next_before_id": 41
}
```

---

## Analytics (FR-1 through FR-7)

All analytics endpoints are scoped to the authenticated user's organization. Tenant scope is injected server-side from the session; the browser cannot supply an arbitrary tenant ID.

**Common query params:**
- `from` — RFC 3339 UTC timestamp (default: 7 days ago)
- `to` — RFC 3339 UTC timestamp (default: now)
- `tenant_id` — optional; defaults to the organization's oldest active tenant

---

### GET /v1/analytics/orders/summary (FR-1)

Counts and totals for the selected time range.

**Response 200**
```json
{
  "tenant_id": "tenant_...",
  "from": "2026-10-24T00:00:00Z",
  "to": "2026-10-31T00:00:00Z",
  "total_orders": 4200,
  "total_revenue": 5189000.00,
  "currency": "INR",
  "active_orders": 3900,
  "cancelled_orders": 300,
  "last_processed_at": "2026-10-31T13:38:00Z"
}
```

---

### GET /v1/analytics/orders/trend (FR-2)

Hourly or daily order counts and revenue trend.

**Query params:** `interval=hour|day` (default `day`)

**Response 200**
```json
{
  "tenant_id": "tenant_...",
  "interval": "day",
  "data": [
    { "bucket": "2026-10-25T00:00:00Z", "order_count": 590, "revenue": 720000.00 }
  ]
}
```

---

### GET /v1/analytics/products/top (FR-3)

Top-N products by revenue in the time range.

**Query params:** `limit=10` (default, max 100)

**Response 200**
```json
{
  "tenant_id": "tenant_...",
  "data": [
    { "product_id": "prod_1", "product_name": "Widget Pro", "category": "Electronics", "order_count": 340, "revenue": 850000.00 }
  ]
}
```

---

### GET /v1/analytics/orders/breakdown (FR-4)

Count and revenue grouped by status or category.

**Query params:** `group_by=status|category` (default `status`)

**Response 200**
```json
{
  "tenant_id": "tenant_...",
  "group_by": "status",
  "data": [
    { "group": "placed",    "count": 3200, "revenue": 3900000.00 },
    { "group": "shipped",   "count": 700,  "revenue": 850000.00  },
    { "group": "cancelled", "count": 300,  "revenue": 0.00       }
  ]
}
```

---

### GET /v1/analytics/status (FR-5 / B-1)

Data freshness and pipeline health for the tenant.

**Response 200**
```json
{
  "tenant_id": "tenant_...",
  "last_processed_at": "2026-10-31T13:38:00Z",
  "last_lsn": "0/1A2B3C4",
  "lag_seconds": 12.5,
  "is_healthy": true
}
```

`is_healthy` is `false` when `lag_seconds >= 300` (5 minutes).

---

### GET /v1/analytics/dead-letter

Dead-letter events that failed validation or exhausted processing retries.

**Query params:** `agent_id=<id>` (optional filter), `limit=100` (max 1000)

**Response 200** — `DeadLetterEvent[]`
```json
[{
  "id": 7,
  "agent_id": "agent_ref_demo",
  "tenant_id": "tenant_...",
  "event_id": "3fa85f64-...",
  "lsn": "0/1A2B3C4",
  "reason": "exhausted 5 retries; last error: ...",
  "payload": { ... },
  "retry_count": 5,
  "replayed_at": null,
  "created_at": "2026-10-31T13:00:00Z"
}]
```

---

## Agent Ingestion (internal — not called from the browser)

These endpoints are authenticated with an **agent token** (`Authorization: Bearer axyra_agt_<hex>`), not a console JWT.

### POST /v1/agent/events

Batch-ingest WAL change events from an Axyra Agent.

**Headers:** `Authorization: Bearer <agent_token>`

**Request body**
```json
{
  "events": [{
    "event_id": "<uuid>",
    "agent_id": "agent_ref_demo",
    "external_tenant_id": "store_001",
    "event_type": "order.created",
    "schema_version": 1,
    "occurred_at": "2026-10-31T13:00:00Z",
    "lsn": "0/1A2B3C4",
    "data": {
      "order_id": "order_1001",
      "amount": 1299.00,
      "currency": "INR",
      "status": "placed",
      "product_id": "prod_42",
      "product_name": "Widget Pro",
      "category": "Electronics"
    }
  }]
}
```

**Response 202**
```json
{ "status": "accepted", "count": 1, "rejected": 0, "last_lsn": "0/1A2B3C4" }
```

`rejected` counts events that failed schema validation and were routed to the dead-letter table.

---

### GET /v1/agent/ack

Return the last confirmed LSN for the agent. The agent uses this to resume WAL replication after a restart.

**Response 200**
```json
{
  "agent_id": "agent_ref_demo",
  "confirmed_lsn": "0/1A2B3C4",
  "last_processed_at": "2026-10-31T13:38:00Z"
}
```

---

## Event schema

All events sent by the agent follow this envelope (schema_version 1):

| Field | Type | Required | Description |
|---|---|---|---|
| `event_id` | string (UUID) | ✓ | Deterministic UUID derived from `LSN:table:primary_key` |
| `agent_id` | string | ✓ | Registered agent ID |
| `external_tenant_id` | string | ✓ | Raw tenant column value from the source row |
| `event_type` | string | ✓ | `order.created` / `order.updated` / `order.cancelled` / `order.deleted` |
| `schema_version` | int | ✓ | Always `1` for this version |
| `occurred_at` | RFC 3339 | ✓ | Timestamp of the source database change |
| `lsn` | string | ✓ | PostgreSQL Log Sequence Number (or `wh/<uuid>` for webhook events) |
| `data` | object | ✓ | Table column values (filtered by the agent's column list) |

---

## Error format

All error responses use a consistent envelope:

```json
{ "error": { "message": "Human-readable description" } }
```

Common HTTP status codes:
- `400 Bad Request` — malformed payload or invalid parameters
- `401 Unauthorized` — missing or expired token
- `403 Forbidden` — authenticated but not authorized
- `404 Not Found` — resource does not exist or is not in scope
- `409 Conflict` — uniqueness violation (e.g. duplicate name)
- `500 Internal Server Error` — unexpected server failure
