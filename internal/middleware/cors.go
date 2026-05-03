package middleware

import (
	"net/http"
	"strings"
)

// CORS returns a middleware that adds permissive CORS response headers for
// local development. It is activated only when DEV_CORS_ORIGINS is set in the
// environment (see cmd/cf-accounts and cmd/cf-provisioner config).
//
// allowedOrigins is the list of origins to allow (e.g. "http://localhost:8096").
// Pass "*" to allow every origin (useful for quick local testing).
//
// Do NOT include this middleware in production builds — scope allowed origins
// to the exact UI origin and validate credentials separately.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	originSet := make(map[string]struct{}, len(allowedOrigins))
	allowAll := false
	for _, o := range allowedOrigins {
		if o == "*" {
			allowAll = true
			break
		}
		originSet[o] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				if allowAll {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				} else if _, ok := originSet[origin]; ok {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				}

				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", strings.Join([]string{
					"Authorization",
					"Content-Type",
					"X-Request-ID",
					"X-CF-Tenant-ID",
				}, ", "))
				w.Header().Set("Access-Control-Max-Age", "86400")
			}

			// Handle preflight requests — return early with 204.
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
