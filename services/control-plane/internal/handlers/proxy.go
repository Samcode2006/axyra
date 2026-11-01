package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

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
		req.Header.Set("Authorization", "Bearer "+h.cfg.ServiceToken)
	}
	// The control plane's CORS middleware already answers for every route. The
	// analytics service adds its own CORS headers, and a response carrying the
	// header twice is rejected by browsers, so drop the upstream's.
	proxy.ModifyResponse = func(resp *http.Response) error {
		for name := range resp.Header {
			if strings.HasPrefix(http.CanonicalHeaderKey(name), "Access-Control-") {
				resp.Header.Del(name)
			}
		}
		return nil
	}

	return func(w http.ResponseWriter, r *http.Request) {
		p := principal(w, r)
		if p == nil {
			return
		}
		// Never trust a browser-supplied internal organization scope.
		r.Header.Del("X-Analytics-Organization")
		if r.URL.Query().Get("scope") == "organization" {
			if p.IsOperator() || p.OrgID == "" || r.Method != http.MethodGet || !organizationAnalyticsPaths[r.URL.Path] {
				respondError(w, http.StatusForbidden, "Organization overview is only available for admin dashboard reads")
				return
			}
			q := r.URL.Query()
			q.Del("tenant_id")
			q.Del("external_tenant_id")
			q.Del("organization_id")
			r.URL.RawQuery = q.Encode()
			r.Header.Del("X-Tenant-ID")
			r.Header.Set("X-Analytics-Organization", p.OrgID)
		} else if !p.IsOperator() {
			if !h.scopeAnalyticsRequest(w, r, p) {
				return
			}
		}
		// Never forward user credentials. The director supplies service credentials
		// only after authentication and organization/tenant authorization above.
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

// Rebuild, metrics configured by users, and operational/admin endpoints cannot
// opt into this scope. Only the dashboard's read endpoints support aggregation.
var organizationAnalyticsPaths = map[string]bool{
	"/v1/analytics/status":         true,
	"/v1/analytics/orders/summary": true, "/v1/analytics/orders/trend": true,
	"/v1/analytics/orders/revenue": true, "/v1/analytics/orders/breakdown": true,
	"/v1/analytics/products/top": true, "/v1/analytics/customers/summary": true,
	"/v1/analytics/customers/top": true, "/v1/analytics/customers/cohorts": true,
	"/v1/analytics/geo/breakdown": true, "/v1/analytics/channels/breakdown": true,
	"/v1/analytics/payments/breakdown": true, "/v1/analytics/returns/summary": true,
	"/v1/analytics/fulfillment/funnel": true, "/v1/analytics/discounts/summary": true,
}
