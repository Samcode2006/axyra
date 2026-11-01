-- Latest completed event lookups for live monitoring.
CREATE INDEX IF NOT EXISTS idx_raw_events_agent_id ON raw_events(agent_id, id);
CREATE INDEX IF NOT EXISTS idx_processed_events_tenant_completed
    ON processed_events(tenant_id, processed_at) WHERE status = 'processed';
