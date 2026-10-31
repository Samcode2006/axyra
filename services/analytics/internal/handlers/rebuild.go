package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"
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

// rebuildTenant clears and re-derives all aggregates for tenantID by streaming
// raw_events in chronological order and applying the same upsert logic used
// by the worker.
func (h *Handler) rebuildTenant(ctx context.Context, tenantID string) (int, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// 1. Clear all precomputed aggregates for this tenant.
	for _, table := range []string{
		"order_summary_hourly",
		"order_summary_daily",
		"product_aggregates",
		"order_state",
	} {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE tenant_id = $1`, table), tenantID); err != nil {
			return 0, fmt.Errorf("clear %s: %w", table, err)
		}
	}

	// 2. Stream raw events in chronological order.
	rows, err := tx.QueryContext(ctx, `
		SELECT event_id, agent_id, external_tenant_id, event_type, schema_version, lsn, occurred_at, data
		FROM raw_events
		WHERE tenant_id = $1
		ORDER BY occurred_at ASC, id ASC
	`, tenantID)
	if err != nil {
		return 0, fmt.Errorf("query raw events: %w", err)
	}
	defer rows.Close()

	processed := 0
	for rows.Next() {
		var (
			eventID, agentID, extTenantID, eventType, lsn string
			schemaVersion                                  int
			occurredAt                                     time.Time
			dataRaw                                        []byte
		)
		if err := rows.Scan(&eventID, &agentID, &extTenantID, &eventType, &schemaVersion, &lsn, &occurredAt, &dataRaw); err != nil {
			return processed, fmt.Errorf("scan: %w", err)
		}

		var data map[string]interface{}
		if err := json.Unmarshal(dataRaw, &data); err != nil {
			continue // skip unparseable rows
		}

		if err := applyOrderAggregates(ctx, tx, tenantID, eventID, data, occurredAt); err != nil {
			return processed, fmt.Errorf("apply event %s: %w", eventID, err)
		}
		processed++
	}
	if err := rows.Err(); err != nil {
		return processed, fmt.Errorf("rows error: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return processed, nil
}

// applyOrderAggregates applies one raw event's contributions to the aggregate
// tables within the supplied transaction. This mirrors the worker logic but
// operates within a single rebuild transaction.
func applyOrderAggregates(ctx context.Context, tx *sql.Tx, tenantID, eventID string, data map[string]interface{}, occurredAt time.Time) error {
	orderID := getString(data, "order_id", getString(data, "id", ""))
	if orderID == "" {
		return nil // non-order event; skip
	}

	amount := getFloat(data, "amount", 0.0)
	currency := getString(data, "currency", "INR")
	status := getString(data, "status", "placed")
	productID := getString(data, "product_id", "")
	productName := getString(data, "product_name", "Unknown Product")
	category := getString(data, "category", "General")

	// Load prior state (if exists).
	var priorStatus string
	var priorAmount float64
	var exists bool
	err := tx.QueryRowContext(ctx, `
		SELECT status, amount FROM order_state
		WHERE tenant_id = $1 AND order_id = $2`, tenantID, orderID).Scan(&priorStatus, &priorAmount)
	if err == nil {
		exists = true
	} else if err != sql.ErrNoRows {
		return fmt.Errorf("query prior state: %w", err)
	}

	var deltaTotalOrders, deltaActiveOrders, deltaCancelledOrders int64
	var deltaRevenue float64

	if !exists {
		deltaTotalOrders = 1
		if status == "cancelled" {
			deltaCancelledOrders = 1
		} else {
			deltaActiveOrders = 1
			deltaRevenue = amount
		}
	} else {
		if priorStatus != "cancelled" && status == "cancelled" {
			deltaActiveOrders = -1
			deltaCancelledOrders = 1
			deltaRevenue = -priorAmount
		} else if priorStatus == "cancelled" && status != "cancelled" {
			deltaActiveOrders = 1
			deltaCancelledOrders = -1
			deltaRevenue = amount
		} else if priorStatus != "cancelled" {
			deltaRevenue = amount - priorAmount
		}
	}

	// Upsert order_state
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO order_state (order_id, tenant_id, amount, currency, status, product_id, product_name, category, occurred_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW())
		ON CONFLICT (tenant_id, order_id) DO UPDATE SET
			amount=EXCLUDED.amount, currency=EXCLUDED.currency, status=EXCLUDED.status,
			product_id=EXCLUDED.product_id, product_name=EXCLUDED.product_name,
			category=EXCLUDED.category, updated_at=NOW()
	`, orderID, tenantID, amount, currency, status, productID, productName, category, occurredAt); err != nil {
		return fmt.Errorf("upsert order_state: %w", err)
	}

	hourlyBucket := occurredAt.Truncate(time.Hour)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO order_summary_hourly (tenant_id, bucket, total_orders, active_orders, cancelled_orders, total_revenue, currency)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (tenant_id, bucket) DO UPDATE SET
			total_orders=order_summary_hourly.total_orders+EXCLUDED.total_orders,
			active_orders=order_summary_hourly.active_orders+EXCLUDED.active_orders,
			cancelled_orders=order_summary_hourly.cancelled_orders+EXCLUDED.cancelled_orders,
			total_revenue=order_summary_hourly.total_revenue+EXCLUDED.total_revenue
	`, tenantID, hourlyBucket, deltaTotalOrders, deltaActiveOrders, deltaCancelledOrders, deltaRevenue, currency); err != nil {
		return fmt.Errorf("upsert hourly: %w", err)
	}

	dailyBucket := occurredAt.Format("2006-01-02")
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO order_summary_daily (tenant_id, bucket, total_orders, active_orders, cancelled_orders, total_revenue, currency)
		VALUES ($1,$2::date,$3,$4,$5,$6,$7)
		ON CONFLICT (tenant_id, bucket) DO UPDATE SET
			total_orders=order_summary_daily.total_orders+EXCLUDED.total_orders,
			active_orders=order_summary_daily.active_orders+EXCLUDED.active_orders,
			cancelled_orders=order_summary_daily.cancelled_orders+EXCLUDED.cancelled_orders,
			total_revenue=order_summary_daily.total_revenue+EXCLUDED.total_revenue
	`, tenantID, dailyBucket, deltaTotalOrders, deltaActiveOrders, deltaCancelledOrders, deltaRevenue, currency); err != nil {
		return fmt.Errorf("upsert daily: %w", err)
	}

	if productID != "" {
		var deltaProductOrder int64
		if !exists && status != "cancelled" {
			deltaProductOrder = 1
		} else if exists && priorStatus == "cancelled" && status != "cancelled" {
			deltaProductOrder = 1
		} else if exists && priorStatus != "cancelled" && status == "cancelled" {
			deltaProductOrder = -1
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO product_aggregates (tenant_id, product_id, product_name, category, order_count, total_revenue)
			VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT (tenant_id, product_id) DO UPDATE SET
				order_count=product_aggregates.order_count+EXCLUDED.order_count,
				total_revenue=product_aggregates.total_revenue+EXCLUDED.total_revenue,
				product_name=EXCLUDED.product_name, category=EXCLUDED.category
		`, tenantID, productID, productName, category, deltaProductOrder, deltaRevenue); err != nil {
			return fmt.Errorf("upsert product: %w", err)
		}
	}
	return nil
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
