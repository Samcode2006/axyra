-- Tenant profile columns (docs/schema-expansion-plan.md, section 5).
-- Idempotent. Constant defaults make this a metadata-only change.

ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS region        VARCHAR(32) NOT NULL DEFAULT 'global',   -- apac | emea | americas | global
    ADD COLUMN IF NOT EXISTS plan_tier     VARCHAR(32) NOT NULL DEFAULT 'growth',   -- free | growth | enterprise
    ADD COLUMN IF NOT EXISTS billing_cycle VARCHAR(16) NOT NULL DEFAULT 'monthly',  -- monthly | annual
    ADD COLUMN IF NOT EXISTS tags          TEXT[]      NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS metadata      JSONB       NOT NULL DEFAULT '{}';

-- The API validates these too; the constraints keep bad values out regardless
-- of the path they arrive by.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'tenants_region_check') THEN
        ALTER TABLE tenants ADD CONSTRAINT tenants_region_check
            CHECK (region IN ('apac', 'emea', 'americas', 'global'));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'tenants_plan_tier_check') THEN
        ALTER TABLE tenants ADD CONSTRAINT tenants_plan_tier_check
            CHECK (plan_tier IN ('free', 'growth', 'enterprise'));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'tenants_billing_cycle_check') THEN
        ALTER TABLE tenants ADD CONSTRAINT tenants_billing_cycle_check
            CHECK (billing_cycle IN ('monthly', 'annual'));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'tenants_metadata_object_check') THEN
        ALTER TABLE tenants ADD CONSTRAINT tenants_metadata_object_check
            CHECK (jsonb_typeof(metadata) = 'object');
    END IF;
END $$;
