package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/axyra/analytics/internal/config"
	"github.com/axyra/analytics/internal/db"
	"github.com/axyra/analytics/internal/limiter"
	"github.com/axyra/analytics/internal/queue"
)

type Handler struct {
	db      *db.DB
	queue   queue.Queue
	cfg     *config.Config
	limiter *limiter.Limiter
}

func New(database *db.DB, q queue.Queue, cfg *config.Config) *Handler {
	return &Handler{
		db:      database,
		queue:   q,
		cfg:     cfg,
		limiter: limiter.New(cfg.MaxConcurrentQueries),
	}
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "analytics"})
}

// acquireQuery tries to acquire a concurrency slot for the tenant (B-3) and
// records query latency when the returned done function is called (B-5).
// If the limit is exceeded it writes 429 and returns ok=false.
func (h *Handler) acquireQuery(w http.ResponseWriter, r *http.Request, tenantID, endpoint string) (done func(), ok bool) {
	capacity := h.db.GetTenantMaxConcurrency(tenantID)
	if err := h.limiter.Acquire(tenantID, capacity); err != nil {
		respondError(w, http.StatusTooManyRequests, err.Error())
		return nil, false
	}
	start := time.Now()
	return func() {
		h.limiter.Release(tenantID)
		ms := time.Since(start).Milliseconds()
		h.db.RecordQueryLatency(r.Context(), tenantID, endpoint, ms)
	}, true
}

// resolveRequestTenant extracts and resolves tenant_id from request query/header or default.
func (h *Handler) resolveRequestTenant(r *http.Request) string {
	// 1. Check query param
	t := r.URL.Query().Get("tenant_id")
	if t == "" {
		t = r.URL.Query().Get("external_tenant_id")
	}
	// 2. Check header
	if t == "" {
		t = r.Header.Get("X-Tenant-ID")
	}

	// 3. If provided, check if it's an external tenant ID (e.g. store_001) that needs resolution
	if t != "" {
		var canonicalID string
		err := h.db.QueryRow("SELECT id FROM tenants WHERE id = $1 OR external_tenant_id = $1 LIMIT 1", t).Scan(&canonicalID)
		if err == nil && canonicalID != "" {
			return canonicalID
		}
		return t
	}

	// 4. Fallback to default active tenant
	return h.db.GetDefaultTenant()
}

func extractBearerToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return strings.TrimSpace(parts[1])
		}
		return strings.TrimSpace(authHeader)
	}
	return r.Header.Get("X-Agent-Token")
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

