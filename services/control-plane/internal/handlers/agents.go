package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/axyra/control-plane/internal/auth"
	"github.com/axyra/control-plane/internal/models"
	"github.com/go-chi/chi/v5"
)

const (
	// agentStaleAfter is how long an agent may be silent before it is reported
	// as disconnected, regardless of the last status the data plane wrote.
	agentStaleAfter = 5 * time.Minute

	maxActiveTokensPerAgent = 10
	maxTokenLifetimeDays    = 3650
	maxRotationGrace        = 7 * 24 * time.Hour
)

// agentSelect reports an agent as disconnected when it has not been seen
// recently, even if the data plane last recorded it as connected.
const agentSelect = `
	SELECT g.id, g.application_id, g.name,
	       CASE WHEN g.status = 'error' THEN 'error'
	            WHEN g.last_seen_at IS NULL OR g.last_seen_at < NOW() - INTERVAL '5 minutes' THEN 'disconnected'
	            ELSE g.status END,
	       g.last_seen_at, g.created_at
	FROM agents g
	JOIN applications a ON a.id = g.application_id`

func scanAgent(row interface{ Scan(...any) error }) (models.Agent, error) {
	var a models.Agent
	err := row.Scan(&a.ID, &a.ApplicationID, &a.Name, &a.Status, &a.LastSeenAt, &a.CreatedAt)
	return a, err
}

func (h *Handler) ListAgents(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	var appFilter *string
	if v := r.URL.Query().Get("application_id"); v != "" {
		appFilter = &v
	}
	rows, err := h.db.QueryContext(r.Context(), agentSelect+`
		WHERE ($1::text IS NULL OR a.organization_id = $1)
		  AND ($2::text IS NULL OR g.application_id = $2)
		ORDER BY g.created_at ASC, g.id ASC`, scopeOrgFilter(r, p), appFilter)
	if err != nil {
		serverError(w, r, err)
		return
	}
	defer rows.Close()

	list := make([]models.Agent, 0)
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			serverError(w, r, err)
			return
		}
		list = append(list, a)
	}
	if err := rows.Err(); err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, list)
}

func (h *Handler) GetAgent(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	id := chi.URLParam(r, "agent_id")
	if _, ok := authorizeOrg(w, r, p, "Agent", func() (string, error) { return orgOfAgent(r.Context(), h.db, id) }); !ok {
		return
	}
	a, err := scanAgent(h.db.QueryRowContext(r.Context(), agentSelect+` WHERE g.id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		notFound(w, "Agent")
		return
	}
	if err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, a)
}

func (h *Handler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	var req models.CreateAgentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	name, ok := cleanName(req.Name, maxNameLen)
	if !ok || req.ApplicationID == "" {
		respondError(w, http.StatusBadRequest, "name and application_id are required")
		return
	}
	org, ok := authorizeOrg(w, r, p, "Application", func() (string, error) {
		return orgOfApplication(r.Context(), h.db, req.ApplicationID)
	})
	if !ok {
		return
	}

	id := newID("agent_")
	err := h.withTx(r.Context(), func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(r.Context(), `
			INSERT INTO agents (id, application_id, name, status) VALUES ($1, $2, $3, 'disconnected')`,
			id, req.ApplicationID, name); err != nil {
			return err
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: org, Action: "agent.create", ResourceType: "agent", ResourceID: id,
			Details: map[string]any{"name": name, "application_id": req.ApplicationID}})
	})
	if err != nil {
		if isForeignKeyViolation(err) { // application deleted concurrently
			notFound(w, "Application")
			return
		}
		serverError(w, r, err)
		return
	}
	a, err := scanAgent(h.db.QueryRowContext(r.Context(), agentSelect+` WHERE g.id = $1`, id))
	if err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusCreated, a)
}

func (h *Handler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	id := chi.URLParam(r, "agent_id")
	var req models.UpdateAgentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == nil {
		respondError(w, http.StatusBadRequest, "Nothing to update: name is required")
		return
	}
	name, ok := cleanName(*req.Name, maxNameLen)
	if !ok {
		respondError(w, http.StatusBadRequest, "name must be 1-255 characters")
		return
	}

	err := h.withTx(r.Context(), func(tx *sql.Tx) error {
		org, err := lockAgent(r.Context(), tx, id, p)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(r.Context(), `UPDATE agents SET name = $2 WHERE id = $1`, id, name); err != nil {
			return err
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: org, Action: "agent.update", ResourceType: "agent",
			ResourceID: id, Details: map[string]any{"name": name}})
	})
	if errors.Is(err, sql.ErrNoRows) {
		notFound(w, "Agent")
		return
	}
	if err != nil {
		serverError(w, r, err)
		return
	}
	a, err := scanAgent(h.db.QueryRowContext(r.Context(), agentSelect+` WHERE g.id = $1`, id))
	if err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, a)
}

// DeleteAgent removes an agent. Its tokens and tenant mappings go with it, so
// the running agent is rejected on its next request.
func (h *Handler) DeleteAgent(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	id := chi.URLParam(r, "agent_id")
	err := h.withTx(r.Context(), func(tx *sql.Tx) error {
		org, err := lockAgent(r.Context(), tx, id, p)
		if err != nil {
			return err
		}
		var name string
		if err := tx.QueryRowContext(r.Context(), `DELETE FROM agents WHERE id = $1 RETURNING name`, id).Scan(&name); err != nil {
			return err
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: org, Action: "agent.delete", ResourceType: "agent",
			ResourceID: id, Details: map[string]any{"name": name}})
	})
	if errors.Is(err, sql.ErrNoRows) {
		notFound(w, "Agent")
		return
	}
	if err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// lockAgent row-locks the agent, checks the caller's access and returns the
// owning organization. Missing and out-of-scope are both sql.ErrNoRows.
func lockAgent(ctx context.Context, tx *sql.Tx, id string, p *auth.Principal) (string, error) {
	var org string
	err := tx.QueryRowContext(ctx, `
		SELECT a.organization_id FROM agents g JOIN applications a ON a.id = g.application_id
		WHERE g.id = $1 FOR UPDATE OF g`, id).Scan(&org)
	if err != nil {
		return "", err
	}
	if !p.CanAccessOrg(org) {
		return "", sql.ErrNoRows
	}
	return org, nil
}

// ─── Agent tokens ────────────────────────────────────────────────────────────

const tokenSelect = `
	SELECT id, agent_id, name, token_prefix, created_at, expires_at, revoked_at,
	       CASE WHEN revoked_at IS NOT NULL THEN 'revoked'
	            WHEN expires_at IS NOT NULL AND expires_at <= NOW() THEN 'expired'
	            ELSE 'active' END
	FROM agent_tokens`

// ListAgentTokens shows credential metadata only: the secret cannot be recovered.
func (h *Handler) ListAgentTokens(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	agentID := chi.URLParam(r, "agent_id")
	if _, ok := authorizeOrg(w, r, p, "Agent", func() (string, error) { return orgOfAgent(r.Context(), h.db, agentID) }); !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), tokenSelect+` WHERE agent_id = $1 ORDER BY created_at DESC, id DESC`, agentID)
	if err != nil {
		serverError(w, r, err)
		return
	}
	defer rows.Close()

	list := make([]models.AgentToken, 0)
	for rows.Next() {
		var t models.AgentToken
		if err := rows.Scan(&t.ID, &t.AgentID, &t.Name, &t.TokenPrefix, &t.CreatedAt, &t.ExpiresAt, &t.RevokedAt, &t.Status); err != nil {
			serverError(w, r, err)
			return
		}
		list = append(list, t)
	}
	if err := rows.Err(); err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, list)
}

func validLifetime(days *int) (*time.Time, bool) {
	if days == nil {
		return nil, true
	}
	if *days < 1 || *days > maxTokenLifetimeDays {
		return nil, false
	}
	t := time.Now().UTC().Add(time.Duration(*days) * 24 * time.Hour)
	return &t, true
}

// IssueAgentToken creates a credential. The plaintext is in this response only.
func (h *Handler) IssueAgentToken(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	agentID := chi.URLParam(r, "agent_id")

	var req models.IssueTokenRequest
	if r.ContentLength != 0 { // the body is optional
		if !decodeJSON(w, r, &req) {
			return
		}
	}
	expires, ok := validLifetime(req.ExpiresInDays)
	if !ok {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("expires_in_days must be between 1 and %d", maxTokenLifetimeDays))
		return
	}
	name := fmt.Sprintf("Token created at %s", time.Now().UTC().Format(time.RFC3339))
	if req.Name != nil {
		n, ok := cleanName(*req.Name, maxNameLen)
		if !ok {
			respondError(w, http.StatusBadRequest, "name must be 1-255 characters")
			return
		}
		name = n
	}

	var resp models.IssueTokenResponse
	err := h.withTx(r.Context(), func(tx *sql.Tx) error {
		org, err := lockAgent(r.Context(), tx, agentID, p)
		if err != nil {
			return err
		}
		active, err := countActiveTokens(r.Context(), tx, agentID)
		if err != nil {
			return err
		}
		if active >= maxActiveTokensPerAgent {
			return errTooManyTokens
		}
		resp, err = insertToken(r.Context(), tx, agentID, name, expires)
		if err != nil {
			return err
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: org, Action: "agent_token.issue", ResourceType: "agent_token",
			ResourceID: resp.TokenID, Details: map[string]any{"agent_id": agentID, "token_prefix": resp.TokenPrefix, "expires_at": expires}})
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		notFound(w, "Agent")
	case errors.Is(err, errTooManyTokens):
		respondError(w, http.StatusConflict, fmt.Sprintf("Agent already has %d active tokens; revoke one first", maxActiveTokensPerAgent))
	case err != nil:
		serverError(w, r, err)
	default:
		w.Header().Set("Cache-Control", "no-store")
		respondJSON(w, http.StatusCreated, resp)
	}
}

var errTooManyTokens = errors.New("too many active tokens")

func countActiveTokens(ctx context.Context, tx *sql.Tx, agentID string) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM agent_tokens
		WHERE agent_id = $1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > NOW())`, agentID).Scan(&n)
	return n, err
}

func insertToken(ctx context.Context, tx *sql.Tx, agentID, name string, expires *time.Time) (models.IssueTokenResponse, error) {
	plaintext, prefix, hash, err := auth.NewAgentToken()
	if err != nil {
		return models.IssueTokenResponse{}, err
	}
	id := newID("tok_")
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO agent_tokens (id, agent_id, token_hash, token_prefix, name, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`, id, agentID, hash, prefix, name, expires); err != nil {
		return models.IssueTokenResponse{}, err
	}
	return models.IssueTokenResponse{TokenID: id, Token: plaintext, TokenPrefix: prefix, ExpiresAt: expires}, nil
}

// RotateAgentToken replaces an active token with a new one. With a grace
// period the old token keeps working briefly, so the agent can be redeployed
// with the new secret without dropping events.
func (h *Handler) RotateAgentToken(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	agentID, tokenID := chi.URLParam(r, "agent_id"), chi.URLParam(r, "token_id")

	var req models.RotateTokenRequest
	if r.ContentLength != 0 {
		if !decodeJSON(w, r, &req) {
			return
		}
	}
	grace := time.Duration(req.GracePeriodSeconds) * time.Second
	if req.GracePeriodSeconds < 0 || grace > maxRotationGrace {
		respondError(w, http.StatusBadRequest, "grace_period_seconds must be between 0 and 604800 (7 days)")
		return
	}
	expires, ok := validLifetime(req.ExpiresInDays)
	if !ok {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("expires_in_days must be between 1 and %d", maxTokenLifetimeDays))
		return
	}

	var resp models.RotateTokenResponse
	err := h.withTx(r.Context(), func(tx *sql.Tx) error {
		org, err := lockAgent(r.Context(), tx, agentID, p)
		if err != nil {
			return err
		}

		var name sql.NullString
		var oldExpiry sql.NullTime
		err = tx.QueryRowContext(r.Context(), `
			SELECT name, expires_at FROM agent_tokens
			WHERE id = $1 AND agent_id = $2 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > NOW())
			FOR UPDATE`, tokenID, agentID).Scan(&name, &oldExpiry)
		if err != nil {
			return errTokenNotActive
		}

		// The old token ends now, or at the end of the grace window, whichever
		// is earlier than any expiry it already had.
		var oldEnd *time.Time
		if grace > 0 {
			t := time.Now().UTC().Add(grace)
			if oldExpiry.Valid && oldExpiry.Time.Before(t) {
				t = oldExpiry.Time
			}
			oldEnd = &t
			if _, err := tx.ExecContext(r.Context(), `UPDATE agent_tokens SET expires_at = $2 WHERE id = $1`, tokenID, t); err != nil {
				return err
			}
		} else if _, err := tx.ExecContext(r.Context(), `UPDATE agent_tokens SET revoked_at = NOW() WHERE id = $1`, tokenID); err != nil {
			return err
		}

		// The replacement does not count against the cap: it takes the old token's place.
		newName := "Rotated from " + tokenID
		if name.Valid && name.String != "" {
			newName = name.String
		}
		issued, err := insertToken(r.Context(), tx, agentID, newName, expires)
		if err != nil {
			return err
		}
		resp = models.RotateTokenResponse{IssueTokenResponse: issued, PreviousTokenID: tokenID, PreviousTokenExpiresAt: oldEnd}
		return audit(r.Context(), tx, p, auditEntry{OrgID: org, Action: "agent_token.rotate", ResourceType: "agent_token",
			ResourceID: issued.TokenID, Details: map[string]any{
				"agent_id": agentID, "previous_token_id": tokenID, "token_prefix": issued.TokenPrefix,
				"grace_period_seconds": req.GracePeriodSeconds}})
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		notFound(w, "Agent")
	case errors.Is(err, errTokenNotActive):
		notFound(w, "Active token")
	case err != nil:
		serverError(w, r, err)
	default:
		w.Header().Set("Cache-Control", "no-store")
		respondJSON(w, http.StatusCreated, resp)
	}
}

var errTokenNotActive = errors.New("token not active")

func (h *Handler) RevokeAgentToken(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	agentID, tokenID := chi.URLParam(r, "agent_id"), chi.URLParam(r, "token_id")

	err := h.withTx(r.Context(), func(tx *sql.Tx) error {
		org, err := lockAgent(r.Context(), tx, agentID, p)
		if err != nil {
			return err
		}
		var prefix string
		err = tx.QueryRowContext(r.Context(), `
			UPDATE agent_tokens SET revoked_at = NOW()
			WHERE id = $1 AND agent_id = $2 AND revoked_at IS NULL
			RETURNING token_prefix`, tokenID, agentID).Scan(&prefix)
		if errors.Is(err, sql.ErrNoRows) {
			return errTokenNotActive
		}
		if err != nil {
			return err
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: org, Action: "agent_token.revoke", ResourceType: "agent_token",
			ResourceID: tokenID, Details: map[string]any{"agent_id": agentID, "token_prefix": prefix}})
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		notFound(w, "Agent")
	case errors.Is(err, errTokenNotActive):
		notFound(w, "Active token")
	case err != nil:
		serverError(w, r, err)
	default:
		respondJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
	}
}

// ─── Tenant mappings ─────────────────────────────────────────────────────────

func (h *Handler) ListTenantMappings(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	agentID := chi.URLParam(r, "agent_id")
	if _, ok := authorizeOrg(w, r, p, "Agent", func() (string, error) { return orgOfAgent(r.Context(), h.db, agentID) }); !ok {
		return
	}

	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, agent_id, external_tenant_id, tenant_id, created_at
		FROM tenant_mappings
		WHERE agent_id = $1
		ORDER BY created_at ASC, id ASC`, agentID)
	if err != nil {
		serverError(w, r, err)
		return
	}
	defer rows.Close()

	mappings := make([]models.TenantMapping, 0)
	for rows.Next() {
		var m models.TenantMapping
		if err := rows.Scan(&m.ID, &m.AgentID, &m.ExternalTenantID, &m.TenantID, &m.CreatedAt); err != nil {
			serverError(w, r, err)
			return
		}
		mappings = append(mappings, m)
	}
	if err := rows.Err(); err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, mappings)
}

// CreateTenantMapping maps an external tenant id seen by an agent to a canonical
// tenant. The tenant must belong to the same organization as the agent;
// without that check one customer could route events into another's tenant.
// Re-posting an existing external id re-points it (200); a new one returns 201.
func (h *Handler) CreateTenantMapping(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	agentID := chi.URLParam(r, "agent_id")
	var req models.CreateTenantMappingRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ext, ok := cleanExternalID(req.ExternalTenantID)
	if !ok || req.TenantID == "" {
		respondError(w, http.StatusBadRequest, "external_tenant_id (max 64 chars) and tenant_id are required")
		return
	}

	var m models.TenantMapping
	var inserted bool
	err := h.withTx(r.Context(), func(tx *sql.Tx) error {
		agentOrg, err := lockAgent(r.Context(), tx, agentID, p)
		if err != nil {
			return err
		}
		tenantOrg, err := orgOfTenant(r.Context(), tx, req.TenantID)
		if err != nil || tenantOrg != agentOrg {
			// Missing and cross-organization look identical to the caller.
			return errTenantOutOfScope
		}

		err = tx.QueryRowContext(r.Context(), `
			INSERT INTO tenant_mappings (id, agent_id, external_tenant_id, tenant_id)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (agent_id, external_tenant_id) DO UPDATE SET tenant_id = EXCLUDED.tenant_id
			RETURNING id, agent_id, external_tenant_id, tenant_id, created_at, (xmax = 0)`,
			newID("map_"), agentID, ext, req.TenantID).
			Scan(&m.ID, &m.AgentID, &m.ExternalTenantID, &m.TenantID, &m.CreatedAt, &inserted)
		if err != nil {
			return err
		}
		action := "tenant_mapping.update"
		if inserted {
			action = "tenant_mapping.create"
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: agentOrg, Action: action, ResourceType: "tenant_mapping",
			ResourceID: m.ID, Details: map[string]any{"agent_id": agentID, "external_tenant_id": ext, "tenant_id": req.TenantID}})
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		notFound(w, "Agent")
	case errors.Is(err, errTenantOutOfScope):
		notFound(w, "Tenant")
	case err != nil:
		writeDBError(w, r, err, "Mapping already exists")
	case inserted:
		respondJSON(w, http.StatusCreated, m)
	default:
		respondJSON(w, http.StatusOK, m)
	}
}

var errTenantOutOfScope = errors.New("tenant not in agent's organization")

func (h *Handler) DeleteTenantMapping(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	agentID, mappingID := chi.URLParam(r, "agent_id"), chi.URLParam(r, "mapping_id")

	err := h.withTx(r.Context(), func(tx *sql.Tx) error {
		org, err := lockAgent(r.Context(), tx, agentID, p)
		if err != nil {
			return err
		}
		var ext, tenantID string
		err = tx.QueryRowContext(r.Context(), `
			DELETE FROM tenant_mappings WHERE id = $1 AND agent_id = $2
			RETURNING external_tenant_id, tenant_id`, mappingID, agentID).Scan(&ext, &tenantID)
		if errors.Is(err, sql.ErrNoRows) {
			return errMappingMissing
		}
		if err != nil {
			return err
		}
		return audit(r.Context(), tx, p, auditEntry{OrgID: org, Action: "tenant_mapping.delete", ResourceType: "tenant_mapping",
			ResourceID: mappingID, Details: map[string]any{"agent_id": agentID, "external_tenant_id": ext, "tenant_id": tenantID}})
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		notFound(w, "Agent")
	case errors.Is(err, errMappingMissing):
		notFound(w, "Mapping")
	case err != nil:
		serverError(w, r, err)
	default:
		respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

var errMappingMissing = errors.New("mapping not found")
