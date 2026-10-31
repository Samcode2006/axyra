package db

import (
	"database/sql"
	_ "embed"
	"fmt"
	"log"

	_ "github.com/lib/pq"
)

//go:embed 001_init.sql
var initMigration string

// Connect opens a connection to PostgreSQL and pings it to confirm connectivity.
func Connect(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)

	// Run initial schema migration
	if err := Migrate(db); err != nil {
		return nil, fmt.Errorf("migrate db: %w", err)
	}

	return db, nil
}

// Migrate executes the initialization SQL schema.
func Migrate(db *sql.DB) error {
	if _, err := db.Exec(initMigration); err != nil {
		return fmt.Errorf("exec migration: %w", err)
	}
	log.Println("[control-plane-db] migrations applied successfully")
	return nil
}

// SeedDefaultData seeds the initial demo org, app, agent, and tenants if not already present.
func SeedDefaultData(db *sql.DB, defaultAgentToken string) error {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM organizations").Scan(&count)
	if err != nil {
		return fmt.Errorf("check organizations count: %w", err)
	}
	if count > 0 {
		return nil // already seeded
	}

	log.Println("[control-plane-db] seeding default demo data...")

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Organization
	_, err = tx.Exec(`
		INSERT INTO organizations (id, name, created_at)
		VALUES ('org_demo', 'Demo Organization', NOW())
		ON CONFLICT (id) DO NOTHING
	`)
	if err != nil {
		return fmt.Errorf("seed org: %w", err)
	}

	// 2. Application
	_, err = tx.Exec(`
		INSERT INTO applications (id, organization_id, name, description, created_at)
		VALUES ('app_demo', 'org_demo', 'Reference Store', 'Demo e-commerce application', NOW())
		ON CONFLICT (id) DO NOTHING
	`)
	if err != nil {
		return fmt.Errorf("seed app: %w", err)
	}

	// 3. Agent
	_, err = tx.Exec(`
		INSERT INTO agents (id, application_id, name, status, created_at)
		VALUES ('agent_ref_demo', 'app_demo', 'Reference Store WAL Agent', 'connected', NOW())
		ON CONFLICT (id) DO NOTHING
	`)
	if err != nil {
		return fmt.Errorf("seed agent: %w", err)
	}

	// 4. Agent Token
	_, err = tx.Exec(`
		INSERT INTO agent_tokens (id, agent_id, token, name, created_at)
		VALUES ('token_demo_01', 'agent_ref_demo', $1, 'Default Demo Token', NOW())
		ON CONFLICT (id) DO NOTHING
	`, defaultAgentToken)
	if err != nil {
		return fmt.Errorf("seed agent token: %w", err)
	}

	// 5. Tenants
	_, err = tx.Exec(`
		INSERT INTO tenants (id, organization_id, external_tenant_id, name, status, max_events_per_day, max_concurrent_queries, created_at)
		VALUES 
			('tenant_001', 'org_demo', 'store_001', 'Store 1 (North)', 'active', 1000000, 10, NOW()),
			('tenant_002', 'org_demo', 'store_002', 'Store 2 (South)', 'active', 1000000, 10, NOW())
		ON CONFLICT (id) DO NOTHING
	`)
	if err != nil {
		return fmt.Errorf("seed tenants: %w", err)
	}

	// 6. Tenant Mappings
	_, err = tx.Exec(`
		INSERT INTO tenant_mappings (id, agent_id, external_tenant_id, tenant_id, created_at)
		VALUES 
			('map_001', 'agent_ref_demo', 'store_001', 'tenant_001', NOW()),
			('map_002', 'agent_ref_demo', 'store_002', 'tenant_002', NOW())
		ON CONFLICT (id) DO NOTHING
	`)
	if err != nil {
		return fmt.Errorf("seed tenant mappings: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit seed transaction: %w", err)
	}

	log.Println("[control-plane-db] default demo data seeded successfully")
	return nil
}
