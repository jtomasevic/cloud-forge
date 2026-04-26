package middleware

import (
	"context"
	"net/http"
)

// tenantKey and projectKey are unexported context key types that prevent
// collisions with context values set by other packages.
type tenantKey struct{}
type projectKey struct{}

// TenantContext is an HTTP middleware that extracts the {tenant} and {project}
// URL path parameters from the request and stores them in context.
//
// # Go 1.22+ path value extraction
//
// CloudForge uses the standard [net/http.ServeMux] with route patterns such as:
//
//	mux.Handle("GET /v1/tenants/{tenant}/projects/{project}/instances", handler)
//
// Since Go 1.22, [http.Request.PathValue] retrieves a named parameter from the
// matched pattern. Unlike chi — where root-level middleware runs before pattern
// matching — the standard mux populates path values BEFORE calling the handler
// chain, so TenantContext works correctly at any middleware nesting level.
//
// When the route does not define {tenant} or {project} (e.g. /healthz, /metrics),
// PathValue returns an empty string and TenantContext is a no-op; calling
// [TenantFromContext] on that request returns ok=false.
func TenantContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// http.Request.PathValue is the idiomatic Go 1.22+ way to read named
		// URL path parameters — no third-party router required.
		tenant := r.PathValue("tenant")
		project := r.PathValue("project")

		ctx := r.Context()

		// Only store non-empty values so that TenantFromContext can distinguish
		// "middleware did not run on this route" from "tenant was genuinely empty"
		// (the latter cannot happen with a properly defined route pattern).
		if tenant != "" {
			ctx = context.WithValue(ctx, tenantKey{}, tenant)
		}
		if project != "" {
			ctx = context.WithValue(ctx, projectKey{}, project)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
