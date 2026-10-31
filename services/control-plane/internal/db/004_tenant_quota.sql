-- Backfill quota columns for databases created before tenant quotas were added.
-- IF NOT EXISTS keeps this safe for fresh and already-upgraded databases.

ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS max_events_per_day BIGINT NOT NULL DEFAULT 1000000,
    ADD COLUMN IF NOT EXISTS max_concurrent_queries INT NOT NULL DEFAULT 10;