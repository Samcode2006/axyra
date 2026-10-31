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

// supportedSchemaVersions lists all event schema versions the data plane accepts.
// v1 carries the 7 base order fields; v2 adds the extended order fields (see
// docs/schema-expansion-plan.md). Both stay accepted so agents can be upgraded
// one at a time.
var supportedSchemaVersions = map[int]bool{1: true, 2: true}

// validateEvent checks that a raw event envelope has all required fields and an
// accepted schema version. It returns a human-readable reason when invalid.
func validateEvent(e *models.EventEnvelope) string {
	if e.EventID == "" {
		return "event_id is required"
	}
	if e.ExternalTenantID == "" {
		return "external_tenant_id is required"
	}
	if e.EventType == "" {
		return "event_type is required"
	}
	if e.LSN == "" {
		return "lsn is required"
	}
	if e.SchemaVersion == 0 {
		// tolerate missing schema_version by treating it as v1
		e.SchemaVersion = 1
	}
	if !supportedSchemaVersions[e.SchemaVersion] {
		return fmt.Sprintf("unsupported schema_version %d; accepted: 1, 2", e.SchemaVersion)
	}
	return ""
}

// mustMarshal marshals v to JSON bytes, returning nil on error.
func mustMarshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

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
	var rejectedCount int

	for i := range events {
		e := events[i]

		// Schema validation — reject individual invalid events and continue
		if reason := validateEvent(&e); reason != "" {
			log.Printf("[analytics-ingest] rejecting event index %d from agent %s: %s", i, agentID, reason)
			// Store in dead-letter for visibility
			_ = h.db.InsertDeadLetter(r.Context(), agentID, "", e.EventID, e.LSN, reason, mustMarshal(e))
			rejectedCount++
			continue
		}

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
	if len(resolvedEvents) > 0 {
		if err := h.queue.Push(r.Context(), resolvedEvents); err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to queue events: %v", err))
			return
		}
	}

	respondJSON(w, http.StatusAccepted, models.IngestResponse{
		Status:   "accepted",
		Count:    len(resolvedEvents),
		Rejected: rejectedCount,
		LastLSN:  lastLSN,
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

	// Each silo has an independent WAL timeline, so its cursor must be keyed by
	// the source named by the agent. Sharing one cursor would skip other silos.
	source := r.URL.Query().Get("source")
	confirmedLSN, _ := h.db.GetAgentCursor(agentID, source)

	var lastProcessedAt time.Time
	_ = h.db.QueryRowContext(r.Context(), `
		SELECT last_processed_at
		FROM processing_watermarks
		WHERE agent_id = $1
	`, agentID).Scan(&lastProcessedAt)
	if lastProcessedAt.IsZero() {
		lastProcessedAt = time.Now().UTC()
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"agent_id":          agentID,
		"source":            source,
		"confirmed_lsn":     confirmedLSN,
		"last_processed_at": lastProcessedAt,
	})
}
