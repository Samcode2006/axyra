package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/axyra/analytics/internal/aggregate"
	"github.com/axyra/analytics/internal/db"
	"github.com/axyra/analytics/internal/models"
	"github.com/axyra/analytics/internal/queue"
)

const (
	// maxRetries is the number of processing attempts before an event is sent
	// to the dead-letter table.
	maxRetries = 5
	// baseRetryDelay is the initial back-off delay; each attempt doubles it.
	baseRetryDelay = 200 * time.Millisecond
	// maxRetryDelay caps the exponential back-off.
	maxRetryDelay = 10 * time.Second
)

type Worker struct {
	db    *db.DB
	queue queue.Queue
	stop  chan struct{}
}

func New(database *db.DB, q queue.Queue) *Worker {
	return &Worker{
		db:    database,
		queue: q,
		stop:  make(chan struct{}),
	}
}

func (w *Worker) Start(ctx context.Context) {
	log.Println("[analytics-worker] worker started processing events")
	go func() {
		for {
			select {
			case <-ctx.Done():
				log.Println("[analytics-worker] worker stopping: context done")
				return
			case <-w.stop:
				log.Println("[analytics-worker] worker stopped")
				return
			default:
				event, err := w.queue.Pop(ctx, 1*time.Second)
				if err != nil {
					log.Printf("[analytics-worker] queue pop error: %v", err)
					time.Sleep(500 * time.Millisecond)
					continue
				}
				if event == nil {
					continue
				}

				// Retry loop with exponential back-off.
				// BLPop removes the event before processing; this retry loop does
				// not protect an in-flight event against a process crash.
				w.processWithRetry(ctx, event)
			}
		}
	}()
}

func (w *Worker) Stop() {
	close(w.stop)
}

// processWithRetry attempts to process the event up to maxRetries times, with
// exponential back-off. On exhaustion the event is written to dead_letter_events.
func (w *Worker) processWithRetry(ctx context.Context, e *models.EventEnvelope) {
	delay := baseRetryDelay
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := w.ProcessEvent(ctx, e)
		if err == nil {
			return
		}

		log.Printf("[analytics-worker] attempt %d/%d failed for event %s: %v", attempt, maxRetries, e.EventID, err)

		if attempt == maxRetries {
			reason := fmt.Sprintf("exhausted %d retries; last error: %v", maxRetries, err)
			log.Printf("[analytics-worker] sending event %s to dead-letter: %s", e.EventID, reason)
			payload, _ := json.Marshal(e)
			if dlErr := w.db.InsertDeadLetter(ctx, e.AgentID, e.TenantID, e.EventID, e.LSN, reason, payload); dlErr != nil {
				log.Printf("[analytics-worker] failed to insert dead-letter for event %s: %v", e.EventID, dlErr)
			}
			// Mark in processed_events so we do not retry on worker restart.
			_, _ = w.db.ExecContext(ctx, `
				INSERT INTO processed_events (event_id, tenant_id, processed_at, status, error_message)
				VALUES ($1, $2, NOW(), 'dead_letter', $3)
				ON CONFLICT (event_id) DO NOTHING
			`, e.EventID, e.TenantID, reason)
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if delay < maxRetryDelay {
			delay *= 2
			if delay > maxRetryDelay {
				delay = maxRetryDelay
			}
		}
	}
}

func (w *Worker) ProcessEvent(ctx context.Context, e *models.EventEnvelope) error {
	// 2. Start transaction
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	if err := db.LockTenant(ctx, tx, e.TenantID); err != nil {
		return fmt.Errorf("lock tenant: %w", err)
	}
	// Check after acquiring the lock so a concurrent delivery cannot apply twice.
	var alreadyProcessed bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM processed_events WHERE event_id = $1)", e.EventID).Scan(&alreadyProcessed); err != nil {
		return fmt.Errorf("check deduplication: %w", err)
	}
	if alreadyProcessed {
		return nil
	}

	// 3. Insert raw event
	rawBytes, err := json.Marshal(e.Data)
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}
	occurredAt := e.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO raw_events (event_id, agent_id, tenant_id, external_tenant_id, event_type, schema_version, lsn, occurred_at, data)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (event_id) DO NOTHING
	`, e.EventID, e.AgentID, e.TenantID, e.ExternalTenantID, e.EventType, e.SchemaVersion, e.LSN, occurredAt, string(rawBytes))
	if err != nil {
		return fmt.Errorf("insert raw event: %w", err)
	}

	// 4. Apply the order to order_state and every aggregate table. The rebuild
	// handler calls the same function, so both paths produce identical results.
	if err := aggregate.ProcessOrderEvent(ctx, tx, e.TenantID, occurredAt, e.Data, e.SchemaVersion); err != nil {
		return err
	}

	// 5. Upsert processing_watermarks
	_, err = tx.ExecContext(ctx, `
		INSERT INTO processing_watermarks (agent_id, tenant_id, last_lsn, last_processed_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (agent_id) DO UPDATE SET
			tenant_id = EXCLUDED.tenant_id,
			last_lsn = EXCLUDED.last_lsn,
			last_processed_at = EXCLUDED.last_processed_at,
			updated_at = NOW();
	`, e.AgentID, e.TenantID, e.LSN, occurredAt)
	if err != nil {
		return fmt.Errorf("upsert watermark: %w", err)
	}

	// 6. Mark processed_events
	_, err = tx.ExecContext(ctx, `
		INSERT INTO processed_events (event_id, tenant_id, processed_at, status)
		VALUES ($1, $2, NOW(), 'processed')
		ON CONFLICT (event_id) DO NOTHING;
	`, e.EventID, e.TenantID)
	if err != nil {
		return fmt.Errorf("insert processed_events: %w", err)
	}

	// 7. Count this event toward the per-minute throughput shown on the monitoring
	// page. Done in the same transaction, so it only counts committed events.
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO op_ingestion_rate (bucket, event_count)
		VALUES (DATE_TRUNC('minute', NOW()), 1)
		ON CONFLICT (bucket) DO UPDATE SET event_count = op_ingestion_rate.event_count + 1
	`); err != nil {
		return fmt.Errorf("record ingestion rate: %w", err)
	}

	// 8. Touch agent status
	_, _ = tx.ExecContext(ctx, `
		UPDATE agents 
		SET status = 'connected', last_seen_at = NOW() 
		WHERE id = $1
	`, e.AgentID)

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	// 9. Advance the confirmed LSN cursor so the agent can restart safely.
	// This is done outside the transaction: if it fails, the worst case is
	// the agent re-delivers already-processed events, which idempotency handles.
	if e.LSN != "" {
		if curErr := w.db.UpsertAgentCursor(ctx, e.AgentID, e.ExternalTenantID, e.LSN); curErr != nil {
			log.Printf("[analytics-worker] warning: failed to advance agent cursor for %s to %s: %v", e.AgentID, e.LSN, curErr)
		}
	}

	log.Printf("[analytics-worker] processed event %s (tenant=%s, lsn=%s)", e.EventID, e.TenantID, e.LSN)
	return nil
}

func getString(m map[string]interface{}, key string, def string) string {
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
