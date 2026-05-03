// Package middleware provides the standard HTTP middleware chain used by all
// CloudForge service HTTP servers.
//
// # Middleware stack (applied in order)
//
//  1. [RequestID]              — generates / forwards X-Request-ID header
//  2. [OTelSpan]               — starts an OTel span, propagates traceparent
//  3. [StructuredLogger]       — logs request/response with trace correlation
//  4. [PanicRecovery]          — catches panics, returns 500 JSON error
//  5. [metrics.HTTPMiddleware] — records duration/count/size histograms
//  6. [TenantContext]          — extracts {tenant}/{project} from URL path values
//
// # No third-party router required
//
// Since Go 1.22 [net/http.ServeMux] supports method routing and named path
// parameters natively. All CloudForge services use the standard library:
//
//	mux := http.NewServeMux()
//
//	// Wrap each route handler with the full middleware chain.
//	mux.Handle("GET /v1/tenants/{tenant}/projects/{project}/instances",
//	    chain.Apply(http.HandlerFunc(instancesHandler)),
//	)
//
// Or apply individual middlewares:
//
//	h := middleware.RequestID(
//	        middleware.OTelSpan()(
//	            middleware.StructuredLogger(logger)(handler)))
package middleware

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/jtomasevic/cloud-forge/internal/metrics"
)

// Middlewares is an ordered slice of HTTP middleware functions.
// It provides a single Apply method for composing the chain onto a handler,
// replacing the need for a third-party router's middleware stack.
type Middlewares []func(http.Handler) http.Handler

// Apply wraps h with every middleware in the slice. The first element is the
// outermost layer — it is entered first on the way in and exited last on the
// way out, which matches the conventional middleware execution model.
//
// Example:
//
//	chain := Middlewares{A, B, C}
//	chain.Apply(h)
//	// Execution order: A → B → C → h → C → B → A
func (ms Middlewares) Apply(h http.Handler) http.Handler {
	// Iterate in reverse so that the first middleware ends up outermost.
	for i := len(ms) - 1; i >= 0; i-- {
		h = ms[i](h)
	}
	return h
}

// ChainWithCORS is like [Chain] but prepends a [CORS] middleware as the
// outermost layer. Use this in development when DEV_CORS_ORIGINS is set so
// that the browser's preflight OPTIONS request is handled before any other
// middleware runs.
//
// Pass an empty slice (or nil) to skip CORS — equivalent to calling [Chain].
func ChainWithCORS(
	corsOrigins []string,
	logger *slog.Logger,
	registry *prometheus.Registry,
	svcName string,
) Middlewares {
	base := Chain(logger, registry, svcName)
	if len(corsOrigins) == 0 {
		return base
	}
	return append(Middlewares{CORS(corsOrigins)}, base...)
}

// Chain returns a [Middlewares] slice with all standard CloudForge middlewares
// pre-wired in the correct execution order.
//
// Prerequisites:
//   - [tracing.Init] must have been called before Chain is invoked so the
//     globally-registered OTel provider is in place for [OTelSpan].
//
// Parameters:
//   - logger:   structured logger from logging.New(...)
//   - registry: Prometheus registry from metrics.NewRegistry(...)
//   - svcName:  service name used to namespace Prometheus metric labels
func Chain(
	logger *slog.Logger,
	registry *prometheus.Registry,
	svcName string,
) Middlewares {
	return Middlewares{
		// 1. RequestID must be first so every subsequent middleware can read
		//    the correlation ID from context when building log records.
		RequestID,

		// 2. OTelSpan uses the globally-registered tracer provider (set by
		//    tracing.Init) to start a server span and propagate W3C trace context
		//    from the incoming traceparent / tracestate headers.
		OTelSpan(),

		// 3. StructuredLogger enriches the logger with request_id, trace_id,
		//    and span_id, stores it in context, and emits a log record for
		//    each request and its response.
		StructuredLogger(logger),

		// 4. PanicRecovery runs after the logger so that panics are logged with
		//    full per-request context before the 500 response is written.
		PanicRecovery(logger),

		// 5. Prometheus metrics records request duration, count, and response
		//    size histograms for every handled request.
		metrics.HTTPMiddleware(registry, svcName),

		// 6. TenantContext reads the {tenant} and {project} path parameters
		//    using the Go 1.22+ r.PathValue() API and stores them in context.
		//    Apply the chain as the handler argument to mux.Handle() so the
		//    mux has already resolved path values before TenantContext runs:
		//
		//        mux.Handle("GET /v1/tenants/{tenant}/projects/{project}/items",
		//            chain.Apply(http.HandlerFunc(handler)))
		TenantContext,
	}
}

// TenantFromContext retrieves the tenant and project identifiers stored in ctx
// by [TenantContext].
//
// Returns ok=false when the current route does not include {tenant} and
// {project} path parameters (e.g. /healthz, /metrics).
func TenantFromContext(ctx context.Context) (tenant, project string, ok bool) {
	t, tok := ctx.Value(tenantKey{}).(string)
	p, pok := ctx.Value(projectKey{}).(string)
	if !tok || !pok || t == "" || p == "" {
		return "", "", false
	}
	return t, p, true
}
