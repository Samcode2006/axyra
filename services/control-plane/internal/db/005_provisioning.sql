CREATE TABLE IF NOT EXISTS tenant_provisioning (
 tenant_id varchar(64) PRIMARY KEY REFERENCES tenants(id) ON DELETE RESTRICT,
 status text NOT NULL DEFAULT 'queued' CHECK(status IN ('queued','provisioning','ready','failed')),
 stage text NOT NULL DEFAULT 'queued',
 error_message text NOT NULL DEFAULT '',
 encrypted_secrets bytea,
 agent_id varchar(64) REFERENCES agents(id) ON DELETE RESTRICT,
 created_at timestamptz NOT NULL DEFAULT NOW(),
 updated_at timestamptz NOT NULL DEFAULT NOW()
);
