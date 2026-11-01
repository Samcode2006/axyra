package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/axyra/control-plane/internal/auth"
	"github.com/axyra/control-plane/internal/config"
	"github.com/axyra/control-plane/internal/db"
	"github.com/go-chi/chi/v5"
)

func TestProvisioningAPIIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_PROVISION_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_PROVISION_DATABASE_URL to disposable PostgreSQL")
	}
	database, err := db.Connect(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	org := newID("org_")
	if _, err = database.Exec(`INSERT INTO organizations(id,name) VALUES($1,'Provisioning tests')`, org); err != nil {
		t.Fatal(err)
	}
	defer func() {
		database.Exec(`DELETE FROM tenant_provisioning WHERE tenant_id IN (SELECT id FROM tenants WHERE organization_id=$1)`, org)
		database.Exec(`DELETE FROM organizations WHERE id=$1`, org)
	}()

	h := New(database, &config.Config{ManagedProvisioning: true})
	p := &auth.Principal{UserID: "test_admin", Username: "test", OrgID: org, Role: auth.RoleAdmin}
	call := func(fn http.HandlerFunc, id, body string, who *auth.Principal) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		ctx := auth.WithPrincipal(req.Context(), who)
		route := chi.NewRouteContext()
		route.URLParams.Add("id", id)
		req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, route))
		out := httptest.NewRecorder()
		fn(out, req)
		return out
	}
	create := func(n int) *httptest.ResponseRecorder {
		return call(h.CreateTenant, "", fmt.Sprintf(`{"name":"Managed %d","external_tenant_id":"store_%d","database_mode":"managed"}`, n, n), p)
	}
	out := create(1)
	if out.Code != 202 {
		t.Fatalf("create: %d %s", out.Code, out.Body.String())
	}
	var tenant struct{ ID, Status string }
	if err = json.Unmarshal(out.Body.Bytes(), &tenant); err != nil {
		t.Fatal(err)
	}
	if tenant.Status != "provisioning" {
		t.Fatal(tenant.Status)
	}
	if out = create(1); out.Code != 409 {
		t.Fatalf("duplicate: %d", out.Code)
	}
	out = call(h.ProvisioningStatus, tenant.ID, "", p)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"status":"queued"`) || strings.Contains(out.Body.String(), "encrypted_secrets") {
		t.Fatal(out.Body.String())
	}
	outsider := &auth.Principal{UserID: "other", OrgID: "other", Role: auth.RoleAdmin}
	if out = call(h.ProvisioningStatus, tenant.ID, "", outsider); out.Code != 404 {
		t.Fatalf("cross-org status=%d", out.Code)
	}
	if out = call(h.RetryProvisioning, tenant.ID, "", outsider); out.Code != 404 {
		t.Fatalf("cross-org retry=%d", out.Code)
	}
	if out = call(h.RetryProvisioning, tenant.ID, "", p); out.Code != 409 {
		t.Fatalf("retry running=%d", out.Code)
	}
	if out = call(h.DeleteTenant, tenant.ID, "", p); out.Code != 409 {
		t.Fatalf("delete=%d", out.Code)
	}
	if out = call(h.UpdateTenant, tenant.ID, `{"status":"active"}`, p); out.Code != 409 {
		t.Fatalf("manual activation=%d", out.Code)
	}
	if _, err = database.Exec(`UPDATE tenant_provisioning SET status='failed' WHERE tenant_id=$1`, tenant.ID); err != nil {
		t.Fatal(err)
	}
	if out = call(h.RetryProvisioning, tenant.ID, "", p); out.Code != 202 {
		t.Fatalf("retry failed=%d %s", out.Code, out.Body.String())
	}
	for i := 2; i <= 5; i++ {
		if out = create(i); out.Code != 202 {
			t.Fatalf("create %d: %d", i, out.Code)
		}
	}
	if out = create(6); out.Code != 409 {
		t.Fatalf("quota=%d", out.Code)
	}
	var jobs int
	if err = database.QueryRow(`SELECT COUNT(*) FROM tenant_provisioning j JOIN tenants t ON t.id=j.tenant_id WHERE t.organization_id=$1`, org).Scan(&jobs); err != nil || jobs != 5 {
		t.Fatalf("atomic job count %d %v", jobs, err)
	}
	h.cfg.ManagedProvisioning = false
	if out = create(7); out.Code != 503 {
		t.Fatalf("disabled=%d", out.Code)
	}
	if out = call(h.CreateTenant, "", `{"name":"External","external_tenant_id":"external","database_mode":"existing"}`, p); out.Code != 201 {
		t.Fatalf("external=%d", out.Code)
	}
}
