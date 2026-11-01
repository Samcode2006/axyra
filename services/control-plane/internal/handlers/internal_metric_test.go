package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/axyra/control-plane/internal/config"
)

func TestInternalMetricAuthentication(t *testing.T) {
	secret := strings.Repeat("s", 32)
	h := New(nil, &config.Config{ServiceToken: secret})
	for _, token := range []string{"", "Bearer bad", "Bearer console-jwt"} {
		req := httptest.NewRequest("GET", "/internal/metric-definitions/test?tenant_id=tenant_002", nil)
		req.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		h.Routes().ServeHTTP(w, req)
		if w.Code != 401 {
			t.Fatalf("got %d", w.Code)
		}
	}
	req := httptest.NewRequest("GET", "/v1/metric-definitions/test", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	w := httptest.NewRecorder()
	h.Routes().ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal("service token must not authorize console routes")
	}
}

// Session-local temporary tables isolate fixtures from any existing database.
func TestInternalMetricTenantScope(t *testing.T) {
	dsn := os.Getenv("TEST_METRIC_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_METRIC_DATABASE_URL for PostgreSQL scope test")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	_, err = db.Exec(`CREATE TEMP TABLE tenants(id text, organization_id text);
	 CREATE TEMP TABLE metric_definitions(id text, organization_id text, name text, description text, config jsonb, enabled boolean, created_at timestamptz DEFAULT now(), updated_at timestamptz DEFAULT now());
	 INSERT INTO tenants VALUES ('ta','orga'),('tb','orgb');
	 INSERT INTO metric_definitions(id,organization_id,name,description,config,enabled) VALUES
	 ('shared',NULL,'Builtin','','{}',true),('custom','orga','A','','{}',true),('custom','orgb','B','','{}',true),('private','orga','Private','','{}',true),('disabled','orga','Disabled','','{}',false);`)
	if err != nil {
		t.Fatal(err)
	}
	secret := strings.Repeat("s", 32)
	h := New(db, &config.Config{ServiceToken: secret})
	for _, tc := range []struct {
		metric, tenant, name string
		status               int
	}{
		{"shared", "ta", "Builtin", 200}, {"shared", "tb", "Builtin", 200},
		{"custom", "ta", "A", 200}, {"custom", "tb", "B", 200},
		{"private", "tb", "", 404}, {"shared", "missing", "", 404},
		{"shared", "", "", 400}, {"disabled", "ta", "Disabled", 200},
	} {
		req := httptest.NewRequest("GET", "/internal/metric-definitions/"+tc.metric+"?tenant_id="+tc.tenant+"&organization_id=orga", nil)
		req.Header.Set("Authorization", "Bearer "+secret)
		w := httptest.NewRecorder()
		h.Routes().ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("%+v: got %d %s", tc, w.Code, w.Body)
		}
		if w.Code == 200 {
			var result struct {
				Name    string
				Enabled bool
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Name != tc.name {
				t.Fatalf("wrong organization definition: %s", result.Name)
			}
			if tc.metric == "disabled" && result.Enabled {
				t.Fatal("disabled flag lost")
			}
		}
	}
	for _, tc := range []struct {
		metric, org, name string
		status            int
	}{
		{"custom", "orga", "A", 200}, {"custom", "orgb", "B", 200},
		{"shared", "orgb", "Builtin", 200}, {"private", "orgb", "", 404},
		{"disabled", "orga", "Disabled", 200}, {"shared", "unknown", "", 404},
	} {
		req := httptest.NewRequest("GET", "/internal/metric-definitions/"+tc.metric+"?organization_id=orga", nil)
		req.Header.Set("Authorization", "Bearer "+secret)
		req.Header.Set("X-Analytics-Organization", tc.org)
		w := httptest.NewRecorder()
		h.Routes().ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("%+v: got %d %s", tc, w.Code, w.Body)
		}
		if w.Code == 200 {
			var result struct{ Name string }
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Name != tc.name {
				t.Fatalf("%+v: got definition %s", tc, result.Name)
			}
		}
	}
}
