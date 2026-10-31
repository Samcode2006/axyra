// Package auth holds the control plane's identity primitives: console users,
// password hashing, session JWTs and agent token generation/hashing.
package auth

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Role string

const (
	// RoleAdmin is a customer administrator bound to exactly one organization.
	RoleAdmin Role = "admin"
	// RoleOperator is an Axyra platform operator with platform-wide access.
	RoleOperator Role = "operator"
)

const (
	jwtIssuer       = "axyra-control-plane"
	passwordIters   = 600_000
	agentTokenBytes = 24
	// AgentTokenPrefix is the fixed, non-secret start of every agent token.
	AgentTokenPrefix = "axyra_agt_"
	// storedPrefixLen is how much of a token is kept in clear for identification
	// in the console: the fixed prefix plus 8 random hex characters.
	storedPrefixLen = len(AgentTokenPrefix) + 8
)

// Principal is the authenticated caller.
type Principal struct {
	UserID   string
	Username string
	OrgID    string // empty for operators
	Role     Role
}

func (p *Principal) IsOperator() bool { return p.Role == RoleOperator }

// OrgScope returns the organization a query must be restricted to, or nil when
// the caller may see every organization.
func (p *Principal) OrgScope() *string {
	if p.IsOperator() {
		return nil
	}
	org := p.OrgID
	return &org
}

// CanAccessOrg reports whether the caller may act on resources of orgID.
func (p *Principal) CanAccessOrg(orgID string) bool {
	if p.IsOperator() {
		return true
	}
	return orgID != "" && orgID == p.OrgID
}

type ctxKey struct{}

func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// FromContext returns the authenticated principal, or nil if the request was
// not authenticated.
func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(ctxKey{}).(*Principal)
	return p
}

// ─── Passwords ───────────────────────────────────────────────────────────────

// HashPassword returns "pbkdf2-sha256$<iterations>$<salt>$<hash>".
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIters, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", passwordIters,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword checks password against a value produced by HashPassword.
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iters, err := strconv.Atoi(parts[1])
	if err != nil || iters < 1 || iters > 10_000_000 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(want) == 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iters, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// RandomPassword returns a 24-character URL-safe random password.
func RandomPassword() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// DummyVerify burns the same CPU as a real verification. Call it when the user
// does not exist so response time does not reveal valid usernames.
func DummyVerify(password string) {
	_, _ = pbkdf2.Key(sha256.New, password, make([]byte, 16), passwordIters, 32)
}

// ─── Session JWTs ────────────────────────────────────────────────────────────

type claims struct {
	Username string `json:"username"`
	OrgID    string `json:"org_id,omitempty"`
	Role     Role   `json:"role"`
	jwt.RegisteredClaims
}

func IssueJWT(secret []byte, p Principal, ttl time.Duration, now time.Time) (string, error) {
	c := claims{
		Username: p.Username,
		OrgID:    p.OrgID,
		Role:     p.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   p.UserID,
			Issuer:    jwtIssuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(secret)
}

var ErrInvalidToken = errors.New("invalid or expired token")

// ParseJWT validates signature (HS256 only), issuer and expiry and returns the
// caller. Admin tokens without an organization are rejected.
func ParseJWT(secret []byte, raw string) (*Principal, error) {
	var c claims
	tok, err := jwt.ParseWithClaims(raw, &c,
		func(*jwt.Token) (any, error) { return secret, nil },
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer(jwtIssuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !tok.Valid {
		return nil, ErrInvalidToken
	}
	if c.Subject == "" {
		return nil, ErrInvalidToken
	}
	switch c.Role {
	case RoleOperator:
	case RoleAdmin:
		if c.OrgID == "" {
			return nil, ErrInvalidToken
		}
	default:
		return nil, ErrInvalidToken
	}
	return &Principal{UserID: c.Subject, Username: c.Username, OrgID: c.OrgID, Role: c.Role}, nil
}

// ─── Agent tokens ────────────────────────────────────────────────────────────

// NewAgentToken generates a token. Only the hash and the short prefix are ever
// persisted; the plaintext is returned to the caller exactly once.
func NewAgentToken() (plaintext, prefix, hash string, err error) {
	b := make([]byte, agentTokenBytes)
	if _, err = rand.Read(b); err != nil {
		return "", "", "", err
	}
	plaintext = AgentTokenPrefix + hex.EncodeToString(b)
	return plaintext, plaintext[:storedPrefixLen], HashAgentToken(plaintext), nil
}

// HashAgentToken is the lookup key for a token. SHA-256 (not a slow KDF) is
// appropriate because tokens are 192 bits of random data, not human-chosen.
// The analytics service uses the same function to validate incoming tokens.
func HashAgentToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// TokenPrefix returns the identifying prefix for an arbitrary token string.
func TokenPrefix(token string) string {
	if len(token) <= storedPrefixLen {
		return token
	}
	return token[:storedPrefixLen]
}
