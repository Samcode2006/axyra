package db

import (
	"context"
	"database/sql"
	"fmt"
)

// LockTenant serializes derived-data writes and ledger writes for one tenant
// across all service replicas. The transaction releases the lock on commit or
// rollback. Hash collisions only serialize unrelated tenants; they cannot mix data.
func LockTenant(ctx context.Context, tx *sql.Tx, tenantID string) error {
	if tenantID == "" {
		return fmt.Errorf("tenant ID is required")
	}
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(7243010, hashtext($1))`, tenantID)
	return err
}
