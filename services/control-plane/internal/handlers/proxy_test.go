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
		w.Header().Set("X-DB-Query-Ms", "12.345")
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
	if w.Header().Get("X-DB-Query-Ms") != "12.345" {
		t.Fatal("database timing was not forwarded unchanged")
	}
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

func TestConfiguredMetricOrganizationScope(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-Analytics-Organization") != "org_own" || r.URL.Query().Get("tenant_id") != "" || r.URL.Query().Get("organization_id") != "" {
			t.Errorf("untrusted scope forwarded: %s %v", r.URL, r.Header)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	h := New(nil, &config.Config{AnalyticsURL: upstream.URL, ServiceToken: strings.Repeat("s", 32)})
	for _, tc := range []struct {
		path, method string
		role         auth.Role
		want         int
	}{
		{"custom_revenue", "GET", auth.RoleAdmin, 204},
		{"custom_revenue/query", "GET", auth.RoleAdmin, 403},
		{"", "GET", auth.RoleAdmin, 403},
		{"custom_revenue", "POST", auth.RoleAdmin, 403},
		{"custom_revenue", "GET", auth.RoleOperator, 403},
	} {
		r := httptest.NewRequest(tc.method, "/v1/analytics/metrics/"+tc.path+"?scope=organization&organization_id=other&tenant_id=other", nil)
		r.Header.Set("X-Analytics-Organization", "other")
		r = r.WithContext(auth.WithPrincipal(r.Context(), &auth.Principal{Role: tc.role, OrgID: "org_own"}))
		w := httptest.NewRecorder()
		h.AnalyticsProxy()(w, r)
		if w.Code != tc.want {
			t.Fatalf("%+v: got %d", tc, w.Code)
		}
	}
	if calls != 1 {
		t.Fatalf("unexpected forwarded calls: %d", calls)
	}
}
