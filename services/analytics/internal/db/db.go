package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log"
	"sort"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

//go:embed *.sql
var migrationFiles embed.FS

// migrationLockKey serializes concurrent analytics replicas starting together.
// It differs from the control plane's key because both services share one database.
const migrationLockKey = 7243002

type DB struct {
	*sql.DB
}

func Connect(dsn string) (*DB, error) {
	// Ensure sslmode=disable if not specified
	if !strings.Contains(dsn, "sslmode=") {
		if strings.Contains(dsn, "?") {
			dsn += "&sslmode=disable"
		} else {
			dsn += "?sslmode=disable"
		}
	}

	var sdb *sql.DB
	var err error

	for attempt := 1; attempt <= 20; attempt++ {
		sdb, err = sql.Open("postgres", dsn)
		if err == nil {
			err = sdb.Ping()
			if err == nil {
				log.Printf("[analytics-db] connected to postgres successfully on attempt %d", attempt)
				break
			}
			_ = sdb.Close()
		}
		log.Printf("[analytics-db] waiting for postgres (%d/20): %v", attempt, err)
		time.Sleep(1 * time.Second)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect to analytics db after 20 attempts: %w", err)
	}

	sdb.SetMaxOpenConns(25)
	sdb.SetMaxIdleConns(10)

	db := &DB{DB: sdb}
	if err := db.Migrate(); err != nil {
		return nil, fmt.Errorf("migrate analytics db: %w", err)
	}

	return db, nil
}

// Migrate applies every embedded NNN_*.sql file that has not been applied yet,
// in version order. Each file runs in its own transaction and is recorded in
// analytics_schema_migrations, so a file is never executed twice.
//
// The tracking table is named differently from the control plane's
// schema_migrations because both services share one database and both ship a
// file called 001_init.sql.
func (db *DB) Migrate() error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS analytics_schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("create analytics_schema_migrations: %w", err)
	}

	names, err := fs.Glob(migrationFiles, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)

	for _, name := range names {
		if err := db.applyMigration(name); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	log.Println("[analytics-db] migrations applied successfully")
	return nil
}

func (db *DB) applyMigration(name string) error {
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
	if err := tx.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM analytics_schema_migrations WHERE version = $1)", name).Scan(&applied); err != nil {
		return err
	}
	if applied {
		return nil
	}

	if _, err := tx.Exec(string(body)); err != nil {
		return err
	}
	if _, err := tx.Exec("INSERT INTO analytics_schema_migrations (version) VALUES ($1)", name); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	log.Printf("[analytics-db] applied migration %s", name)
	return nil
}

// ValidateAgentToken verifies that the agent token exists, is not revoked and
// has not expired. Tokens are stored as SHA-256 hashes by the control plane
// (see auth.HashAgentToken there), so the lookup hashes the presented value.
// Returns agent_id.
func (db *DB) ValidateAgentToken(token string) (string, error) {
	sum := sha256.Sum256([]byte(token))
	var agentID string
	err := db.QueryRow(`
		SELECT agent_id
		FROM agent_tokens
		WHERE token_hash = $1
		  AND revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > NOW())
	`, hex.EncodeToString(sum[:])).Scan(&agentID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("invalid or revoked agent token")
		}
		return "", err
	}
	return agentID, nil
}

// ResolveTenant maps an external tenant ID (e.g. store_001) to canonical Axyra tenant ID.
func (db *DB) ResolveTenant(agentID, externalTenantID string) (string, error) {
	var tenantID string
	err := db.QueryRow(`
		SELECT tenant_id 
		FROM tenant_mappings 
		WHERE agent_id = $1 AND external_tenant_id = $2
	`, agentID, externalTenantID).Scan(&tenantID)
	if err != nil {
		if err == sql.ErrNoRows {
			// Fallback: check if externalTenantID itself matches a tenant id
			err2 := db.QueryRow("SELECT id FROM tenants WHERE id = $1 OR external_tenant_id = $1", externalTenantID).Scan(&tenantID)
			if err2 == nil {
				return tenantID, nil
			}
			return "", fmt.Errorf("tenant mapping not found for agent %s and external tenant %s", agentID, externalTenantID)
		}
		return "", err
	}
	return tenantID, nil
}

// GetDefaultTenant returns the first active tenant or "tenant_001".
func (db *DB) GetDefaultTenant() string {
	var tenantID string
	err := db.QueryRow("SELECT id FROM tenants WHERE status = 'active' ORDER BY created_at ASC LIMIT 1").Scan(&tenantID)
	if err == nil && tenantID != "" {
		return tenantID
	}
	return "tenant_001"
}

// InsertDeadLetter stores an event that failed validation or exhausted retries.
// tenantID and eventID may be empty when the event was rejected before parsing.
func (db *DB) InsertDeadLetter(ctx context.Context, agentID, tenantID, eventID, lsn, reason string, payload []byte) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO dead_letter_events (agent_id, tenant_id, event_id, lsn, reason, payload)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, agentID, tenantID, eventID, lsn, reason, string(payload))
	return err
}

// GetAgentCursor returns the last confirmed LSN for an agent source, or "0/0" if none.
func (db *DB) GetAgentCursor(agentID, source string) (string, error) {
	if source == "" {
		source = "default"
	}
	var lsn string
	err := db.QueryRow(`SELECT confirmed_lsn FROM agent_cursors WHERE agent_id = $1 AND source = $2`, agentID, source).Scan(&lsn)
	if err != nil {
		if err == sql.ErrNoRows {
			return "0/0", nil
		}
		return "0/0", err
	}
	return lsn, nil
}

// UpsertAgentCursor persists a source-specific LSN so the agent can resume that WAL stream.
func (db *DB) UpsertAgentCursor(ctx context.Context, agentID, source, confirmedLSN string) error {
	if source == "" {
		source = "default"
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO agent_cursors (agent_id, source, confirmed_lsn, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (agent_id, source) DO UPDATE SET
			confirmed_lsn = EXCLUDED.confirmed_lsn,
			updated_at = NOW()
	`, agentID, source, confirmedLSN)
	return err
}

// RecordQueryLatency persists one query observation for the monitoring page.
// It is best-effort: failures are logged but never returned to the caller.
func (db *DB) RecordQueryLatency(ctx context.Context, tenantID, endpoint string, durationMs int64) {
	_, _ = db.ExecContext(ctx,
		`INSERT INTO op_query_latency (tenant_id, endpoint, duration_ms) VALUES ($1, $2, $3)`,
		tenantID, endpoint, durationMs)
}

// GetTenantMaxConcurrency returns the max_concurrent_queries quota for a tenant,
// or 0 if no quota is set (meaning unlimited).
func (db *DB) GetTenantMaxConcurrency(tenantID string) int {
	var n int
	err := db.QueryRow(`
		SELECT COALESCE(max_concurrent_queries, 0)
		FROM tenants WHERE id = $1`, tenantID).Scan(&n)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
