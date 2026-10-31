package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/axyra/control-plane/internal/auth"
)

// AnalyticsProxy forwards dashboard analytics requests to the analytics service.
//
// The analytics service trusts whatever tenant it is told to query, so tenant
// scope is enforced here, before forwarding. A customer admin's request is
// rewritten to carry exactly one canonical tenant id that belongs to their
// organization; anything they supplied that points elsewhere is a 404. Operators
// pass through unchanged.
func (h *Handler) AnalyticsProxy() http.HandlerFunc {
	targetURL, err := url.Parse(h.cfg.AnalyticsURL)
	if err != nil {
		panic(err)
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = targetURL.Host
	}

	return func(w http.ResponseWriter, r *http.Request) {
		p := principal(w, r)
		if p == nil {
			return
		}
		if !p.IsOperator() {
			if !h.scopeAnalyticsRequest(w, r, p) {
				return
			}
		}
		// The console JWT is for this service only; the analytics service must not receive it.
		r.Header.Del("Authorization")
		proxy.ServeHTTP(w, r)
	}
}

// scopeAnalyticsRequest rewrites r so it addresses a single tenant of the
// caller's organization. It writes the response and returns false on failure.
func (h *Handler) scopeAnalyticsRequest(w http.ResponseWriter, r *http.Request, p *auth.Principal) bool {
	q := r.URL.Query()

	// Analytics reads these in this order: tenant_id, external_tenant_id, X-Tenant-ID.
	requested := q.Get("tenant_id")
	if requested == "" {
		requested = q.Get("external_tenant_id")
	}
	if requested == "" {
		requested = r.Header.Get("X-Tenant-ID")
	}

	var tenantID string
	var err error
	if requested != "" {
		// External ids are only unique per organization, so match inside the caller's.
		err = h.db.QueryRowContext(r.Context(), `
			SELECT id FROM tenants
			WHERE organization_id = $1 AND (id = $2 OR external_tenant_id = $2)
			ORDER BY (id = $2) DESC LIMIT 1`, p.OrgID, requested).Scan(&tenantID)
	} else {
		// No tenant named: default to the organization's oldest active tenant,
		// never the platform-wide default the analytics service would pick.
		err = h.db.QueryRowContext(r.Context(), `
			SELECT id FROM tenants WHERE organization_id = $1 AND status = 'active'
			ORDER BY created_at ASC, id ASC LIMIT 1`, p.OrgID).Scan(&tenantID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		notFound(w, "Tenant")
		return false
	}
	if err != nil {
		serverError(w, r, err)
		return false
	}

	q.Set("tenant_id", tenantID)
	q.Del("external_tenant_id")
	r.URL.RawQuery = q.Encode()
	r.Header.Del("X-Tenant-ID")
	return true
}
