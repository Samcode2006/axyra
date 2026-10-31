-- Schema expansion (docs/schema-expansion-plan.md, section 4).
-- Every statement is idempotent. The analytics migration runner also records
-- each file once it has been applied.

-- ─── 4.1 order_state: 17 extended columns + 2 internal flags ─────────────────
-- Constant defaults are a metadata-only change in PostgreSQL 11+, so this does
-- not rewrite existing rows.
ALTER TABLE order_state
    ADD COLUMN IF NOT EXISTS customer_id        VARCHAR(64)   NOT NULL DEFAULT '',
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
    ADD COLUMN IF NOT EXISTS referrer_source    VARCHAR(64)   NOT NULL DEFAULT '',
    -- has_extended is false for orders that only ever arrived in schema v1. Their
    -- extended columns above hold column defaults, not real data, so every query
    -- that reads an extended column must filter on this flag.
    ADD COLUMN IF NOT EXISTS has_extended       BOOLEAN       NOT NULL DEFAULT FALSE,
    -- Set once, when the order is first seen: the customer had no earlier order.
    ADD COLUMN IF NOT EXISTS is_new_customer    BOOLEAN       NOT NULL DEFAULT FALSE;

-- ─── 4.2 hourly / daily summaries ────────────────────────────────────────────
ALTER TABLE order_summary_hourly
    ADD COLUMN IF NOT EXISTS returned_orders     BIGINT        NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_discount      NUMERIC(14,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_tax           NUMERIC(14,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_shipping      NUMERIC(14,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_quantity      BIGINT        NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS new_customer_orders BIGINT        NOT NULL DEFAULT 0;

ALTER TABLE order_summary_daily
    ADD COLUMN IF NOT EXISTS returned_orders     BIGINT        NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_discount      NUMERIC(14,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_tax           NUMERIC(14,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_shipping      NUMERIC(14,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_quantity      BIGINT        NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS new_customer_orders BIGINT        NOT NULL DEFAULT 0;

-- ─── 4.3 product_aggregates ──────────────────────────────────────────────────
ALTER TABLE product_aggregates
    ADD COLUMN IF NOT EXISTS quantity_sold  BIGINT        NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS return_count   BIGINT        NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_discount NUMERIC(14,2) NOT NULL DEFAULT 0;

-- ─── 4.4 customer_aggregates ─────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS customer_aggregates (
    tenant_id      VARCHAR(64)   NOT NULL,
    customer_id    VARCHAR(64)   NOT NULL,
    customer_tier  VARCHAR(32)   NOT NULL DEFAULT 'regular',
    order_count    BIGINT        NOT NULL DEFAULT 0,
    total_spend    NUMERIC(14,2) NOT NULL DEFAULT 0,
    total_discount NUMERIC(14,2) NOT NULL DEFAULT 0,
    total_quantity BIGINT        NOT NULL DEFAULT 0,
    first_order_at TIMESTAMPTZ,
    last_order_at  TIMESTAMPTZ,
    PRIMARY KEY (tenant_id, customer_id)
);
CREATE INDEX IF NOT EXISTS idx_customer_agg_spend ON customer_aggregates(tenant_id, total_spend DESC);
CREATE INDEX IF NOT EXISTS idx_customer_agg_last  ON customer_aggregates(tenant_id, last_order_at DESC);
CREATE INDEX IF NOT EXISTS idx_customer_agg_first ON customer_aggregates(tenant_id, first_order_at);

-- ─── 4.5 geo_aggregates ──────────────────────────────────────────────────────
-- The new aggregate tables below are keyed by the day the order was CREATED, so
-- a row always describes the current state of orders created that day.
CREATE TABLE IF NOT EXISTS geo_aggregates (
    tenant_id      VARCHAR(64)   NOT NULL,
    bucket         DATE          NOT NULL,
    country        CHAR(2)       NOT NULL,
    state_province VARCHAR(64)   NOT NULL DEFAULT '',
    city           VARCHAR(64)   NOT NULL DEFAULT '',
    order_count    BIGINT        NOT NULL DEFAULT 0,
    total_revenue  NUMERIC(14,2) NOT NULL DEFAULT 0,
    total_quantity BIGINT        NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, bucket, country, state_province, city)
);
CREATE INDEX IF NOT EXISTS idx_geo_agg_country ON geo_aggregates(tenant_id, bucket, country);

-- ─── 4.6 channel_aggregates ──────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS channel_aggregates (
    tenant_id       VARCHAR(64)   NOT NULL,
    bucket          DATE          NOT NULL,
    channel         VARCHAR(32)   NOT NULL,
    referrer_source VARCHAR(64)   NOT NULL DEFAULT '',
    order_count     BIGINT        NOT NULL DEFAULT 0,
    total_revenue   NUMERIC(14,2) NOT NULL DEFAULT 0,
    new_customers   BIGINT        NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, bucket, channel, referrer_source)
);

-- ─── 4.7 payment_aggregates ──────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS payment_aggregates (
    tenant_id      VARCHAR(64)   NOT NULL,
    bucket         DATE          NOT NULL,
    payment_method VARCHAR(32)   NOT NULL,
    payment_status VARCHAR(32)   NOT NULL,
    order_count    BIGINT        NOT NULL DEFAULT 0,
    total_amount   NUMERIC(14,2) NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, bucket, payment_method, payment_status)
);

-- ─── 4.8 return_aggregates ───────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS return_aggregates (
    tenant_id     VARCHAR(64)   NOT NULL,
    bucket        DATE          NOT NULL,
    return_reason VARCHAR(128)  NOT NULL DEFAULT '',
    return_count  BIGINT        NOT NULL DEFAULT 0,
    refund_amount NUMERIC(14,2) NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, bucket, return_reason)
);

-- ─── 4.9 fulfillment_aggregates ──────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS fulfillment_aggregates (
    tenant_id          VARCHAR(64) NOT NULL,
    bucket             DATE        NOT NULL,
    fulfillment_status VARCHAR(32) NOT NULL,
    order_count        BIGINT      NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, bucket, fulfillment_status)
);

-- ─── 4.10 cohort_daily ───────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS cohort_daily (
    tenant_id     VARCHAR(64)   NOT NULL,
    bucket        DATE          NOT NULL,
    customer_type VARCHAR(16)   NOT NULL,   -- 'new' | 'returning'
    order_count   BIGINT        NOT NULL DEFAULT 0,
    total_revenue NUMERIC(14,2) NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, bucket, customer_type)
);

-- ─── Not in the plan: discount_aggregates ────────────────────────────────────
-- The discounts endpoint needs per-code usage. Without a table it would scan
-- every order of the tenant for each request, which breaks the rule that
-- dashboard queries read precomputed aggregates.
CREATE TABLE IF NOT EXISTS discount_aggregates (
    tenant_id      VARCHAR(64)   NOT NULL,
    bucket         DATE          NOT NULL,
    discount_code  VARCHAR(64)   NOT NULL,
    uses           BIGINT        NOT NULL DEFAULT 0,
    total_discount NUMERIC(14,2) NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, bucket, discount_code)
);

-- ─── 4.11 op_ingestion_rate ──────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS op_ingestion_rate (
    bucket      TIMESTAMPTZ NOT NULL,       -- truncated to the minute
    event_count BIGINT      NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket)
);
CREATE INDEX IF NOT EXISTS idx_op_ingestion_rate_time ON op_ingestion_rate(bucket DESC);

-- ─── Indexes for the new time-range reads ────────────────────────────────────
CREATE INDEX IF NOT EXISTS idx_order_state_tenant_time ON order_state(tenant_id, occurred_at);
CREATE INDEX IF NOT EXISTS idx_raw_events_received_at  ON raw_events(received_at);
CREATE INDEX IF NOT EXISTS idx_dead_letter_tenant      ON dead_letter_events(tenant_id, created_at DESC);
