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
