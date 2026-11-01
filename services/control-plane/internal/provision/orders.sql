-- Reference order schema for newly managed sources.

CREATE TABLE IF NOT EXISTS orders (
    id          VARCHAR PRIMARY KEY,
    store_id    VARCHAR NOT NULL,
    amount      NUMERIC(12,2) NOT NULL,
    currency    VARCHAR(3) DEFAULT 'INR',
    status      VARCHAR(32) DEFAULT 'placed',
    product_id  VARCHAR NOT NULL,
    product_name VARCHAR NOT NULL,
    category    VARCHAR NOT NULL,
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    updated_at  TIMESTAMPTZ DEFAULT NOW()
);
ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS customer_id        VARCHAR(64)   NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS customer_email     VARCHAR(255)  NOT NULL DEFAULT '',
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
    ADD COLUMN IF NOT EXISTS referrer_source    VARCHAR(64)   NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_orders_store_id  ON orders(store_id);
CREATE INDEX IF NOT EXISTS idx_orders_created_at ON orders(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_orders_status    ON orders(status);
