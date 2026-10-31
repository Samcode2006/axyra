-- Analytics Data Plane Database Schema

CREATE TABLE IF NOT EXISTS raw_events (
    id BIGSERIAL PRIMARY KEY,
    event_id VARCHAR(128) UNIQUE NOT NULL,
    agent_id VARCHAR(64) NOT NULL,
    tenant_id VARCHAR(64) NOT NULL,
    external_tenant_id VARCHAR(64) NOT NULL,
    event_type VARCHAR(64) NOT NULL,
    schema_version INT NOT NULL DEFAULT 1,
    lsn VARCHAR(64) NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    data JSONB NOT NULL
);

CREATE TABLE IF NOT EXISTS processed_events (
    event_id VARCHAR(128) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    status VARCHAR(32) NOT NULL,
    error_message TEXT
);

CREATE TABLE IF NOT EXISTS order_state (
    order_id VARCHAR(64) NOT NULL,
    tenant_id VARCHAR(64) NOT NULL,
    amount NUMERIC(12, 2) NOT NULL,
    currency VARCHAR(3) NOT NULL DEFAULT 'INR',
    status VARCHAR(32) NOT NULL,
    product_id VARCHAR(64) NOT NULL,
    product_name VARCHAR(255) NOT NULL,
    category VARCHAR(64) NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, order_id)
);

CREATE TABLE IF NOT EXISTS order_summary_hourly (
    tenant_id VARCHAR(64) NOT NULL,
    bucket TIMESTAMPTZ NOT NULL,
    total_orders BIGINT NOT NULL DEFAULT 0,
    active_orders BIGINT NOT NULL DEFAULT 0,
    cancelled_orders BIGINT NOT NULL DEFAULT 0,
    total_revenue NUMERIC(14, 2) NOT NULL DEFAULT 0,
    currency VARCHAR(3) NOT NULL DEFAULT 'INR',
    PRIMARY KEY (tenant_id, bucket)
);

CREATE TABLE IF NOT EXISTS order_summary_daily (
    tenant_id VARCHAR(64) NOT NULL,
    bucket DATE NOT NULL,
    total_orders BIGINT NOT NULL DEFAULT 0,
    active_orders BIGINT NOT NULL DEFAULT 0,
    cancelled_orders BIGINT NOT NULL DEFAULT 0,
    total_revenue NUMERIC(14, 2) NOT NULL DEFAULT 0,
    currency VARCHAR(3) NOT NULL DEFAULT 'INR',
    PRIMARY KEY (tenant_id, bucket)
);

CREATE TABLE IF NOT EXISTS product_aggregates (
    tenant_id VARCHAR(64) NOT NULL,
    product_id VARCHAR(64) NOT NULL,
    product_name VARCHAR(255) NOT NULL,
    category VARCHAR(64) NOT NULL,
    order_count BIGINT NOT NULL DEFAULT 0,
    total_revenue NUMERIC(14, 2) NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, product_id)
);

CREATE TABLE IF NOT EXISTS processing_watermarks (
    agent_id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    last_lsn VARCHAR(64) NOT NULL,
    last_processed_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Persistent LSN cursor used by the agent for restart safety.
-- The agent polls GET /v1/agent/ack and resumes replication from this LSN.
CREATE TABLE IF NOT EXISTS agent_cursors (
    agent_id VARCHAR(64) PRIMARY KEY,
    source VARCHAR(128) NOT NULL DEFAULT 'default',
    confirmed_lsn VARCHAR(64) NOT NULL DEFAULT '0/0',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Dead-letter queue: events that fail validation or exhaust processing retries.
-- Operators can inspect and replay entries from the web console.
CREATE TABLE IF NOT EXISTS dead_letter_events (
    id BIGSERIAL PRIMARY KEY,
    agent_id VARCHAR(64) NOT NULL,
    tenant_id VARCHAR(64) NOT NULL DEFAULT '',
    event_id VARCHAR(128) NOT NULL DEFAULT '',
    lsn VARCHAR(64) NOT NULL DEFAULT '',
    reason TEXT NOT NULL,
    payload JSONB,
    retry_count INT NOT NULL DEFAULT 0,
    replayed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Operational measurements for the monitoring page (B-5).
-- Each row is one query observation; the monitoring endpoint aggregates over
-- the last N minutes. Rows older than 24h can be pruned.
CREATE TABLE IF NOT EXISTS op_query_latency (
    id BIGSERIAL PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    endpoint VARCHAR(128) NOT NULL,
    duration_ms INT NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_raw_events_tenant_time ON raw_events(tenant_id, occurred_at);
CREATE INDEX IF NOT EXISTS idx_raw_events_agent_lsn ON raw_events(agent_id, lsn);
CREATE INDEX IF NOT EXISTS idx_order_state_tenant_status ON order_state(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_order_summary_hourly_time ON order_summary_hourly(tenant_id, bucket);
CREATE INDEX IF NOT EXISTS idx_order_summary_daily_time ON order_summary_daily(tenant_id, bucket);
CREATE INDEX IF NOT EXISTS idx_product_aggregates_top ON product_aggregates(tenant_id, total_revenue DESC);
CREATE INDEX IF NOT EXISTS idx_dead_letter_agent ON dead_letter_events(agent_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_op_query_latency_time ON op_query_latency(recorded_at DESC);
