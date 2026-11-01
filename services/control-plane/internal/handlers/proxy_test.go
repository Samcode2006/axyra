package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/axyra/control-plane/internal/auth"
	"github.com/axyra/control-plane/internal/config"
)

func TestAnalyticsProxyServiceCredential(t *testing.T) {
	secret := strings.Repeat("s", 32)
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer "+secret {
			t.Error("service credential not supplied")
		}
		w.WriteHeader(204)
	}))
	defer upstream.Close()
	h := New(nil, &config.Config{AnalyticsURL: upstream.URL, ServiceToken: secret})
	req := httptest.NewRequest("GET", "/v1/analytics/orders/summary?tenant_id=tenant_001", nil)
	req.Header.Set("Authorization", "Bearer forged-client-token")
	w := httptest.NewRecorder()
	h.AnalyticsProxy()(w, req)
	if w.Code != 401 || calls != 0 {
		t.Fatal("unauthenticated request reached backend")
	}
	req = req.WithContext(auth.WithPrincipal(req.Context(), &auth.Principal{Role: auth.RoleOperator}))
	w = httptest.NewRecorder()
	h.AnalyticsProxy()(w, req)
	if w.Code != 204 || calls != 1 {
		t.Fatalf("authorized proxy failed: %d, %d", w.Code, calls)
	}
}

func TestOrganizationScopeCannotBeSpoofed(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-Analytics-Organization") != "org_own" {
			t.Error("organization not derived from principal")
		}
		for _, key := range []string{"tenant_id", "external_tenant_id", "organization_id"} {
			if r.URL.Query().Get(key) != "" {
				t.Errorf("forwarded selector %s", key)
			}
		}
		if r.Header.Get("X-Tenant-ID") != "" {
			t.Error("forwarded tenant header")
		}
		w.WriteHeader(204)
	}))
	defer upstream.Close()
	h := New(nil, &config.Config{AnalyticsURL: upstream.URL, ServiceToken: strings.Repeat("x", 32)})
	admin := &auth.Principal{Role: auth.RoleAdmin, OrgID: "org_own"}
	req := httptest.NewRequest("GET", "/v1/analytics/orders/summary?scope=organization&organization_id=other&tenant_id=other&external_tenant_id=other", nil)
	req.Header.Set("X-Analytics-Organization", "org_other")
	req.Header.Set("X-Tenant-ID", "other")
	req = req.WithContext(auth.WithPrincipal(req.Context(), admin))
	out := httptest.NewRecorder()
	h.AnalyticsProxy()(out, req)
	if out.Code != 204 || calls != 1 {
		t.Fatalf("scoped proxy: %d", out.Code)
	}
	for _, path := range []string{"/v1/analytics/rebuild", "/v1/analytics/monitoring", "/v1/analytics/metrics/test/query"} {
		req := httptest.NewRequest("POST", path+"?scope=organization", nil)
		req = req.WithContext(auth.WithPrincipal(req.Context(), admin))
		out := httptest.NewRecorder()
		h.AnalyticsProxy()(out, req)
		if out.Code != 403 {
			t.Fatalf("scope accepted on %s: %d", path, out.Code)
		}
	}
	if calls != 1 {
		t.Fatal("unsupported scope reached analytics")
	}
}
