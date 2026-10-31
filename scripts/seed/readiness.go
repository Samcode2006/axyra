package main

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Check the publication AND a streaming logical slot in this database. Merely
// having a running agent container is insufficient on a newly created source.
func waitForReplication(ctx context.Context, pool *pgxpool.Pool, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		var ready bool
		err := pool.QueryRow(ctx, `SELECT
			EXISTS (SELECT 1 FROM pg_publication_tables WHERE pubname='axyra_pub'
			        AND schemaname='public' AND tablename='orders')
			AND EXISTS (SELECT 1 FROM pg_replication_slots s
			            JOIN pg_stat_replication r ON r.pid=s.active_pid
			            WHERE s.database=current_database() AND s.plugin='pgoutput'
			              AND s.slot_name LIKE 'axyra_slot%' AND r.state='streaming')`).Scan(&ready)
		if err != nil {
			return fmt.Errorf("check replication readiness: %w", err)
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("agent not ready within %s; register this database as an agent source and start the agent (standalone seeding: -wait-for-agent 0)", timeout)
		case <-ticker.C:
		}
	}
}
