package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/jtomasevic/cloud-forge/internal/logging"
)

// StructuredLogger returns an HTTP middleware that logs each request and
// its response using the provided structured logger.
//
// Every log record carries:
//   - method     — HTTP verb (GET, POST, …)
//   - path       — request URL path
//   - status     — HTTP response status code
//   - latency_ms — elapsed time in milliseconds from request start to last byte written
//   - request_id — from the X-Request-ID context (set by [RequestID] middleware)
//   - trace_id   — OTel trace ID, if a span is active
//   - span_id    — OTel span ID, if a span is active
//
// The enriched logger is also stored back into the request context via
// [logging.WithContext] so that handlers can retrieve it and emit
// request-correlated log records without carrying the logger through
// every function parameter.
//
// Important: [RequestID] and [OTelSpan] must run before this middleware in
// the chain so that request_id and trace IDs are available at log time.
func StructuredLogger(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// ── Build per-request logger ──────────────────────────────────
			// Enrich the base logger with request-specific fields. This
			// creates a new *slog.Logger that inherits all attributes from
			// base but adds the per-request fields on top.
			reqID := RequestIDFromContext(r.Context())

			// Extract OTel trace and span IDs if a span is active. These
			// are zero-value strings if no span has been started yet — in
			// that case they are omitted from the log record via the empty
			// check below.
			spanCtx := trace.SpanFromContext(r.Context()).SpanContext()
			traceID := spanCtx.TraceID().String()
			spanID := spanCtx.SpanID().String()

			// Build the enriched logger.
			attrs := []any{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("request_id", reqID),
			}
			if spanCtx.IsValid() {
				// Only add trace/span IDs when a span is actually active;
				// adding zero-value IDs would pollute logs with "0000…".
				attrs = append(attrs,
					slog.String("trace_id", traceID),
					slog.String("span_id", spanID),
				)
			}

			reqLogger := base.With(attrs...)

			// Store the enriched logger in the context so downstream handlers
			// can call logging.FromContext(ctx) to get a correlated logger.
			ctx := logging.WithContext(r.Context(), reqLogger)

			// Wrap the ResponseWriter to capture status and bytes written.
			rw := newResponseWriter(w)

			reqLogger.Info("request started")

			// ── Delegate to next handler ──────────────────────────────────
			next.ServeHTTP(rw, r.WithContext(ctx))

			// ── Log the completed request ─────────────────────────────────
			elapsed := time.Since(start)
			reqLogger.Info("request completed",
				slog.Int("status", rw.Status()),
				slog.Int64("latency_ms", elapsed.Milliseconds()),
				slog.Int("bytes", rw.BytesWritten()),
			)
		})
	}
}
