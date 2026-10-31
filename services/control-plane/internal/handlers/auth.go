package handlers

import (
	"database/sql"
	"errors"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/axyra/control-plane/internal/auth"
	"github.com/axyra/control-plane/internal/models"
)

const sessionTTL = 12 * time.Hour

// Login verifies console credentials against the users table and returns a
// short-lived session JWT carrying the user's role and organization.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req models.LoginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Username == "" || req.Password == "" {
		respondError(w, http.StatusBadRequest, "Username and password are required")
		return
	}

	key := req.Username + "|" + clientIP(r)
	if wait := h.login.blockedFor(key, time.Now()); wait > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		respondError(w, http.StatusTooManyRequests, "Too many failed attempts. Try again later.")
		return
	}

	var (
		id, hash, role string
		org            sql.NullString
	)
	err := h.db.QueryRowContext(r.Context(),
		`SELECT id, password_hash, role, organization_id FROM users WHERE username = $1`, req.Username).
		Scan(&id, &hash, &role, &org)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		auth.DummyVerify(req.Password) // keep timing the same for unknown users
		h.login.fail(key, time.Now())
		respondError(w, http.StatusUnauthorized, "Invalid username or password")
		return
	case err != nil:
		serverError(w, r, err)
		return
	}
	if !auth.VerifyPassword(req.Password, hash) {
		h.login.fail(key, time.Now())
		respondError(w, http.StatusUnauthorized, "Invalid username or password")
		return
	}
	h.login.succeed(key)

	p := auth.Principal{UserID: id, Username: req.Username, OrgID: org.String, Role: auth.Role(role)}
	token, err := auth.IssueJWT(h.jwtSecret, p, sessionTTL, time.Now())
	if err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, models.LoginResponse{Token: token, User: userView(&p)})
}

// Me returns the authenticated caller, letting the console pick role-aware navigation.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	respondJSON(w, http.StatusOK, userView(p))
}

func userView(p *auth.Principal) models.User {
	u := models.User{ID: p.UserID, Username: p.Username, Role: string(p.Role)}
	if p.OrgID != "" {
		org := p.OrgID
		u.OrganizationID = &org
	}
	return u
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ─── Login throttling ────────────────────────────────────────────────────────

const (
	loginMaxFailures = 10
	loginWindow      = 15 * time.Minute
)

type attempt struct {
	failures int
	first    time.Time
}

// loginLimiter blocks a (username, IP) pair after repeated failures. It is
// in-process, which is enough for a single control-plane instance.
type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]*attempt
}

func newLoginLimiter() *loginLimiter { return &loginLimiter{attempts: map[string]*attempt{}} }

// blockedFor returns how long the key must wait, or 0 if it may try.
func (l *loginLimiter) blockedFor(key string, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.attempts[key]
	if a == nil {
		return 0
	}
	if now.Sub(a.first) >= loginWindow {
		delete(l.attempts, key)
		return 0
	}
	if a.failures >= loginMaxFailures {
		return loginWindow - now.Sub(a.first)
	}
	return 0
}

func (l *loginLimiter) fail(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.attempts[key]
	if a == nil || now.Sub(a.first) >= loginWindow {
		l.attempts[key] = &attempt{failures: 1, first: now}
		l.evictLocked(now)
		return
	}
	a.failures++
}

func (l *loginLimiter) succeed(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

// evictLocked bounds memory by dropping expired entries once the map grows.
func (l *loginLimiter) evictLocked(now time.Time) {
	if len(l.attempts) < 1024 {
		return
	}
	for k, a := range l.attempts {
		if now.Sub(a.first) >= loginWindow {
			delete(l.attempts, k)
		}
	}
}
