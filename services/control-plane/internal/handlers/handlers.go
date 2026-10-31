package handlers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/axyra/control-plane/internal/auth"
	"github.com/axyra/control-plane/internal/config"
	"github.com/lib/pq"
)

const maxBodyBytes = 1 << 20 // 1 MiB

type Handler struct {
	db        *sql.DB
	cfg       *config.Config
	jwtSecret []byte
	login     *loginLimiter
}

func New(db *sql.DB, cfg *config.Config) *Handler {
	return &Handler{
		db:        db,
		cfg:       cfg,
		jwtSecret: []byte(cfg.JWTSecret),
		login:     newLoginLimiter(),
	}
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "control-plane"})
}

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		_ = json.NewEncoder(w).Encode(data)
	}
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]interface{}{
		"error": map[string]string{
			"message": message,
		},
	})
}

// serverError logs the real cause and returns a generic 500, so database
// details never reach API clients.
func serverError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("[control-plane] %s %s: %v", r.Method, r.URL.Path, err)
	respondError(w, http.StatusInternalServerError, "Internal server error")
}

func isForeignKeyViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23503"
}

// writeDBError maps well-known constraint failures to client errors and
// everything else to a 500.
func writeDBError(w http.ResponseWriter, r *http.Request, err error, conflictMsg string) {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		switch pqErr.Code {
		case "23505": // unique_violation
			respondError(w, http.StatusConflict, conflictMsg)
			return
		case "23503": // foreign_key_violation
			respondError(w, http.StatusBadRequest, "Referenced resource does not exist")
			return
		}
	}
	serverError(w, r, err)
}

// decodeJSON reads a single JSON object, rejecting unknown fields, trailing
// data and bodies over 1 MiB. It writes the 400 itself and returns false on failure.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			respondError(w, http.StatusRequestEntityTooLarge, "Request body too large")
		} else {
			respondError(w, http.StatusBadRequest, "Invalid request body: "+jsonErrMessage(err))
		}
		return false
	}
	if _, err := dec.Token(); err != io.EOF {
		respondError(w, http.StatusBadRequest, "Invalid request body: unexpected trailing data")
		return false
	}
	return true
}

func jsonErrMessage(err error) string {
	if errors.Is(err, io.EOF) {
		return "body is empty"
	}
	return err.Error()
}

// principal returns the authenticated caller. Routes are mounted behind the
// Authenticate middleware, so a nil here is a wiring bug and fails closed.
func principal(w http.ResponseWriter, r *http.Request) *auth.Principal {
	p := auth.FromContext(r.Context())
	if p == nil {
		respondError(w, http.StatusUnauthorized, "Authentication required")
	}
	return p
}

// requireOperator limits an endpoint to platform operators.
func requireOperator(w http.ResponseWriter, p *auth.Principal) bool {
	if !p.IsOperator() {
		respondError(w, http.StatusForbidden, "This action requires a platform operator")
		return false
	}
	return true
}

func notFound(w http.ResponseWriter, what string) {
	respondError(w, http.StatusNotFound, what+" not found")
}

// ─── Identifiers & validation ────────────────────────────────────────────────

func newID(prefix string) string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return prefix + hex.EncodeToString(b)
}

// cleanName trims and validates a display name.
func cleanName(s string, max int) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || utf8.RuneCountInString(s) > max || strings.ContainsAny(s, "\x00\r\n") {
		return "", false
	}
	return s, true
}

// cleanExternalID validates an identifier that comes from a customer's database.
func cleanExternalID(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 64 || strings.ContainsAny(s, "\x00\r\n") {
		return "", false
	}
	return s, true
}

func queryInt(r *http.Request, key string, def, min, max int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// ─── Transactions & audit ────────────────────────────────────────────────────

// withTx runs fn in a transaction, committing only if fn returns nil.
func (h *Handler) withTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

type auditEntry struct {
	OrgID        string // empty when the event is not tied to one organization
	Action       string
	ResourceType string
	ResourceID   string
	Details      map[string]any
}

// audit records an administrative change inside the same transaction as the
// change itself, so a mutation can never be committed without its audit record.
// Callers must never put secrets in Details.
func audit(ctx context.Context, tx *sql.Tx, p *auth.Principal, e auditEntry) error {
	details := e.Details
	if details == nil {
		details = map[string]any{}
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return err
	}
	var org any
	if e.OrgID != "" {
		org = e.OrgID
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO audit_logs (organization_id, actor_id, actor_name, actor_role, action, resource_type, resource_id, details)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		org, p.UserID, p.Username, string(p.Role), e.Action, e.ResourceType, e.ResourceID, string(raw))
	return err
}

// ─── Organization scope ──────────────────────────────────────────────────────
//
// Every resource belongs to exactly one organization. A customer admin may only
// touch resources of their own organization; a resource outside that scope is
// reported as "not found", never "forbidden", so ids from other customers
// cannot be probed.

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func orgOf(ctx context.Context, q queryer, query string, id string) (string, error) {
	var org string
	err := q.QueryRowContext(ctx, query, id).Scan(&org)
	return org, err
}

func orgOfApplication(ctx context.Context, q queryer, id string) (string, error) {
	return orgOf(ctx, q, `SELECT organization_id FROM applications WHERE id = $1`, id)
}

func orgOfAgent(ctx context.Context, q queryer, id string) (string, error) {
	return orgOf(ctx, q, `
		SELECT a.organization_id FROM agents g JOIN applications a ON a.id = g.application_id WHERE g.id = $1`, id)
}

func orgOfTenant(ctx context.Context, q queryer, id string) (string, error) {
	return orgOf(ctx, q, `SELECT organization_id FROM tenants WHERE id = $1`, id)
}

func orgOfPolicy(ctx context.Context, q queryer, id string) (string, error) {
	return orgOf(ctx, q, `
		SELECT t.organization_id FROM policies p JOIN tenants t ON t.id = p.tenant_id WHERE p.id = $1`, id)
}

// authorizeOrg resolves the owning organization with lookup and checks the
// caller's access. It writes the response and returns ok=false when the
// resource is missing or out of scope.
func authorizeOrg(w http.ResponseWriter, r *http.Request, p *auth.Principal, what string,
	lookup func() (string, error)) (orgID string, ok bool) {
	org, err := lookup()
	if errors.Is(err, sql.ErrNoRows) {
		notFound(w, what)
		return "", false
	}
	if err != nil {
		serverError(w, r, err)
		return "", false
	}
	if !p.CanAccessOrg(org) {
		notFound(w, what)
		return "", false
	}
	return org, true
}

// scopeOrgFilter returns the organization a list query must be limited to.
// Admins are always pinned to their own organization. Operators see everything
// unless they pass ?organization_id=.
func scopeOrgFilter(r *http.Request, p *auth.Principal) *string {
	if s := p.OrgScope(); s != nil {
		return s
	}
	if v := r.URL.Query().Get("organization_id"); v != "" {
		return &v
	}
	return nil
}

// resolveCreateOrg decides which organization a newly created resource belongs
// to. Admins default to (and may only use) their own; operators must say.
func resolveCreateOrg(w http.ResponseWriter, p *auth.Principal, requested string) (string, bool) {
	if p.IsOperator() {
		if requested == "" {
			respondError(w, http.StatusBadRequest, "organization_id is required")
			return "", false
		}
		return requested, true
	}
	if requested != "" && requested != p.OrgID {
		// Same answer as a missing organization, so other customers' ids are not revealed.
		notFound(w, "Organization")
		return "", false
	}
	return p.OrgID, true
}
