package handlers

import (
	"database/sql"
	"errors"
	"github.com/go-chi/chi/v5"
	"net/http"
	"time"
)

var errProvisionLimit = errors.New("managed database limit reached")
var errManagedLifecycle = errors.New("managed lifecycle requires operator action")

func (h *Handler) ProvisioningStatus(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	id := chi.URLParam(r, "id")
	if _, ok := authorizeOrg(w, r, p, "Tenant", func() (string, error) { return orgOfTenant(r.Context(), h.db, id) }); !ok {
		return
	}
	var status, stage, message string
	var updated time.Time
	var agent sql.NullString
	err := h.db.QueryRowContext(r.Context(), `SELECT status,stage,error_message,agent_id,updated_at FROM tenant_provisioning WHERE tenant_id=$1`, id).Scan(&status, &stage, &message, &agent, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		respondJSON(w, 200, map[string]any{"status": "external", "enabled": h.cfg.ManagedProvisioning})
		return
	}
	if err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, 200, map[string]any{"status": status, "stage": stage, "error": message, "agent_id": agent.String, "updated_at": updated, "database": "reference_db", "host": "axyra-managed-" + id, "port": 5432, "enabled": h.cfg.ManagedProvisioning})
}

func (h *Handler) RetryProvisioning(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	if !h.cfg.ManagedProvisioning {
		respondError(w, 503, "Managed provisioning is disabled")
		return
	}
	id := chi.URLParam(r, "id")
	err := h.withTx(r.Context(), func(tx *sql.Tx) error {
		var org string
		if err := tx.QueryRowContext(r.Context(), `SELECT organization_id FROM tenants WHERE id=$1 FOR UPDATE`, id).Scan(&org); err != nil {
			return err
		}
		if !p.CanAccessOrg(org) {
			return sql.ErrNoRows
		}
		result, err := tx.ExecContext(r.Context(), `UPDATE tenant_provisioning SET status='queued',stage='queued',error_message='',updated_at=NOW() WHERE tenant_id=$1 AND status='failed'`, id)
		if err != nil {
			return err
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			return errManagedLifecycle
		}
		if _, err = tx.ExecContext(r.Context(), `UPDATE tenants SET status='provisioning' WHERE id=$1`, id); err != nil {
			return err
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: org, Action: "tenant.provisioning.retry", ResourceType: "tenant", ResourceID: id})
	})
	if errors.Is(err, sql.ErrNoRows) {
		notFound(w, "Tenant")
		return
	}
	if errors.Is(err, errManagedLifecycle) {
		respondError(w, 409, "Only failed provisioning jobs can be retried")
		return
	}
	if err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, 202, map[string]string{"status": "queued"})
}
