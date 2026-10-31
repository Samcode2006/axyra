package middleware

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/axyra/control-plane/internal/auth"
)

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": message}})
}

// Authenticate requires a valid session JWT and stores the caller in the
// request context.
func Authenticate(secret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			header := r.Header.Get("Authorization")
			scheme, raw, ok := strings.Cut(header, " ")
			if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(raw) == "" {
				writeError(w, http.StatusUnauthorized, "Missing or malformed Authorization header")
				return
			}
			p, err := auth.ParseJWT(secret, strings.TrimSpace(raw))
			if err != nil {
				writeError(w, http.StatusUnauthorized, "Invalid or expired token")
				return
			}
			next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), p)))
		})
	}
}
