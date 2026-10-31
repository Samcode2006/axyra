package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

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
				// If the worker restarts mid-processing, the event remains in the
				// Redis queue (BLPop only removes on successful dequeue) or in
				// processed_events (idempotency guard catches duplicates).
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
	// 1. Check deduplication
	var alreadyProcessed bool
	err := w.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM processed_events WHERE event_id = $1)", e.EventID).Scan(&alreadyProcessed)
	if err == nil && alreadyProcessed {
		log.Printf("[analytics-worker] skipping duplicate event: %s", e.EventID)
		return nil
	}

	// 2. Start transaction
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// 3. Insert raw event
	rawBytes, _ := json.Marshal(e.Data)
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

	// 4. Extract order fields
	orderID := getString(e.Data, "order_id", getString(e.Data, "id", ""))
	if orderID != "" {
		amount := getFloat(e.Data, "amount", 0.0)
		currency := getString(e.Data, "currency", "INR")
		status := getString(e.Data, "status", "placed")
		productID := getString(e.Data, "product_id", "")
		productName := getString(e.Data, "product_name", "Unknown Product")
		category := getString(e.Data, "category", "General")

		// Check prior order state
		var priorStatus string
		var priorAmount float64
		var exists bool

		err = tx.QueryRowContext(ctx, `
			SELECT status, amount 
			FROM order_state 
			WHERE tenant_id = $1 AND order_id = $2
		`, e.TenantID, orderID).Scan(&priorStatus, &priorAmount)
		if err == nil {
			exists = true
		} else if err != sql.ErrNoRows {
			return fmt.Errorf("query prior order state: %w", err)
		}

		var deltaTotalOrders int64
		var deltaActiveOrders int64
		var deltaCancelledOrders int64
		var deltaRevenue float64

		if !exists {
			deltaTotalOrders = 1
			if status == "cancelled" {
				deltaCancelledOrders = 1
				deltaActiveOrders = 0
				deltaRevenue = 0
			} else {
				deltaActiveOrders = 1
				deltaCancelledOrders = 0
				deltaRevenue = amount
			}
		} else {
			deltaTotalOrders = 0 // already counted in total
			if priorStatus != "cancelled" && status == "cancelled" {
				// Transition to cancelled: revert revenue and active count
				deltaActiveOrders = -1
				deltaCancelledOrders = 1
				deltaRevenue = -priorAmount
			} else if priorStatus == "cancelled" && status != "cancelled" {
				// Re-activated
				deltaActiveOrders = 1
				deltaCancelledOrders = -1
				deltaRevenue = amount
			} else if priorStatus != "cancelled" && status != "cancelled" {
				// Update on active order (e.g. price change)
				deltaActiveOrders = 0
				deltaCancelledOrders = 0
				deltaRevenue = amount - priorAmount
			}
		}

		// Upsert order_state
		_, err = tx.ExecContext(ctx, `
			INSERT INTO order_state (order_id, tenant_id, amount, currency, status, product_id, product_name, category, occurred_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
			ON CONFLICT (tenant_id, order_id) DO UPDATE SET
				amount = EXCLUDED.amount,
				currency = EXCLUDED.currency,
				status = EXCLUDED.status,
				product_id = EXCLUDED.product_id,
				product_name = EXCLUDED.product_name,
				category = EXCLUDED.category,
				updated_at = NOW();
		`, orderID, e.TenantID, amount, currency, status, productID, productName, category, occurredAt)
		if err != nil {
			return fmt.Errorf("upsert order state: %w", err)
		}

		// Upsert order_summary_hourly
		hourlyBucket := occurredAt.Truncate(time.Hour)
		_, err = tx.ExecContext(ctx, `
			INSERT INTO order_summary_hourly (tenant_id, bucket, total_orders, active_orders, cancelled_orders, total_revenue, currency)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (tenant_id, bucket) DO UPDATE SET
				total_orders = order_summary_hourly.total_orders + EXCLUDED.total_orders,
				active_orders = order_summary_hourly.active_orders + EXCLUDED.active_orders,
				cancelled_orders = order_summary_hourly.cancelled_orders + EXCLUDED.cancelled_orders,
				total_revenue = order_summary_hourly.total_revenue + EXCLUDED.total_revenue;
		`, e.TenantID, hourlyBucket, deltaTotalOrders, deltaActiveOrders, deltaCancelledOrders, deltaRevenue, currency)
		if err != nil {
			return fmt.Errorf("upsert order summary hourly: %w", err)
		}

		// Upsert order_summary_daily
		dailyBucket := occurredAt.Format("2006-01-02")
		_, err = tx.ExecContext(ctx, `
			INSERT INTO order_summary_daily (tenant_id, bucket, total_orders, active_orders, cancelled_orders, total_revenue, currency)
			VALUES ($1, $2::date, $3, $4, $5, $6, $7)
			ON CONFLICT (tenant_id, bucket) DO UPDATE SET
				total_orders = order_summary_daily.total_orders + EXCLUDED.total_orders,
				active_orders = order_summary_daily.active_orders + EXCLUDED.active_orders,
				cancelled_orders = order_summary_daily.cancelled_orders + EXCLUDED.cancelled_orders,
				total_revenue = order_summary_daily.total_revenue + EXCLUDED.total_revenue;
		`, e.TenantID, dailyBucket, deltaTotalOrders, deltaActiveOrders, deltaCancelledOrders, deltaRevenue, currency)
		if err != nil {
			return fmt.Errorf("upsert order summary daily: %w", err)
		}

		// Upsert product_aggregates (only if productID is present)
		if productID != "" {
			var deltaProductOrder int64 = 0
			if !exists && status != "cancelled" {
				deltaProductOrder = 1
			} else if exists && priorStatus == "cancelled" && status != "cancelled" {
				deltaProductOrder = 1
			} else if exists && priorStatus != "cancelled" && status == "cancelled" {
				deltaProductOrder = -1
			}

			_, err = tx.ExecContext(ctx, `
				INSERT INTO product_aggregates (tenant_id, product_id, product_name, category, order_count, total_revenue)
				VALUES ($1, $2, $3, $4, $5, $6)
				ON CONFLICT (tenant_id, product_id) DO UPDATE SET
					order_count = product_aggregates.order_count + EXCLUDED.order_count,
					total_revenue = product_aggregates.total_revenue + EXCLUDED.total_revenue,
					product_name = EXCLUDED.product_name,
					category = EXCLUDED.category;
			`, e.TenantID, productID, productName, category, deltaProductOrder, deltaRevenue)
			if err != nil {
				return fmt.Errorf("upsert product aggregates: %w", err)
			}
		}
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

	// 7. Touch agent status
	_, _ = tx.ExecContext(ctx, `
		UPDATE agents 
		SET status = 'connected', last_seen_at = NOW() 
		WHERE id = $1
	`, e.AgentID)

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	// 8. Advance the confirmed LSN cursor so the agent can restart safely.
	// This is done outside the transaction: if it fails, the worst case is
	// the agent re-delivers already-processed events, which idempotency handles.
	if e.LSN != "" {
		if curErr := w.db.UpsertAgentCursor(ctx, e.AgentID, e.LSN); curErr != nil {
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
