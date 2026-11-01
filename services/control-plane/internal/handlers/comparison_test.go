package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/axyra/control-plane/internal/config"
)

func TestComparisonDecimalAndRanking(t *testing.T) {
	expected := []comparisonProduct{{"A", 2, "1000.01"}, {"B", 1, "10.00"}}
	for _, tc := range []struct {
		name  string
		rows  []comparisonProduct
		match bool
	}{
		{"decimal formatting", []comparisonProduct{{"A", 2, "1000.010"}, {"B", 1, "10"}}, true},
		{"one cent", []comparisonProduct{{"A", 2, "1000.02"}, {"B", 1, "10"}}, false},
		{"count", []comparisonProduct{{"A", 1, "1000.01"}, {"B", 1, "10"}}, false},
		{"missing", []comparisonProduct{{"A", 2, "1000.01"}}, false},
		{"extra", append(append([]comparisonProduct{}, expected...), comparisonProduct{"C", 1, "1"}), false},
		{"rank", []comparisonProduct{expected[1], expected[0]}, false},
		{"invalid amount", []comparisonProduct{{"A", 2, "NaN"}, expected[1]}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if (len(comparisonDiff(expected, tc.rows)) == 0) != tc.match {
				t.Fatal("incorrect comparison")
			}
		})
	}
}

func TestComparisonTicketIsolation(t *testing.T) {
	h := New(nil, &config.Config{JWTSecret: strings.Repeat("x", 32)})
	ticket := comparisonTicket{Tenant: "tenant_001", Prefix: "cmp_fixture", Expires: time.Now().Add(time.Minute).Unix()}
	signed := h.signComparison(ticket)
	if _, err := h.readComparison(signed, "tenant_001"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.readComparison(signed, "tenant_002"); err == nil {
		t.Fatal("cross-tenant ticket accepted")
	}
	parts := strings.Split(signed, ".")
	if _, err := h.readComparison(parts[0]+"."+strings.Repeat("A", 43), "tenant_001"); err == nil {
		t.Fatal("tampered ticket accepted")
	}
	ticket.Expires = time.Now().Add(-time.Minute).Unix()
	if _, err := h.readComparison(h.signComparison(ticket), "tenant_001"); err == nil {
		t.Fatal("expired ticket accepted")
	}
	if _, err := h.readComparison("malformed", "tenant_001"); err == nil {
		t.Fatal("malformed ticket accepted")
	}
}

func TestComparisonRoutesRequireLogin(t *testing.T) {
	h := New(nil, &config.Config{JWTSecret: strings.Repeat("s", 32), AnalyticsURL: "http://unused"})
	for _, path := range []string{"sources", "snapshot", "simulation"} {
		method := "GET"
		if path == "simulation" {
			method = "POST"
		}
		res := httptest.NewRecorder()
		h.Routes().ServeHTTP(res, httptest.NewRequest(method, "/v1/comparison/"+path, nil))
		if res.Code != 401 {
			t.Fatalf("%s: expected 401, got %d", path, res.Code)
		}
	}
}

func TestComparisonSourceConfiguration(t *testing.T) {
	h := New(nil, &config.Config{TenantCount: 2, ComparisonReferenceDSN: "postgres://reference-db-{index}/db"})
	sources, err := h.comparisonDSNs()
	if err != nil || len(sources) != 2 || sources["tenant_002"] != "postgres://reference-db-2/db" {
		t.Fatalf("unexpected sources: %v %v", sources, err)
	}
	h.cfg.ComparisonSources = `{"custom":"postgres://custom/db"}`
	sources, err = h.comparisonDSNs()
	if err != nil || len(sources) != 1 || sources["custom"] == "" {
		t.Fatal("explicit configuration did not replace demo defaults")
	}
	h.cfg.ComparisonSources = `broken`
	if _, err = h.comparisonDSNs(); err == nil {
		t.Fatal("invalid configuration accepted")
	}
}

func TestComparisonDateRange(t *testing.T) {
	for _, query := range []string{"?from=bad&to=bad", "?from=2026-11-01T00:00:00Z", "?from=2026-11-02T00:00:00Z&to=2026-11-01T00:00:00Z"} {
		if _, _, err := comparisonRange(httptest.NewRequest("GET", "/"+query, nil)); err == nil {
			t.Fatalf("accepted %s", query)
		}
	}
	from, to, err := comparisonRange(httptest.NewRequest("GET", "/?from=2026-11-01T00:00:00Z&to=2026-11-01T00:00:00.999999Z", nil))
	if err != nil || to.Sub(from) != time.Second-time.Microsecond {
		t.Fatalf("range precision lost: %v", err)
	}
}

func TestComparisonRequiresBackendDBTiming(t *testing.T) {
	for _, raw := range []string{"", "NaN", "+Inf", "-1", "invalid"} {
		h := http.Header{}
		h.Set("X-DB-Query-Ms", raw)
		if _, err := comparisonDBDuration(h); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	h := http.Header{}
	h.Set("X-DB-Query-Ms", "0.125")
	ms, err := comparisonDBDuration(h)
	if err != nil || ms != 0.125 {
		t.Fatalf("lost backend precision: %f %v", ms, err)
	}
}
