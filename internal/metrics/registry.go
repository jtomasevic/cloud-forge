package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// newRegistry is the internal implementation of [NewRegistry].
func newRegistry(serviceName string) *prometheus.Registry {
	reg := prometheus.NewRegistry()

	// ── Go runtime collectors ────────────────────────────────────────────
	// GoCollector records goroutine count, GC statistics, and memory metrics.
	// ProcessCollector records CPU, file descriptor, and virtual memory stats.
	// Both are safe to register on a per-service basis because each service
	// gets its own isolated registry.
	reg.MustRegister(
		collectors.NewGoCollector(
			// Collect all Go runtime memory statistics by default.
			collectors.WithGoCollectorRuntimeMetrics(
				collectors.GoRuntimeMetricsRule{Matcher: collectors.MetricsAll.Matcher},
			),
		),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{
			// Namespace the process metrics by service name so that
			// multi-service Prometheus scrape configs can tell them apart.
			Namespace: serviceName,
		}),
	)

	return reg
}

// newHandler wraps the given registry in a promhttp.Handler and returns it
// as a plain http.Handler suitable for mounting at /metrics.
//
// The handler is configured with sensible defaults:
//   - EnableOpenMetrics: true  — allows Prometheus to negotiate the OpenMetrics format
//   - Timeout: none           — streaming is not used, so no special timeout needed
func newHandler(registry *prometheus.Registry) http.Handler {
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		// Expose help text and TYPE lines so the Prometheus UI and Grafana
		// dashboards can auto-populate metric descriptions.
		EnableOpenMetrics: true,
	})
}
