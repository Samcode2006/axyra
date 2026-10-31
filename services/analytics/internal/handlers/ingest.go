package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/axyra/analytics/internal/models"
)

func (h *Handler) IngestEvents(w http.ResponseWriter, r *http.Request) {
	token := extractBearerToken(r)
	if token == "" {
		respondError(w, http.StatusUnauthorized, "Agent token missing")
		return
	}

	agentID, err := h.db.ValidateAgentToken(token)
	if err != nil {
		respondError(w, http.StatusUnauthorized, fmt.Sprintf("Invalid agent token: %v", err))
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Failed to read request body")
		return
	}

	var events []models.EventEnvelope

	// Attempt parsing as IngestRequest { "events": [...] }
	var ingestReq models.IngestRequest
	if err := json.Unmarshal(bodyBytes, &ingestReq); err == nil && len(ingestReq.Events) > 0 {
		events = ingestReq.Events
	} else {
		// Attempt parsing as array [...]
		var arrayReq []models.EventEnvelope
		if err := json.Unmarshal(bodyBytes, &arrayReq); err == nil && len(arrayReq) > 0 {
			events = arrayReq
		} else {
			// Attempt parsing as single event {...}
			var singleReq models.EventEnvelope
			if err := json.Unmarshal(bodyBytes, &singleReq); err == nil && singleReq.EventID != "" {
				events = []models.EventEnvelope{singleReq}
			}
		}
	}

	if len(events) == 0 {
		respondError(w, http.StatusBadRequest, "No valid events found in request body")
		return
	}

	lastLSN := ""
	resolvedEvents := make([]models.EventEnvelope, 0, len(events))

	for i := range events {
		e := events[i]
		e.AgentID = agentID

		if e.OccurredAt.IsZero() {
			e.OccurredAt = time.Now().UTC()
		}

		// Resolve external tenant ID to canonical tenant ID
		canonicalTenant, err := h.db.ResolveTenant(agentID, e.ExternalTenantID)
		if err != nil {
			log.Printf("[analytics-ingest] warning: cannot resolve tenant %s for agent %s: %v", e.ExternalTenantID, agentID, err)
			// fallback: use external ID directly if no mapping exists
			e.TenantID = e.ExternalTenantID
		} else {
			e.TenantID = canonicalTenant
		}

		if e.LSN != "" {
			lastLSN = e.LSN
		}
		resolvedEvents = append(resolvedEvents, e)
	}

	// Push to queue
	if err := h.queue.Push(r.Context(), resolvedEvents); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to queue events: %v", err))
		return
	}

	respondJSON(w, http.StatusAccepted, models.IngestResponse{
		Status:  "accepted",
		Count:   len(resolvedEvents),
		LastLSN: lastLSN,
	})
}

func (h *Handler) AgentAck(w http.ResponseWriter, r *http.Request) {
	token := extractBearerToken(r)
	if token == "" {
		respondError(w, http.StatusUnauthorized, "Agent token missing")
		return
	}

	agentID, err := h.db.ValidateAgentToken(token)
	if err != nil {
		respondError(w, http.StatusUnauthorized, fmt.Sprintf("Invalid agent token: %v", err))
		return
	}

	var lastLSN string
	var lastProcessedAt time.Time
	err = h.db.QueryRowContext(r.Context(), `
		SELECT last_lsn, last_processed_at 
		FROM processing_watermarks 
		WHERE agent_id = $1
	`, agentID).Scan(&lastLSN, &lastProcessedAt)
	if err != nil {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"agent_id":          agentID,
			"last_lsn":          "0/0",
			"last_processed_at": time.Now().UTC(),
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"agent_id":          agentID,
		"last_lsn":          lastLSN,
		"last_processed_at": lastProcessedAt,
	})
}
