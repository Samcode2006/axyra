-- Phase 2: complete control plane.
-- Every statement is idempotent so the migration is safe to re-run.

-- ─── Agent tokens: store only a hash of the secret ───────────────────────────
-- The plaintext column is backfilled into token_hash and then dropped, so a
-- database read no longer reveals usable agent credentials.
ALTER TABLE agent_tokens ADD COLUMN IF NOT EXISTS token_hash CHAR(64);
ALTER TABLE agent_tokens ADD COLUMN IF NOT EXISTS token_prefix VARCHAR(32);
ALTER TABLE agent_tokens ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'agent_tokens' AND column_name = 'token'
    ) THEN
        UPDATE agent_tokens
        SET token_hash   = encode(sha256(convert_to(token, 'UTF8')), 'hex'),
            token_prefix = left(token, 18)
        WHERE token_hash IS NULL AND token IS NOT NULL;

        ALTER TABLE agent_tokens DROP COLUMN token;
    END IF;
END $$;

ALTER TABLE agent_tokens ALTER COLUMN token_hash SET NOT NULL;
ALTER TABLE agent_tokens ALTER COLUMN token_prefix SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_tokens_hash ON agent_tokens(token_hash);
CREATE INDEX IF NOT EXISTS idx_agent_tokens_agent ON agent_tokens(agent_id);

-- ─── Console users (authentication + org scoping) ────────────────────────────
-- 'operator' users are platform-wide and have no organization.
-- 'admin' users are customer administrators bound to exactly one organization.
CREATE TABLE IF NOT EXISTS users (
    id VARCHAR(64) PRIMARY KEY,
    organization_id VARCHAR(64) REFERENCES organizations(id) ON DELETE CASCADE,
    username VARCHAR(128) NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role VARCHAR(32) NOT NULL CHECK (role IN ('admin', 'operator')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (role = 'operator' OR organization_id IS NOT NULL)
);

-- ─── Audit log ───────────────────────────────────────────────────────────────
-- No foreign keys on purpose: history must outlive the resources it describes.
CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGSERIAL PRIMARY KEY,
    organization_id VARCHAR(64),
    actor_id VARCHAR(64) NOT NULL,
    actor_name VARCHAR(128) NOT NULL,
    actor_role VARCHAR(32) NOT NULL,
    action VARCHAR(64) NOT NULL,
    resource_type VARCHAR(64) NOT NULL,
    resource_id VARCHAR(64) NOT NULL,
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_org ON audit_logs(organization_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_resource ON audit_logs(resource_type, resource_id);

-- ─── Metric definitions ──────────────────────────────────────────────────────
-- organization_id NULL means a built-in definition managed by platform operators.
ALTER TABLE metric_definitions ADD COLUMN IF NOT EXISTS organization_id VARCHAR(64) REFERENCES organizations(id) ON DELETE CASCADE;
ALTER TABLE metric_definitions ADD COLUMN IF NOT EXISTS enabled BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE metric_definitions ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- A metric id only has to be unique inside its owner (an organization, or the
-- built-in scope ''), so two organizations can each define "revenue_by_category"
-- without learning about each other.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'metric_definitions' AND column_name = 'scope'
    ) THEN
        ALTER TABLE metric_definitions DROP CONSTRAINT IF EXISTS metric_definitions_pkey;
        ALTER TABLE metric_definitions
            ADD COLUMN scope VARCHAR(64) GENERATED ALWAYS AS (COALESCE(organization_id, '')) STORED;
        ALTER TABLE metric_definitions ADD PRIMARY KEY (scope, id);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_metric_definitions_org ON metric_definitions(organization_id);

-- ─── Lookup indexes for org-scoped listing ───────────────────────────────────
CREATE INDEX IF NOT EXISTS idx_applications_org ON applications(organization_id);
CREATE INDEX IF NOT EXISTS idx_agents_application ON agents(application_id);
CREATE INDEX IF NOT EXISTS idx_policies_tenant ON policies(tenant_id);
