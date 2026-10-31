-- Keyset replay follows ledger order within one tenant.
CREATE INDEX IF NOT EXISTS idx_raw_events_tenant_id ON raw_events(tenant_id, id);
