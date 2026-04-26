// Package metrics provides a Prometheus registry and standard HTTP middleware
// for all CloudForge services.
//
// # Overview
//
// Each service creates one [*prometheus.Registry] per process via [NewRegistry].
// The registry is pre-populated with Go runtime metrics (goroutines, GC, memory)
// so that every service gets baseline observability for free.
//
// The HTTP metrics middleware ([HTTPMiddleware]) records three histograms:
//   - http_server_request_duration_seconds — request latency by method, path, status
//   - http_server_requests_total           — request count by method, path, status
//   - http_server_response_size_bytes      — response body size by method, path, status
//
// The /metrics endpoint is served by [Handler].
//
// # Label cardinality
//
// The "path" label uses the chi route pattern (e.g. /v1/tenants/{tenant}/projects/{project})
// rather than the actual request URL to prevent high-cardinality label explosions.
// Services must wire up [HTTPMiddleware] after the chi router so the pattern
// is available via chi.RouteContext.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
)

// HTTPMetrics groups the Prometheus instruments used by [HTTPMiddleware].
// Exported so that services that build custom middleware can embed the
// same instruments rather than creating duplicates.
type HTTPMetrics struct {
	// RequestDuration records the time from first byte received to last byte
	// written, labelled by HTTP method, route pattern, and status code.
	RequestDuration *prometheus.HistogramVec

	// RequestsTotal counts every handled request.
	RequestsTotal *prometheus.CounterVec

	// ResponseSize records the size in bytes of the response body.
	ResponseSize *prometheus.HistogramVec
}

// defaultDurationBuckets are the histogram bucket boundaries (in seconds)
// for request duration. The boundaries are chosen to give good resolution
// for CloudForge's typical API response latencies (sub-1s) while still
// capturing tail-latency outliers for long-running streaming operations.
var defaultDurationBuckets = []float64{
	0.005, 0.010, 0.025, 0.050, 0.100, 0.250,
	0.500, 1.000, 2.500, 5.000, 10.000,
}

// defaultSizeBuckets are the histogram bucket boundaries (in bytes)
// for response body size.
var defaultSizeBuckets = []float64{
	256, 512, 1_024, 4_096, 16_384, 65_536, 262_144, 1_048_576,
}

// NewRegistry creates and returns a new isolated Prometheus registry
// pre-populated with the standard Go runtime collectors:
//   - go_goroutines, go_threads
//   - go_gc_duration_seconds
//   - go_memstats_* (heap, stack, GC)
//   - process_* (CPU, file descriptors, virtual memory)
//
// Using a non-default registry (instead of prometheus.DefaultRegisterer) is
// intentional: it prevents test pollution and gives each service a clean
// slate without prometheus.MustRegister conflicts.
func NewRegistry(serviceName string) *prometheus.Registry {
	return newRegistry(serviceName)
}

// HTTPMiddleware returns a chi-compatible HTTP middleware that records
// request metrics (duration, count, response size) using instruments
// registered in registry.
//
// The serviceName label is added to all instruments so that metrics from
// multiple services forwarded to the same Prometheus instance can be
// distinguished.
func HTTPMiddleware(registry *prometheus.Registry, serviceName string) func(http.Handler) http.Handler {
	return newHTTPMiddleware(registry, serviceName)
}

// Handler returns an http.Handler that serves the Prometheus /metrics
// endpoint for the given registry.
//
// Mount it at /metrics in your service router:
//
//	mux.Handle("GET /metrics", metrics.Handler(registry))
func Handler(registry *prometheus.Registry) http.Handler {
	return newHandler(registry)
}
