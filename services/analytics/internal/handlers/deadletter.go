package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// DeadLetterEvent is the API representation of a dead-letter entry.
type DeadLetterEvent struct {
	ID         int64           `json:"id"`
	AgentID    string          `json:"agent_id"`
	TenantID   string          `json:"tenant_id"`
	EventID    string          `json:"event_id"`
	LSN        string          `json:"lsn"`
	Reason     string          `json:"reason"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	RetryCount int             `json:"retry_count"`
	ReplayedAt *time.Time      `json:"replayed_at,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}

// ListDeadLetterEvents returns dead-letter entries, optionally filtered by
// agent_id query param. It accepts an optional ?limit=N query param (default 100).
func (h *Handler) ListDeadLetterEvents(w http.ResponseWriter, r *http.Request) {
	agentFilter := r.URL.Query().Get("agent_id")
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}

	// With a tenant named (always the case for customer admins) only that
	// tenant's entries are returned; without one, all entries (operators).
	var tenantArg interface{}
	if t := h.explicitTenant(r); t != "" {
		tenantArg = t
	}

	query := `
		SELECT id, agent_id, tenant_id, event_id, lsn, reason, payload, retry_count, replayed_at, created_at
		FROM dead_letter_events
		WHERE ($1::text IS NULL OR agent_id = $1) AND ($2::text IS NULL OR tenant_id = $2)
		ORDER BY created_at DESC
		LIMIT $3`
	var agentArg interface{}
	if agentFilter != "" {
		agentArg = agentFilter
	}
	args := []interface{}{agentArg, tenantArg, limit}

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to query dead-letter events")
		return
	}
	defer rows.Close()

	list := make([]DeadLetterEvent, 0)
	for rows.Next() {
		var e DeadLetterEvent
		var payloadStr *string
		if err := rows.Scan(&e.ID, &e.AgentID, &e.TenantID, &e.EventID, &e.LSN, &e.Reason,
			&payloadStr, &e.RetryCount, &e.ReplayedAt, &e.CreatedAt); err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to scan dead-letter event")
			return
		}
		if payloadStr != nil {
			e.Payload = json.RawMessage(*payloadStr)
		}
		list = append(list, e)
	}
	if err := rows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to iterate dead-letter events")
		return
	}

	respondJSON(w, http.StatusOK, list)
}
