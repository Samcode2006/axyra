package db

import (
	"database/sql"
	_ "embed"
	"fmt"
	"log"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

//go:embed 001_init.sql
var initMigration string

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

func (db *DB) Migrate() error {
	if _, err := db.Exec(initMigration); err != nil {
		return fmt.Errorf("exec migration: %w", err)
	}
	log.Println("[analytics-db] migrations applied successfully")
	return nil
}

// ValidateAgentToken verifies if the agent token exists and is not revoked.
// Returns agent_id.
func (db *DB) ValidateAgentToken(token string) (string, error) {
	var agentID string
	err := db.QueryRow(`
		SELECT agent_id 
		FROM agent_tokens 
		WHERE token = $1 AND revoked_at IS NULL
	`, token).Scan(&agentID)
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
