package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/axyra/analytics/internal/aggregate"
	"github.com/axyra/analytics/internal/db"
)

// Rebuild truncates all precomputed aggregates for a tenant and replays
// every raw event from the ledger, reconstructing the aggregates from scratch.
//
// POST /v1/analytics/rebuild?tenant_id=...
//
// This is a safe, idempotent operation (B-2). It runs synchronously because
// it is expected to be triggered by an operator after a data correction.
// For very large tenants this will take proportional time; the client should
// use a long timeout.
func (h *Handler) Rebuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	tenantID := h.resolveRequestTenant(r)

	log.Printf("[analytics-rebuild] starting rebuild for tenant %s", tenantID)
	start := time.Now()

	processed, err := h.rebuildTenant(r.Context(), tenantID)
	if err != nil {
		log.Printf("[analytics-rebuild] rebuild failed for tenant %s: %v", tenantID, err)
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Rebuild failed: %v", err))
		return
	}

	elapsed := time.Since(start)
	log.Printf("[analytics-rebuild] completed rebuild for tenant %s: %d events in %s", tenantID, processed, elapsed)
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"tenant_id":       tenantID,
		"events_replayed": processed,
		"duration_ms":     elapsed.Milliseconds(),
	})
}

// rebuildBatchSize is how many ledger events are read and applied per round trip.
const rebuildBatchSize = 2000

// rebuildTenant clears and re-derives all aggregates for tenantID by streaming
// raw_events in ledger insertion order and applying the same upsert logic used
// by the worker.
func (h *Handler) rebuildTenant(ctx context.Context, tenantID string) (int, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	if err := db.LockTenant(ctx, tx, tenantID); err != nil {
		return 0, fmt.Errorf("lock tenant: %w", err)
	}

	// 1. Clear all precomputed aggregates for this tenant. The list lives next to
	// the engine that writes the tables, so the two cannot drift apart.
	for _, table := range aggregate.TenantTables {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE tenant_id = $1`, table), tenantID); err != nil {
			return 0, fmt.Errorf("clear %s: %w", table, err)
		}
	}

	// 2. Replay in ledger insertion order, matching live processing even when
	// source timestamps arrive out of order. The tenant lock freezes this ledger.
	//
	// Each batch is read completely and its cursor closed before any event is
	// applied: lib/pq cannot run other statements on the same connection while a
	// result set is still open, so streaming the whole ledger through one cursor
	// fails once the result outgrows the driver's first read. Batching also keeps
	// memory flat for very large tenants.
	type ledgerEvent struct {
		id            int64
		eventID       string
		schemaVersion int
		occurredAt    time.Time
		data          []byte
	}
	var (
		processed int
		lastID    int64
	)
	for {
		rows, err := tx.QueryContext(ctx, `
			SELECT id, event_id, schema_version, occurred_at, data
			FROM raw_events
			WHERE tenant_id = $1 AND id > $2
			ORDER BY id ASC
			LIMIT $3
		`, tenantID, lastID, rebuildBatchSize)
		if err != nil {
			return processed, fmt.Errorf("query raw events: %w", err)
		}
		batch := make([]ledgerEvent, 0, rebuildBatchSize)
		for rows.Next() {
			var ev ledgerEvent
			if err := rows.Scan(&ev.id, &ev.eventID, &ev.schemaVersion, &ev.occurredAt, &ev.data); err != nil {
				rows.Close()
				return processed, fmt.Errorf("scan: %w", err)
			}
			batch = append(batch, ev)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return processed, fmt.Errorf("rows error: %w", err)
		}
		rows.Close()
		if len(batch) == 0 {
			break
		}

		for _, ev := range batch {
			var data map[string]interface{}
			if err := json.Unmarshal(ev.data, &data); err != nil {
				return processed, fmt.Errorf("decode event %s: %w", ev.eventID, err)
			}
			if err := aggregate.ProcessOrderEvent(ctx, tx, tenantID, ev.occurredAt, data, ev.schemaVersion); err != nil {
				return processed, fmt.Errorf("apply event %s: %w", ev.eventID, err)
			}
			processed++
		}
		last := batch[len(batch)-1]
		lastID = last.id
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return processed, nil
}

// helpers re-used from worker (copied here to avoid cross-package import)
func getString(m map[string]interface{}, key, def string) string {
	if v, ok := m[key]; ok && v != nil {
		return fmt.Sprintf("%v", v)
	}
	return def
}

func getFloat(m map[string]interface{}, key string, def float64) float64 {
	if v, ok := m[key]; ok && v != nil {
		switch val := v.(type) {
		case float64:
			return val
		case float32:
			return float64(val)
		case int:
			return float64(val)
		case int64:
			return float64(val)
		case string:
			if f, err := strconv.ParseFloat(val, 64); err == nil {
				return f
			}
		}
	}
	return def
}
