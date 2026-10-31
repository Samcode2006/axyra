package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/axyra/control-plane/internal/models"
)

// ListAuditLogs returns administrative changes, newest first. Admins see only
// their organization's records; operators see all, or one organization with
// ?organization_id=. Page backwards with before_id=<next_before_id>.
func (h *Handler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	q := r.URL.Query()
	opt := func(key string) *string {
		if v := q.Get(key); v != "" {
			return &v
		}
		return nil
	}
	var before *int64
	if v := q.Get("before_id"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			respondError(w, http.StatusBadRequest, "before_id must be a positive integer")
			return
		}
		before = &n
	}
	limit := queryInt(r, "limit", 50, 1, 200)

	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, organization_id, actor_id, actor_name, actor_role, action, resource_type, resource_id, details, created_at
		FROM audit_logs
		WHERE ($1::text IS NULL OR organization_id = $1)
		  AND ($2::text IS NULL OR resource_type = $2)
		  AND ($3::text IS NULL OR resource_id = $3)
		  AND ($4::text IS NULL OR action = $4)
		  AND ($5::bigint IS NULL OR id < $5)
		ORDER BY id DESC
		LIMIT $6`, scopeOrgFilter(r, p), opt("resource_type"), opt("resource_id"), opt("action"), before, limit+1)
	if err != nil {
		serverError(w, r, err)
		return
	}
	defer rows.Close()

	page := models.AuditLogPage{Data: make([]models.AuditLog, 0, limit)}
	for rows.Next() {
		var l models.AuditLog
		var details []byte
		if err := rows.Scan(&l.ID, &l.OrganizationID, &l.ActorID, &l.ActorName, &l.ActorRole, &l.Action,
			&l.ResourceType, &l.ResourceID, &details, &l.CreatedAt); err != nil {
			serverError(w, r, err)
			return
		}
		l.Details = json.RawMessage(details)
		page.Data = append(page.Data, l)
	}
	if err := rows.Err(); err != nil {
		serverError(w, r, err)
		return
	}
	if len(page.Data) > limit {
		page.Data = page.Data[:limit]
		next := page.Data[limit-1].ID
		page.NextBeforeID = &next
	}
	respondJSON(w, http.StatusOK, page)
}
