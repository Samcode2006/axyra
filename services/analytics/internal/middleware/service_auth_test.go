package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServiceAuth(t *testing.T) {
	secret := strings.Repeat("s", 32)
	for _, test := range []struct {
		name, configured, header string
		want                     int
	}{
		{"valid", secret, "Bearer " + secret, 204},
		{"wrong", secret, "Bearer " + strings.Repeat("x", 32), 401},
		{"empty config", "", "Bearer ", 401},
		{"missing", secret, "", 401},
		{"wrong scheme", secret, "Basic " + secret, 401},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := ServiceAuth(test.configured)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("Authorization", test.header)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != test.want {
				t.Fatalf("got %d want %d", w.Code, test.want)
			}
		})
	}
}
