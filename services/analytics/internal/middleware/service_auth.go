package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// ServiceAuth authenticates the control plane, which enforces user/tenant scope
// before proxying. Agent tokens and console JWTs cannot authorize query access.
func ServiceAuth(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scheme, provided, ok := strings.Cut(r.Header.Get("Authorization"), " ")
			if len(token) < 32 || len(r.Header.Values("Authorization")) != 1 || !ok ||
				!strings.EqualFold(scheme, "Bearer") || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"message":"Service authentication required"}}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
