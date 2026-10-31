# Axyra — Full Schema & Metrics Expansion Plan

**Status:** Design document — not yet implemented  
**Based on:** Code audit of all layers as of 31 October 2026  
**Purpose:** Define every data field, aggregate table, API endpoint, and frontend panel needed to make Axyra a complete, detailed analytics platform — not just a toy order counter.

---

## 0. Current state (ground truth)

Everything below is derived from reading the actual source files. No speculation.

### Reference DB `orders` — 8 columns
```
id, store_id, amount, currency, status,
product_id, product_name, category
```

### Analytics `order_state` — 10 columns (same 8 + timestamps)
```
order_id, tenant_id, amount, currency, status,
product_id, product_name, category, occurred_at, updated_at
```

### Precomputed aggregate tables — 2 tables
```
order_summary_hourly / order_summary_daily
  → total_orders, active_orders, cancelled_orders, total_revenue

product_aggregates
  → order_count, total_revenue
```

### Analytics API — 5 endpoints
```
GET /v1/analytics/orders/summary
GET /v1/analytics/orders/trend
GET /v1/analytics/products/top
GET /v1/analytics/orders/breakdown   (group_by = status | category only)
GET /v1/analytics/status
```

### Event envelope `data` — 7 known fields read by worker
```
order_id, amount, currency, status, product_id, product_name, category
```

### Agent column watch-list — 10 columns
```
id, store_id, amount, currency, status,
product_id, product_name, category, created_at, updated_at
```

### Frontend Analytics page — 4 panels
```
4 stat cards (total/active/cancelled/revenue)
Bar chart trend (order count + revenue)
Top products table
Status / category breakdown table
```

---

## 1. Reference DB — 16 new columns on `orders`

These columns make every order a rich, multi-dimensional event. Each one unlocks at least one new analytics dimension on the platform.

```sql
ALTER TABLE orders
  -- Customer identity
  ADD COLUMN customer_id        VARCHAR(64)    NOT NULL DEFAULT '',
  ADD COLUMN customer_email     VARCHAR(255)   NOT NULL DEFAULT '',
      -- stored hashed or partially masked; never shown in full on analytics pages
  ADD COLUMN customer_tier      VARCHAR(32)    NOT NULL DEFAULT 'regular',
      -- free | silver | gold | platinum

  -- Transaction financial detail
  ADD COLUMN payment_method     VARCHAR(32)    NOT NULL DEFAULT 'card',
      -- card | upi | wallet | cod | bnpl | netbanking
  ADD COLUMN payment_status     VARCHAR(32)    NOT NULL DEFAULT 'paid',
      -- pending | paid | failed | refunded | chargeback
  ADD COLUMN discount_code      VARCHAR(64)    NOT NULL DEFAULT '',
  ADD COLUMN discount_amount    NUMERIC(10,2)  NOT NULL DEFAULT 0,
  ADD COLUMN tax_amount         NUMERIC(10,2)  NOT NULL DEFAULT 0,
  ADD COLUMN shipping_cost      NUMERIC(10,2)  NOT NULL DEFAULT 0,

  -- Item-level detail
  ADD COLUMN quantity           INT            NOT NULL DEFAULT 1,
  ADD COLUMN sku                VARCHAR(64)    NOT NULL DEFAULT '',

  -- Fulfilment lifecycle
  ADD COLUMN fulfillment_status VARCHAR(32)    NOT NULL DEFAULT 'pending',
      -- pending | processing | shipped | delivered | returned
  ADD COLUMN return_reason      VARCHAR(128)   NOT NULL DEFAULT '',
      -- populated when fulfillment_status = 'returned'

  -- Geography
  ADD COLUMN country            CHAR(2)        NOT NULL DEFAULT 'IN',
      -- ISO 3166-1 alpha-2
  ADD COLUMN state_province     VARCHAR(64)    NOT NULL DEFAULT '',
  ADD COLUMN city               VARCHAR(64)    NOT NULL DEFAULT '',

  -- Acquisition / attribution
  ADD COLUMN channel            VARCHAR(32)    NOT NULL DEFAULT 'web',
      -- web | mobile_app | pos | marketplace | api
  ADD COLUMN referrer_source    VARCHAR(64)    NOT NULL DEFAULT '';
      -- google | instagram | direct | email | affiliate | whatsapp
```

### Why each column matters

| Column | Analytics it unlocks |
|---|---|
| `customer_id` | Customer LTV, repeat purchase rate, new vs returning buyers, top buyers table |
| `customer_email` | Masked display only — never used as a group-by dimension |
| `customer_tier` | Revenue by tier, tier upgrade conversion rate, tier-level AOV |
| `payment_method` | Payment method split, failure rate per method, preferred method trends |
| `payment_status` | Failed payment rate, chargeback rate, refund volume |
| `discount_code / discount_amount` | Discount impact on GMV, promo code effectiveness, net revenue vs gross |
| `tax_amount` | Tax contribution to total collected, compliance reporting |
| `shipping_cost` | Shipping revenue vs cost proxy, free-shipping threshold analysis |
| `quantity / sku` | Units sold per product, inventory velocity, basket size |
| `fulfillment_status` | Fulfilment funnel (pending → delivered), bottleneck identification |
| `return_reason` | Return rate by reason, product quality signal, defect hotspots |
| `country / state / city` | Geographic revenue map, top cities, cross-border share |
| `channel` | Channel attribution, mobile vs web vs POS split |
| `referrer_source` | Marketing ROI proxy, acquisition channel performance |

### Updated `OrderCreate` / `OrderUpdate` schemas (FastAPI)

```python
class OrderCreate(BaseModel):
    store_id: str
    amount: float
    currency: str = "INR"
    product_id: str
    product_name: str
    category: str
    sku: str = ""
    quantity: int = 1
    customer_id: str = ""
    customer_tier: str = "regular"      # free | silver | gold | platinum
    payment_method: str = "card"        # card | upi | wallet | cod | bnpl | netbanking
    discount_code: str = ""
    discount_amount: float = 0.0
    tax_amount: float = 0.0
    shipping_cost: float = 0.0
    channel: str = "web"                # web | mobile_app | pos | marketplace | api
    referrer_source: str = ""
    country: str = "IN"
    state_province: str = ""
    city: str = ""

class OrderUpdate(BaseModel):
    status: Literal["placed","confirmed","shipped","delivered","cancelled","returned"]
    fulfillment_status: Optional[str] = None
    return_reason: Optional[str] = None
    payment_status: Optional[str] = None
```

---

## 2. Agent column watch-list expansion

[`agent/internal/config/config.go`](../agent/internal/config/config.go) default columns list must
expand from 10 to 27:

```yaml
tables:
  - name: orders
    tenant_id_column: store_id
    pk_column: id
    event_type_column: status
    columns:
      - id
      - store_id
      - amount
      - currency
      - status
      - product_id
      - product_name
      - category
      - customer_id
      - customer_tier
      - payment_method
      - payment_status
      - discount_code
      - discount_amount
      - tax_amount
      - shipping_cost
      - quantity
      - sku
      - fulfillment_status
      - return_reason
      - country
      - state_province
      - city
      - channel
      - referrer_source
      - created_at
      - updated_at
```

---

## 3. Event envelope — 27-field `data` payload

After the schema expansion every event carries the full order picture. The worker reads all 27 fields; unknown fields are stored in `raw_events.data` JSONB and ignored by aggregation (forward-compatible).

```json
{
  "event_id": "evt_<lsn-derived-uuid>",
  "agent_id": "agent_ref_demo",
  "external_tenant_id": "store_001",
  "event_type": "order.created",
  "schema_version": 2,
  "occurred_at": "2026-10-31T10:00:00Z",
  "lsn": "0/1A2B3C4",
  "data": {
    "order_id":          "order_abc123",
    "store_id":          "store_001",
    "amount":            4999.00,
    "currency":          "INR",
    "status":            "placed",
    "product_id":        "prod_006",
    "product_name":      "Smart Watch",
    "category":          "Electronics",
    "sku":               "SW-006-BLK-M",
    "quantity":          1,
    "customer_id":       "cust_99f12a",
    "customer_tier":     "gold",
    "payment_method":    "upi",
    "payment_status":    "paid",
    "discount_code":     "SALE20",
    "discount_amount":   999.80,
    "tax_amount":        720.73,
    "shipping_cost":     0.00,
    "fulfillment_status":"processing",
    "return_reason":     "",
    "country":           "IN",
    "state_province":    "Maharashtra",
    "city":              "Mumbai",
    "channel":           "mobile_app",
    "referrer_source":   "instagram",
    "created_at":        "2026-10-31T10:00:00Z",
    "updated_at":        "2026-10-31T10:00:00Z"
  }
}
```

Schema version bumps to **2** when the new fields first appear. The ingestion endpoint continues to accept version 1 for backward compatibility.

---

## 4. Analytics DB — full schema expansion

### 4.1 Expand `order_state` — add 17 columns

```sql
ALTER TABLE order_state
  ADD COLUMN customer_id        VARCHAR(64)    NOT NULL DEFAULT '',
  ADD COLUMN customer_tier      VARCHAR(32)    NOT NULL DEFAULT 'regular',
  ADD COLUMN payment_method     VARCHAR(32)    NOT NULL DEFAULT 'card',
  ADD COLUMN payment_status     VARCHAR(32)    NOT NULL DEFAULT 'paid',
  ADD COLUMN discount_code      VARCHAR(64)    NOT NULL DEFAULT '',
  ADD COLUMN discount_amount    NUMERIC(10,2)  NOT NULL DEFAULT 0,
  ADD COLUMN tax_amount         NUMERIC(10,2)  NOT NULL DEFAULT 0,
  ADD COLUMN shipping_cost      NUMERIC(10,2)  NOT NULL DEFAULT 0,
  ADD COLUMN quantity           INT            NOT NULL DEFAULT 1,
  ADD COLUMN sku                VARCHAR(64)    NOT NULL DEFAULT '',
  ADD COLUMN fulfillment_status VARCHAR(32)    NOT NULL DEFAULT 'pending',
  ADD COLUMN return_reason      VARCHAR(128)   NOT NULL DEFAULT '',
  ADD COLUMN country            CHAR(2)        NOT NULL DEFAULT 'IN',
  ADD COLUMN state_province     VARCHAR(64)    NOT NULL DEFAULT '',
  ADD COLUMN city               VARCHAR(64)    NOT NULL DEFAULT '',
  ADD COLUMN channel            VARCHAR(32)    NOT NULL DEFAULT 'web',
  ADD COLUMN referrer_source    VARCHAR(64)    NOT NULL DEFAULT '';
```

All existing correction logic (cancel reversals, update deltas) must account for these new numeric fields: `discount_amount`, `tax_amount`, `shipping_cost`, `quantity`.

### 4.2 Expand `order_summary_hourly` and `order_summary_daily`

```sql
ALTER TABLE order_summary_hourly
  ADD COLUMN returned_orders     BIGINT        NOT NULL DEFAULT 0,
  ADD COLUMN total_discount      NUMERIC(14,2) NOT NULL DEFAULT 0,
  ADD COLUMN total_tax           NUMERIC(14,2) NOT NULL DEFAULT 0,
  ADD COLUMN total_shipping      NUMERIC(14,2) NOT NULL DEFAULT 0,
  ADD COLUMN total_quantity      BIGINT        NOT NULL DEFAULT 0,
  ADD COLUMN new_customer_orders BIGINT        NOT NULL DEFAULT 0;
  -- new_customer_orders: orders from a customer_id seeing their first ever event for this tenant

ALTER TABLE order_summary_daily
  ADD COLUMN returned_orders     BIGINT        NOT NULL DEFAULT 0,
  ADD COLUMN total_discount      NUMERIC(14,2) NOT NULL DEFAULT 0,
  ADD COLUMN total_tax           NUMERIC(14,2) NOT NULL DEFAULT 0,
  ADD COLUMN total_shipping      NUMERIC(14,2) NOT NULL DEFAULT 0,
  ADD COLUMN total_quantity      BIGINT        NOT NULL DEFAULT 0,
  ADD COLUMN new_customer_orders BIGINT        NOT NULL DEFAULT 0;
```

Derived values calculable at query time (no extra storage):
- `net_revenue = total_revenue - total_discount`
- `avg_order_value = total_revenue / NULLIF(total_orders, 0)`
- `return_rate_pct = returned_orders * 100.0 / NULLIF(total_orders, 0)`

### 4.3 Expand `product_aggregates`

```sql
ALTER TABLE product_aggregates
  ADD COLUMN quantity_sold    BIGINT        NOT NULL DEFAULT 0,
  ADD COLUMN return_count     BIGINT        NOT NULL DEFAULT 0,
  ADD COLUMN total_discount   NUMERIC(14,2) NOT NULL DEFAULT 0;
  -- avg_unit_price = total_revenue / NULLIF(quantity_sold, 0)  (computed at query time)
```

### 4.4 New aggregate table: `customer_aggregates`

```sql
CREATE TABLE IF NOT EXISTS customer_aggregates (
    tenant_id       VARCHAR(64)    NOT NULL,
    customer_id     VARCHAR(64)    NOT NULL,
    customer_tier   VARCHAR(32)    NOT NULL DEFAULT 'regular',
    order_count     BIGINT         NOT NULL DEFAULT 0,
    total_spend     NUMERIC(14,2)  NOT NULL DEFAULT 0,
    total_discount  NUMERIC(14,2)  NOT NULL DEFAULT 0,
    total_quantity  BIGINT         NOT NULL DEFAULT 0,
    first_order_at  TIMESTAMPTZ,
    last_order_at   TIMESTAMPTZ,
    PRIMARY KEY (tenant_id, customer_id)
);

CREATE INDEX IF NOT EXISTS idx_customer_agg_spend
    ON customer_aggregates(tenant_id, total_spend DESC);
CREATE INDEX IF NOT EXISTS idx_customer_agg_last
    ON customer_aggregates(tenant_id, last_order_at DESC);
```

### 4.5 New aggregate table: `geo_aggregates`

```sql
CREATE TABLE IF NOT EXISTS geo_aggregates (
    tenant_id      VARCHAR(64)    NOT NULL,
    bucket         DATE           NOT NULL,
    country        CHAR(2)        NOT NULL,
    state_province VARCHAR(64)    NOT NULL DEFAULT '',
    city           VARCHAR(64)    NOT NULL DEFAULT '',
    order_count    BIGINT         NOT NULL DEFAULT 0,
    total_revenue  NUMERIC(14,2)  NOT NULL DEFAULT 0,
    total_quantity BIGINT         NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, bucket, country, state_province, city)
);

CREATE INDEX IF NOT EXISTS idx_geo_agg_country
    ON geo_aggregates(tenant_id, bucket, country);
```

### 4.6 New aggregate table: `channel_aggregates`

```sql
CREATE TABLE IF NOT EXISTS channel_aggregates (
    tenant_id       VARCHAR(64)    NOT NULL,
    bucket          DATE           NOT NULL,
    channel         VARCHAR(32)    NOT NULL,
    referrer_source VARCHAR(64)    NOT NULL DEFAULT '',
    order_count     BIGINT         NOT NULL DEFAULT 0,
    total_revenue   NUMERIC(14,2)  NOT NULL DEFAULT 0,
    new_customers   BIGINT         NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, bucket, channel, referrer_source)
);
```

### 4.7 New aggregate table: `payment_aggregates`

```sql
CREATE TABLE IF NOT EXISTS payment_aggregates (
    tenant_id      VARCHAR(64)    NOT NULL,
    bucket         DATE           NOT NULL,
    payment_method VARCHAR(32)    NOT NULL,
    payment_status VARCHAR(32)    NOT NULL,
    order_count    BIGINT         NOT NULL DEFAULT 0,
    total_amount   NUMERIC(14,2)  NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, bucket, payment_method, payment_status)
);
```

### 4.8 New aggregate table: `return_aggregates`

```sql
CREATE TABLE IF NOT EXISTS return_aggregates (
    tenant_id     VARCHAR(64)    NOT NULL,
    bucket        DATE           NOT NULL,
    return_reason VARCHAR(128)   NOT NULL DEFAULT '',
    return_count  BIGINT         NOT NULL DEFAULT 0,
    refund_amount NUMERIC(14,2)  NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, bucket, return_reason)
);
```

### 4.9 New aggregate table: `fulfillment_aggregates`

```sql
CREATE TABLE IF NOT EXISTS fulfillment_aggregates (
    tenant_id          VARCHAR(64)  NOT NULL,
    bucket             DATE         NOT NULL,
    fulfillment_status VARCHAR(32)  NOT NULL,
    order_count        BIGINT       NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, bucket, fulfillment_status)
);
```

### 4.10 New aggregate table: `cohort_daily`

```sql
CREATE TABLE IF NOT EXISTS cohort_daily (
    tenant_id     VARCHAR(64)    NOT NULL,
    bucket        DATE           NOT NULL,
    customer_type VARCHAR(16)    NOT NULL,   -- 'new' | 'returning'
    order_count   BIGINT         NOT NULL DEFAULT 0,
    total_revenue NUMERIC(14,2)  NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, bucket, customer_type)
);
```

### 4.11 New operational table: `op_ingestion_rate`

For the monitoring page throughput sparkline (events/minute over the last 60 minutes).

```sql
CREATE TABLE IF NOT EXISTS op_ingestion_rate (
    bucket      TIMESTAMPTZ  NOT NULL,   -- truncated to the minute
    event_count BIGINT       NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket)
);

CREATE INDEX IF NOT EXISTS idx_op_ingestion_rate_time
    ON op_ingestion_rate(bucket DESC);
```

The worker upserts one row per minute:
```sql
INSERT INTO op_ingestion_rate (bucket, event_count)
VALUES (DATE_TRUNC('minute', NOW()), 1)
ON CONFLICT (bucket) DO UPDATE SET event_count = op_ingestion_rate.event_count + 1;
```

---

## 5. Control-plane `tenants` table — 5 new columns

```sql
ALTER TABLE tenants
  ADD COLUMN IF NOT EXISTS region        VARCHAR(32)  NOT NULL DEFAULT 'global',
      -- apac | emea | americas | global
  ADD COLUMN IF NOT EXISTS plan_tier     VARCHAR(32)  NOT NULL DEFAULT 'growth',
      -- free | growth | enterprise
  ADD COLUMN IF NOT EXISTS billing_cycle VARCHAR(16)  NOT NULL DEFAULT 'monthly',
      -- monthly | annual
  ADD COLUMN IF NOT EXISTS tags          TEXT[]       NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS metadata      JSONB        NOT NULL DEFAULT '{}';
```

---

## 6. Analytics API — 11 new endpoints

All endpoints are tenant-scoped (derive tenant from authenticated session), use precomputed aggregates, and honour `?from=&to=` date ranges.

### 6.1 Revenue breakdown
```
GET /v1/analytics/orders/revenue?from=&to=

Response:
{
  "tenant_id": "...",
  "from": "...", "to": "...",
  "gross_revenue": 9820000.00,
  "total_discount": 420000.00,
  "net_revenue": 9400000.00,
  "total_tax": 1692000.00,
  "total_shipping": 88000.00,
  "avg_order_value": 982.00,
  "total_quantity": 48200
}
```

### 6.2 Customer summary
```
GET /v1/analytics/customers/summary?from=&to=

Response:
{
  "tenant_id": "...",
  "total_customers": 8420,
  "new_customers": 312,
  "returning_customers": 8108,
  "new_customer_rate_pct": 3.7,
  "avg_ltv": 11650.00,
  "avg_order_frequency": 2.3,
  "avg_orders_per_customer": 2.3
}
```

### 6.3 Top buyers
```
GET /v1/analytics/customers/top?limit=20&from=&to=

Response: {
  "tenant_id": "...",
  "data": [
    {
      "customer_id": "cust_99f12a",
      "customer_tier": "platinum",
      "order_count": 18,
      "total_spend": 182400.00,
      "total_discount": 12000.00,
      "first_order_at": "...",
      "last_order_at": "..."
    }
  ]
}
```

### 6.4 New vs returning cohort trend
```
GET /v1/analytics/customers/cohorts?from=&to=&interval=day|week

Response: {
  "tenant_id": "...",
  "interval": "day",
  "data": [
    {
      "bucket": "2026-10-31",
      "new_orders": 120,
      "returning_orders": 380,
      "new_revenue": 98400.00,
      "returning_revenue": 312000.00
    }
  ]
}
```

### 6.5 Geographic breakdown
```
GET /v1/analytics/geo/breakdown?from=&to=&group_by=country|state|city

Response: {
  "tenant_id": "...",
  "group_by": "country",
  "data": [
    {
      "group": "IN",
      "order_count": 8200,
      "total_revenue": 7820000.00,
      "share_pct": 79.6
    }
  ]
}
```

### 6.6 Channel attribution
```
GET /v1/analytics/channels/breakdown?from=&to=

Response: {
  "tenant_id": "...",
  "data": [
    {
      "channel": "mobile_app",
      "referrer_source": "instagram",
      "order_count": 1820,
      "total_revenue": 1640000.00,
      "new_customers": 88
    }
  ]
}
```

### 6.7 Payment method health
```
GET /v1/analytics/payments/breakdown?from=&to=

Response: {
  "tenant_id": "...",
  "data": [
    {
      "payment_method": "upi",
      "total_orders": 3200,
      "paid_orders": 3072,
      "failed_orders": 96,
      "refunded_orders": 32,
      "chargeback_orders": 0,
      "total_amount": 2880000.00,
      "failure_rate_pct": 3.0
    }
  ]
}
```

### 6.8 Returns and refunds summary
```
GET /v1/analytics/returns/summary?from=&to=

Response: {
  "tenant_id": "...",
  "total_returns": 420,
  "total_refund_amount": 380000.00,
  "return_rate_pct": 4.3,
  "top_reasons": [
    { "reason": "defective",    "count": 147, "share_pct": 35.0 },
    { "reason": "wrong_item",   "count": 105, "share_pct": 25.0 },
    { "reason": "changed_mind", "count": 84,  "share_pct": 20.0 },
    { "reason": "damaged",      "count": 63,  "share_pct": 15.0 },
    { "reason": "other",        "count": 21,  "share_pct":  5.0 }
  ]
}
```

### 6.9 Fulfilment funnel
```
GET /v1/analytics/fulfillment/funnel?from=&to=

Response: {
  "tenant_id": "...",
  "data": [
    { "stage": "pending",     "order_count": 9820, "pct_of_total": 100.0 },
    { "stage": "processing",  "order_count": 9600, "pct_of_total": 97.8 },
    { "stage": "shipped",     "order_count": 8900, "pct_of_total": 90.6 },
    { "stage": "delivered",   "order_count": 8200, "pct_of_total": 83.5 },
    { "stage": "returned",    "order_count":  420, "pct_of_total":  4.3 }
  ]
}
```

### 6.10 Discount / promo effectiveness
```
GET /v1/analytics/discounts/summary?from=&to=

Response: {
  "tenant_id": "...",
  "orders_with_discount": 2450,
  "discount_rate_pct": 25.0,
  "total_discount_given": 420000.00,
  "avg_discount_per_order": 171.43,
  "top_codes": [
    { "code": "SALE20", "uses": 820, "total_discount": 148000.00 },
    { "code": "FLAT100", "uses": 640, "total_discount": 64000.00 }
  ]
}
```

### 6.11 Enriched top products
```
GET /v1/analytics/products/top?limit=20&from=&to=&sort_by=revenue|quantity|returns

Response per item:
{
  "product_id": "prod_006",
  "product_name": "Smart Watch",
  "category": "Electronics",
  "order_count": 1820,
  "quantity_sold": 1920,
  "total_revenue": 9100000.00,
  "total_discount": 280000.00,
  "return_count": 62,
  "return_rate_pct": 3.4,
  "avg_unit_price": 4739.58
}
```

---

## 7. Expand existing `breakdown` endpoint

Current accepted `group_by` values: `status`, `category`.  
After expansion:

```
group_by =
  status              (existing)
  category            (existing)
  payment_method      (new)
  channel             (new)
  country             (new)
  customer_tier       (new)
  fulfillment_status  (new)
  return_reason       (new)
  referrer_source     (new)
```

Each new group resolves against `order_state` with a time filter, or against the relevant precomputed aggregate table for all-time queries.

---

## 8. Metric config allowlist expansion

[`services/analytics/internal/handlers/metricquery.go`](../services/analytics/internal/handlers/metricquery.go) currently enforces these allowlists:

### Current
```
numeric fields:    amount
distinct fields:   status, category, product_id, currency
group_by:          status, category, product_id, currency
filters:           status, category, currency, product_id
```

### After expansion
```
numeric fields:    amount, discount_amount, tax_amount, shipping_cost, quantity

distinct fields:   status, category, product_id, currency,
                   payment_method, channel, country,
                   customer_tier, fulfillment_status, sku

group_by:          status, category, product_id, currency,
                   payment_method, channel, country,
                   customer_tier, fulfillment_status, referrer_source

filters:           status, category, currency, product_id,
                   payment_method, channel, country,
                   customer_tier, fulfillment_status, payment_status,
                   referrer_source, return_reason
```

---

## 9. Frontend — Analytics page (9 new panels)

All panels are inside the existing single `AnalyticsPage.tsx`. They appear below the existing 4 panels and are grouped logically.

### Section A — Revenue & financial
**Revenue breakdown strip** _(new)_
- Gross GMV · Net revenue (after discounts) · Total discount · Total tax · Total shipping · AOV
- Source: `/v1/analytics/orders/revenue`

**Enriched summary stat cards** _(expand existing 4 → 8)_
- Add: AOV · Net Revenue · New customers today · Return rate %

**Trend chart toggle** _(expand existing)_
- Add selectable series: Gross revenue / Net revenue / Discount total / New customers
- Same bar chart component, additional toggle buttons

### Section B — Customer analytics
**Customer summary panel** _(new)_
- 4 stat cards: Total customers · New (period) · Returning · Avg LTV
- Source: `/v1/analytics/customers/summary`

**New vs Returning cohort chart** _(new)_
- Stacked bar chart per day/week: new orders (blue) vs returning orders (purple)
- Source: `/v1/analytics/customers/cohorts`

**Top buyers table** _(new)_
- Columns: Rank · Customer ID (masked) · Tier badge · Orders · Total spend · Avg order · Last active
- Source: `/v1/analytics/customers/top`

### Section C — Geography
**Geographic breakdown table** _(new)_
- Toggle: country → state → city drill-down
- Columns: Location · Orders · Revenue · Share %
- Source: `/v1/analytics/geo/breakdown`

### Section D — Channel & acquisition
**Channel attribution table** _(new)_
- Rows: channel + referrer_source combination
- Columns: Channel · Source · Orders · Revenue · New customers · Revenue share %
- Source: `/v1/analytics/channels/breakdown`

### Section E — Products (enriched)
**Top products table** _(expand existing)_
- Add columns: Units sold · Return count · Return rate % · Avg unit price
- Add sort toggle: by Revenue / by Quantity / by Returns
- Source: `/v1/analytics/products/top` (enriched)

### Section F — Operations
**Payment health table** _(new)_
- One row per payment method
- Columns: Method · Total orders · Success · Failed · Refunded · Failure rate % (red if >5%)
- Source: `/v1/analytics/payments/breakdown`

**Fulfilment funnel** _(new)_
- Horizontal progress bars: pending → processing → shipped → delivered → returned
- Shows count and drop-off % at each stage
- Source: `/v1/analytics/fulfillment/funnel`

**Returns & refunds panel** _(new)_
- 3 stat cards: Total returns · Total refund amount · Return rate %
- Return reasons bar chart (top 5)
- Source: `/v1/analytics/returns/summary`

### Section G — Discounts & promos
**Discount effectiveness panel** _(new)_
- Stat cards: Orders with discount · Total discount given · Avg discount/order · Discount rate %
- Top promo codes table: code · uses · total discount
- Source: `/v1/analytics/discounts/summary`

---

## 10. Frontend — Overview page enrichment

Currently: 5 stat cards + tenants mini-table + agents mini-table.

### Add to Overview
- **Revenue summary strip**: Gross GMV (7d) · Net revenue (7d) · AOV (7d) · New customers (7d)
- **Active alerts strip**: auto-populated when any tenant has:
  - Payment failure rate > 10%
  - Return rate > 15%
  - Processing lag > 5 minutes
  - Dead-letter count > 0
- **Top tenant callout**: tenant with highest revenue in last 24h (name, revenue, order count)
- **Tenant health table** (replace plain tenants table):
  - Columns: Name · Plan tier · Last event · Lag · Dead-letter · Status badge

---

## 11. Frontend — Monitoring page enrichment

Currently: worker health strip + 5 stat cards + latency panel + rate grid + dead-letter table.

### Add to Monitoring

**Per-agent WAL lag table** _(new)_
- Source: `processing_watermarks` + `agent_cursors` joined
- Columns: Agent ID · Application · Confirmed LSN · Last seen · Lag (s) · Status badge

**Throughput sparkline** _(new)_
- Mini bar chart of events/minute for the last 60 minutes
- Source: `GET /v1/analytics/monitoring/ingestion-rate` (new endpoint, reads `op_ingestion_rate`)

**Event type breakdown** _(new)_
- Bar chart: `order.created` vs `order.updated` vs `order.cancelled` in last 5 minutes
- Source: `raw_events` count by `event_type` where `received_at >= NOW() - 5m`

**Dead-letter grouped by tenant** _(new)_
- Table: Tenant ID · Unresolved count · Most recent failure reason
- Helps operators triage which tenant is broken

**Schema version distribution** _(new)_
- Table: Schema version · Event count · First seen · Last seen
- Catches version skew during rolling deploys

---

## 12. Bulk seeder — realistic field distributions

[`scripts/seed/bulk_seed.go`](../scripts/seed/bulk_seed.go) must generate all 24 columns with realistic statistical distributions:

| Field | Distribution |
|---|---|
| `customer_id` | Pool of ~10,000 IDs per tenant; 30% are repeat buyers (Zipf-distributed) |
| `customer_tier` | free 40% · silver 30% · gold 20% · platinum 10% |
| `payment_method` | upi 35% · card 30% · wallet 15% · cod 12% · bnpl 5% · netbanking 3% |
| `payment_status` | paid 88% · pending 5% · failed 4% · refunded 2% · chargeback 1% |
| `discount_code` | 25% of orders; codes from pool of 10: SALE20, FLAT100, WELCOME50, etc. |
| `discount_amount` | When code present: uniform 5–30% of `amount`; else 0 |
| `tax_amount` | 18% GST on `(amount - discount_amount)` |
| `shipping_cost` | 0 for cod/wallet orders; else ₹0–₹199 |
| `quantity` | Zipf: 1 unit 70% · 2 units 18% · 3 units 8% · 4 units 4% |
| `sku` | `{product_id}-{colour}-{size}` from small pools per product |
| `channel` | web 45% · mobile_app 35% · pos 10% · marketplace 8% · api 2% |
| `referrer_source` | direct 30% · google 25% · instagram 18% · email 12% · affiliate 10% · whatsapp 5% |
| `country` | IN 70% · US 12% · SG 5% · AE 5% · GB 4% · AU 4% |
| `state_province` | Realistic per country (Maharashtra, Karnataka, Delhi, Tamil Nadu, etc. for IN) |
| `city` | Realistic per state (Mumbai, Bengaluru, Delhi, Chennai, Hyderabad, etc.) |
| `fulfillment_status` | Derived from `status`: placed→pending, confirmed→processing, shipped→shipped, delivered→delivered, cancelled→cancelled, returned→returned |
| `return_reason` | Only when `fulfillment_status = returned`: defective 35% · wrong_item 25% · changed_mind 20% · damaged 15% · other 5% |
| `customer_email` | Synthetic `user{N}@example.com` — never used as a real address |

---

## 13. Summary of scope delta

| Layer | Current | After expansion | Delta |
|---|---|---|---|
| Reference DB `orders` columns | 8 | 24 | +16 |
| Analytics `order_state` columns | 10 | 27 | +17 |
| Precomputed aggregate tables | 2 | 9 | +7 |
| Operational tables | 2 (latency, dead-letter) | 3 (+ ingestion_rate) | +1 |
| Analytics API endpoints | 5 | 16 | +11 |
| `breakdown` group_by options | 2 | 9 | +7 |
| Metric config numeric fields | 1 | 5 | +4 |
| Metric config group_by options | 4 | 10 | +6 |
| Metric config filter options | 4 | 13 | +9 |
| Frontend Analytics panels | 4 | 13 | +9 |
| Frontend Overview panels | 2 | 5 | +3 |
| Frontend Monitoring panels | 4 | 9 | +5 |
| Event envelope known fields | 7 | 27 | +20 |
| Seeder column distributions | flat/random | realistic weighted | — |
| Agent watched columns | 10 | 27 | +17 |

---

## 14. Implementation order

The phases below are ordered to avoid seeding thin data first and then having to re-seed.

1. **Reference DB migration** — add 16 columns to `orders`; update FastAPI schemas
2. **Bulk seeder update** — generate all 24 columns with realistic distributions; re-seed before anything else runs
3. **Agent config** — expand watched columns list to 27
4. **Analytics DB migration** — expand `order_state`; expand hourly/daily summaries; add 7 new aggregate tables + `op_ingestion_rate`
5. **Worker update** — extract all 27 fields; populate all 9 aggregate tables; upsert `op_ingestion_rate`
6. **11 new API endpoints** — implement handlers backed by precomputed aggregates
7. **Expand `breakdown` endpoint** — add 7 new `group_by` values
8. **Metric config allowlist** — add 4 numeric fields, 6 group_by, 9 filter columns
9. **Frontend — Analytics page** — 9 new panels + enriched existing panels
10. **Frontend — Overview page** — alerts strip, tenant health table, revenue strip
11. **Frontend — Monitoring page** — agent lag table, throughput sparkline, event type breakdown

Each step is independently deployable. Steps 1–3 must happen before steps 4–5. Steps 4–5 must happen before steps 6–8. Frontend (9–11) can be built against the API in parallel with steps 6–8.
