package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jtomasevic/cloud-forge/internal/metrics"
)

// TestNewRegistry verifies that NewRegistry returns a non-nil registry and
// that the Go runtime collectors are pre-registered.
func TestNewRegistry(t *testing.T) {
	reg := metrics.NewRegistry("test-svc")
	require.NotNil(t, reg)

	// The registry must contain at least the Go runtime metrics.
	// We verify this by fetching the /metrics page and checking for
	// a known go_goroutines metric.
	handler := metrics.Handler(reg)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", http.NoBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, body, "go_goroutines",
		"registry must include Go runtime goroutine gauge")
}

// TestHandler_ContentType verifies that the /metrics endpoint returns the
// correct content type (text/plain with version and encoding headers).
func TestHandler_ContentType(t *testing.T) {
	reg := metrics.NewRegistry("test-svc")
	handler := metrics.Handler(reg)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", http.NoBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	contentType := w.Header().Get("Content-Type")
	assert.True(t,
		strings.HasPrefix(contentType, "text/plain") ||
			strings.HasPrefix(contentType, "application/openmetrics-text"),
		"unexpected Content-Type: %s", contentType,
	)
}

// TestHTTPMiddleware_RecordsMetrics verifies that the middleware records
// a request_duration_seconds observation for a handled request.
func TestHTTPMiddleware_RecordsMetrics(t *testing.T) {
	reg := metrics.NewRegistry("test-svc")
	mw := metrics.HTTPMiddleware(reg, "test_svc")

	// Build a minimal handler that returns 201 Created.
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/resource", http.NoBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	// Verify that the histogram was recorded by checking the /metrics output.
	metricsHandler := metrics.Handler(reg)
	metricsReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", http.NoBody)
	metricsW := httptest.NewRecorder()
	metricsHandler.ServeHTTP(metricsW, metricsReq)

	body := metricsW.Body.String()
	assert.Contains(t, body, "http_server_request_duration_seconds",
		"middleware must emit request duration histogram")
	assert.Contains(t, body, "http_server_requests_total",
		"middleware must emit request counter")
}

// TestHTTPMiddleware_DefaultStatus verifies that when the handler does not
// call WriteHeader explicitly, the status defaults to 200.
func TestHTTPMiddleware_DefaultStatus(t *testing.T) {
	reg := metrics.NewRegistry("test-svc-2")
	mw := metrics.HTTPMiddleware(reg, "test_svc_2")

	// Handler writes a body without calling WriteHeader → implicit 200.
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", http.NoBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// The recorder captures the implicit 200.
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestWithRoutePattern verifies that WithRoutePattern stores a low-cardinality
// route pattern in the request context so the metrics middleware can read it
// instead of using the raw URL path.
//
// WithRoutePattern must wrap the metrics middleware (not the other way around)
// because it creates a new *http.Request with the enriched context; the outer
// middleware only sees that new request if WithRoutePattern runs first.
func TestWithRoutePattern(t *testing.T) {
	reg := metrics.NewRegistry("test-svc-pattern")
	mw := metrics.HTTPMiddleware(reg, "test_pattern")

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Correct usage: WithRoutePattern is outermost, metrics middleware is inside.
	// Execution order: WithRoutePattern → mw (sets path label) → inner handler.
	handler := metrics.WithRoutePattern("/v1/things/{id}", mw(inner))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/things/123", http.NoBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// The path label in the emitted metrics must be the LOW-CARDINALITY pattern,
	// not the actual high-cardinality URL ("/v1/things/123").
	metricsHandler := metrics.Handler(reg)
	metricsReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", http.NoBody)
	metricsW := httptest.NewRecorder()
	metricsHandler.ServeHTTP(metricsW, metricsReq)

	body := metricsW.Body.String()
	assert.Contains(t, body, `path="/v1/things/{id}"`,
		"WithRoutePattern must inject the pattern as the path label (low cardinality)")
	assert.NotContains(t, body, `path="/v1/things/123"`,
		"high-cardinality actual path must NOT appear as a label when pattern is set")
}

// TestWithRoutePattern_FallsBackToURLPath verifies that when WithRoutePattern
// is NOT used, the middleware falls back to the raw URL path for labelling.
func TestWithRoutePattern_FallsBackToURLPath(t *testing.T) {
	reg := metrics.NewRegistry("test-svc-fallback")
	mw := metrics.HTTPMiddleware(reg, "test_fallback")

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/v1/things/456", http.NoBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)

	// Metrics must still be recorded even without an explicit route pattern.
	metricsHandler := metrics.Handler(reg)
	metricsReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", http.NoBody)
	metricsW := httptest.NewRecorder()
	metricsHandler.ServeHTTP(metricsW, metricsReq)
	body := metricsW.Body.String()
	assert.Contains(t, body, "http_server_request_duration_seconds")
}
