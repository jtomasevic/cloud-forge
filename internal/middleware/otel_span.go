package middleware

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// OTelSpan returns an HTTP middleware that starts an OpenTelemetry span for
// every incoming request and propagates the W3C Trace Context from incoming
// HTTP headers (traceparent / tracestate) into the request context.
//
// The middleware uses the globally-registered OTel tracer provider, which is
// set by [tracing.Init] at service startup. Calling [tracing.Init] before
// registering any routes ensures the correct provider is always in use.
//
// Span naming follows the OTel HTTP semantic conventions:
//   - Span name: "{HTTP method} {chi route pattern}" (e.g. "GET /v1/tenants/{tenant}")
//   - Attributes: http.method, http.route, http.status_code, http.url, etc.
//
// W3C Trace Context (traceparent / tracestate) and Baggage headers from
// upstream services are extracted and stored in the request context so that
// the span is a child of the upstream span when present.
//
// Important: OTelSpan must run before [StructuredLogger] in the middleware
// chain so that trace_id and span_id are present in the context when
// the logger reads them.
func OTelSpan() func(http.Handler) http.Handler {
	// otelhttp.NewMiddleware automatically uses the global TracerProvider and
	// TextMapPropagator registered by tracing.Init, so no configuration is
	// needed here. The operation name is overridden per-request by the
	// otelhttp.WithRouteTag option used in each route group.
	return otelhttp.NewMiddleware("cloudforge")
}
