package metrics

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// newHTTPMiddleware is the internal implementation of [HTTPMiddleware].
// It creates the three HTTP instruments (duration, count, size), registers
// them in the provided registry, and returns a middleware function.
func newHTTPMiddleware(registry *prometheus.Registry, serviceName string) func(http.Handler) http.Handler {
	// ── Request duration histogram ────────────────────────────────────────
	// Labels: method (GET/POST/…), path (URL path), status (200/404/…)
	//
	// With the standard library's http.ServeMux (Go 1.22+) the matched route
	// pattern is not directly exposed to middleware through the request context.
	// We therefore use r.URL.Path as the path label. High-cardinality paths
	// (e.g. /v1/tenants/acme/projects/p1/instances/i-123) are an accepted
	// trade-off in this package; services that want pattern-level labels can
	// use the optional [PatternMiddleware] wrapper documented below.
	duration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: serviceName,
			Subsystem: "http_server",
			Name:      "request_duration_seconds",
			Help:      "Histogram of HTTP request latency in seconds, labelled by method, path, and status code.",
			Buckets:   defaultDurationBuckets,
		},
		[]string{"method", "path", "status"},
	)

	// ── Request counter ────────────────────────────────────────────────────
	total := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: serviceName,
			Subsystem: "http_server",
			Name:      "requests_total",
			Help:      "Total number of HTTP requests handled, labelled by method, path, and status code.",
		},
		[]string{"method", "path", "status"},
	)

	// ── Response size histogram ────────────────────────────────────────────
	size := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: serviceName,
			Subsystem: "http_server",
			Name:      "response_size_bytes",
			Help:      "Histogram of HTTP response body sizes in bytes, labelled by method, path, and status code.",
			Buckets:   defaultSizeBuckets,
		},
		[]string{"method", "path", "status"},
	)

	// MustRegister panics on a duplicate registration, which is intentional:
	// registering the same instruments twice is a programming error.
	registry.MustRegister(duration, total, size)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Wrap the ResponseWriter to capture status code and byte count
			// after the downstream handler has finished writing.
			rw := newResponseRecorder(w)

			next.ServeHTTP(rw, r)

			// Use the route pattern stored in context when available (set by
			// WithRoutePattern further down the chain), otherwise fall back to
			// the raw URL path. This keeps label cardinality low on services
			// that opt in to pattern-level labelling.
			path := routePatternFromContext(r)

			statusStr := strconv.Itoa(rw.statusCode)
			elapsed := time.Since(start).Seconds()

			duration.WithLabelValues(r.Method, path, statusStr).Observe(elapsed)
			total.WithLabelValues(r.Method, path, statusStr).Inc()
			size.WithLabelValues(r.Method, path, statusStr).Observe(float64(rw.bytesWritten))
		})
	}
}

// routePatternKey is the context key used by [WithRoutePattern] to store the
// matched route pattern for use by the metrics middleware.
type routePatternKey struct{}

// WithRoutePattern returns an http.Handler that stores the given pattern in
// the request context before calling next. Mount this thin wrapper at each
// route registration site to enable low-cardinality metric labels:
//
//	const pattern = "/v1/tenants/{tenant}/projects/{project}/instances"
//	mux.Handle("GET "+pattern,
//	    metrics.WithRoutePattern(pattern,
//	        chain.Apply(http.HandlerFunc(handler))))
//
// When this wrapper is not used, the metrics middleware falls back to r.URL.Path.
func WithRoutePattern(pattern string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Store the pattern only if one has not already been set; the innermost
		// (most specific) pattern wins over an outer generic wrapper.
		if r.Context().Value(routePatternKey{}) == nil {
			ctx := context.WithValue(r.Context(), routePatternKey{}, pattern)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

// routePatternFromContext reads the route pattern stored by [WithRoutePattern],
// falling back to r.URL.Path when no pattern has been set.
func routePatternFromContext(r *http.Request) string {
	if p, ok := r.Context().Value(routePatternKey{}).(string); ok && p != "" {
		return p
	}
	// Fall back to the raw URL path. Services that do not use WithRoutePattern
	// will see per-path cardinality in their metrics. This is acceptable for
	// internal services with a small, known set of URL shapes.
	return r.URL.Path
}

// responseRecorder wraps http.ResponseWriter to capture status code and body
// size for the metrics middleware. It is also used by the middleware package's
// StructuredLogger to capture response details for logging.
type responseRecorder struct {
	http.ResponseWriter

	// statusCode is set by WriteHeader; defaults to 200 to match stdlib behaviour.
	statusCode int

	// bytesWritten accumulates the total bytes written across all Write calls.
	bytesWritten int
}

// newResponseRecorder wraps w with a default status of 200 OK.
func newResponseRecorder(w http.ResponseWriter) *responseRecorder {
	return &responseRecorder{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
}

// WriteHeader captures the status code before forwarding to the real writer.
func (r *responseRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

// Write accumulates the byte count and forwards to the real writer.
func (r *responseRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytesWritten += n
	return n, err
}
