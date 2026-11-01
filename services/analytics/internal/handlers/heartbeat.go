package handlers

import "net/http"

// AgentHeartbeat records connectivity without fabricating an order event.
func (h *Handler) AgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	token := extractBearerToken(r)
	if token == "" {
		respondError(w, http.StatusUnauthorized, "Agent token missing")
		return
	}
	id, err := h.db.ValidateAgentToken(token)
	if err != nil {
		respondError(w, http.StatusUnauthorized, "Invalid agent token")
		return
	}
	if _, err = h.db.ExecContext(r.Context(), `UPDATE agents SET last_seen_at=NOW(),status='connected' WHERE id=$1`, id); err != nil {
		respondError(w, 500, "Could not record heartbeat")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
