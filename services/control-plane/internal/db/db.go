package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/axyra/control-plane/internal/auth"
	"github.com/axyra/control-plane/internal/metrics"
	_ "github.com/lib/pq"
)

//go:embed *.sql
var migrationFiles embed.FS

// migrationLockKey is an arbitrary constant used with pg_advisory_xact_lock so
// two control-plane replicas starting together do not migrate concurrently.
const migrationLockKey = 7243001

// Connect opens a connection to PostgreSQL with retries and pings it to confirm connectivity.
func Connect(dsn string) (*sql.DB, error) {
	// Ensure sslmode=disable if not specified
	if !strings.Contains(dsn, "sslmode=") {
		if strings.Contains(dsn, "?") {
			dsn += "&sslmode=disable"
		} else {
			dsn += "?sslmode=disable"
		}
	}

	var db *sql.DB
	var err error

	for attempt := 1; attempt <= 20; attempt++ {
		db, err = sql.Open("postgres", dsn)
		if err == nil {
			err = db.Ping()
			if err == nil {
				log.Printf("[control-plane-db] connected to postgres successfully on attempt %d", attempt)
				break
			}
			_ = db.Close()
		}
		log.Printf("[control-plane-db] waiting for postgres (%d/20): %v", attempt, err)
		time.Sleep(1 * time.Second)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect to db after 20 attempts: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)

	if err := Migrate(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate db: %w", err)
	}

	return db, nil
}

// Migrate applies every embedded NNN_*.sql file that has not been applied yet,
// in version order. Each file runs in its own transaction and is recorded in
// schema_migrations, so a file that is not safe to re-run (for example one that
// drops a column) is never executed twice.
func Migrate(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	names, err := fs.Glob(migrationFiles, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)

	for _, name := range names {
		if err := applyMigration(db, name); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func applyMigration(db *sql.DB, name string) error {
	body, err := migrationFiles.ReadFile(name)
	if err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("SELECT pg_advisory_xact_lock($1)", migrationLockKey); err != nil {
		return err
	}

	var applied bool
	if err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)", name).Scan(&applied); err != nil {
		return err
	}
	if applied {
		return nil
	}

	if _, err := tx.Exec(string(body)); err != nil {
		return err
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES ($1)", name); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	log.Printf("[control-plane-db] applied migration %s", name)
	return nil
}

// SeedOptions carries the credentials used when seeding a fresh platform.
type SeedOptions struct {
	AgentToken       string
	AdminUsername    string
	AdminPassword    string
	OperatorUsername string
	OperatorPassword string
}

// SeedDefaultData seeds the demo org, app, agent, tenants, console users and
// built-in metric definitions. Every step is skipped when its data already
// exists, so it is safe to call on each start and on upgraded Phase 1 databases.
func SeedDefaultData(db *sql.DB, opts SeedOptions) error {
	if err := seedDemoTenancy(db, opts.AgentToken); err != nil {
		return err
	}
	if err := seedUsers(db, opts); err != nil {
		return err
	}
	return seedBuiltinMetrics(db)
}

func seedDemoTenancy(db *sql.DB, agentToken string) error {
	log.Println("[control-plane-db] seeding default demo data...")

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmts := []struct {
		name string
		sql  string
		args []any
	}{
		{"org", `INSERT INTO organizations (id, name, created_at)
			VALUES ('org_demo', 'Demo Organization', NOW()) ON CONFLICT (id) DO NOTHING`, nil},
		{"app", `INSERT INTO applications (id, organization_id, name, description, created_at)
			VALUES ('app_demo', 'org_demo', 'Reference Store', 'Demo e-commerce application', NOW())
			ON CONFLICT (id) DO NOTHING`, nil},
		{"agent", `INSERT INTO agents (id, application_id, name, status, created_at)
			VALUES ('agent_ref_demo', 'app_demo', 'Reference Store WAL Agent', 'disconnected', NOW())
			ON CONFLICT (id) DO NOTHING`, nil},
		{"agent token", `INSERT INTO agent_tokens (id, agent_id, token_hash, token_prefix, name, created_at)
			VALUES ('token_demo_01', 'agent_ref_demo', $1, $2, 'Default Demo Token', NOW())
			ON CONFLICT (id) DO NOTHING`,
			[]any{auth.HashAgentToken(agentToken), auth.TokenPrefix(agentToken)}},
		{"tenants", `INSERT INTO tenants (id, organization_id, external_tenant_id, name, status, max_events_per_day, max_concurrent_queries, created_at)
			VALUES
				('tenant_001', 'org_demo', 'store_001', 'Store 1 (North)', 'active', 1000000, 10, NOW()),
				('tenant_002', 'org_demo', 'store_002', 'Store 2 (South)', 'active', 1000000, 10, NOW()),
				('tenant_003', 'org_demo', 'store_003', 'Store 3 (East)', 'active', 1000000, 10, NOW()),
				('tenant_004', 'org_demo', 'store_004', 'Store 4 (West)', 'active', 1000000, 10, NOW()),
				('tenant_005', 'org_demo', 'store_005', 'Store 5 (Central)', 'active', 1000000, 10, NOW())
			ON CONFLICT (id) DO NOTHING`, nil},
		{"tenant mappings", `INSERT INTO tenant_mappings (id, agent_id, external_tenant_id, tenant_id, created_at)
			VALUES
				('map_001', 'agent_ref_demo', 'store_001', 'tenant_001', NOW()),
				('map_002', 'agent_ref_demo', 'store_002', 'tenant_002', NOW()),
				('map_003', 'agent_ref_demo', 'store_003', 'tenant_003', NOW()),
				('map_004', 'agent_ref_demo', 'store_004', 'tenant_004', NOW()),
				('map_005', 'agent_ref_demo', 'store_005', 'tenant_005', NOW())
			ON CONFLICT (id) DO NOTHING`, nil},
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s.sql, s.args...); err != nil {
			return fmt.Errorf("seed %s: %w", s.name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit seed transaction: %w", err)
	}
	log.Println("[control-plane-db] default demo data seeded successfully")
	return nil
}

// seedUsers creates the demo console admin (bound to org_demo) and a platform
// operator, but only when no users exist yet. A missing password is generated
// and logged once, so a fresh install is never left with a guessable login.
func seedUsers(db *sql.DB, opts SeedOptions) error {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return fmt.Errorf("check users count: %w", err)
	}
	if count > 0 {
		return nil
	}

	var hasDemoOrg bool
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM organizations WHERE id = 'org_demo')").Scan(&hasDemoOrg); err != nil {
		return err
	}

	type seedUser struct {
		id, username, password string
		org                    any
		role                   auth.Role
	}
	users := []seedUser{
		{id: "user_operator", username: opts.OperatorUsername, password: opts.OperatorPassword, role: auth.RoleOperator},
	}
	if hasDemoOrg {
		users = append(users, seedUser{id: "user_admin", username: opts.AdminUsername, password: opts.AdminPassword, org: "org_demo", role: auth.RoleAdmin})
	}

	for _, u := range users {
		password := u.password
		if password == "" {
			generated, err := auth.RandomPassword()
			if err != nil {
				return err
			}
			password = generated
			log.Printf("[control-plane-db] generated password for %s user %q: %s (shown once; set AXYRA_%s_PASSWORD to choose your own)",
				u.role, u.username, password, strings.ToUpper(string(u.role)))
		}
		hash, err := auth.HashPassword(password)
		if err != nil {
			return err
		}
		if _, err := db.Exec(`
			INSERT INTO users (id, organization_id, username, password_hash, role)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT DO NOTHING`, u.id, u.org, u.username, hash, string(u.role)); err != nil {
			return fmt.Errorf("seed user %s: %w", u.username, err)
		}
	}
	return nil
}

// seedBuiltinMetrics installs the platform's built-in metric definitions
// (organization_id NULL). Existing rows are left untouched so operator edits survive restarts.
func seedBuiltinMetrics(db *sql.DB) error {
	for _, m := range metrics.Builtins() {
		cfg, err := m.Config.Marshal()
		if err != nil {
			return err
		}
		if _, err := db.Exec(`
			INSERT INTO metric_definitions (id, organization_id, name, description, config, enabled)
			VALUES ($1, NULL, $2, $3, $4, TRUE)
			ON CONFLICT (scope, id) DO NOTHING`, m.ID, m.Name, m.Description, string(cfg)); err != nil {
			return fmt.Errorf("seed metric %s: %w", m.ID, err)
		}
	}
	return nil
}
