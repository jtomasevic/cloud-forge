// Whitebox tests for the storage service router wire-up.
// They live in package storage (not storage_test) so that the unexported
// requestErrorHandler helper can be exercised directly.
package storage

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jtomasevic/cloud-forge/internal/metrics"
)

// newTestRouter is a convenience helper that builds a router wired to the
// placeholder handler with an isolated Prometheus registry so test runs never
// collide on metric names.
func newTestRouter(t *testing.T, registrySuffix string) http.Handler {
	t.Helper()

	// Each test gets its own registry to avoid "duplicate metrics" panics when
	// tests run in parallel and share the default prometheus.DefaultRegisterer.
	reg := prometheus.NewPedanticRegistry()
	// Register the default Go and process collectors that metrics.NewRegistry
	// normally adds; here we use a plain pedantic registry for isolation.
	_ = reg

	cfReg := metrics.NewRegistry(registrySuffix)
	handler := NewRouter(NewHandler(), slog.Default(), cfReg, registrySuffix)
	require.NotNil(t, handler)
	return handler
}

// TestNewRouter_ReturnsNonNilHandler verifies that NewRouter returns a usable
// http.Handler and does not panic during construction.
func TestNewRouter_ReturnsNonNilHandler(t *testing.T) {
	t.Parallel()

	h := newTestRouter(t, "test_new_router")
	assert.NotNil(t, h)
}

// TestNewRouter_ListBucketsRoute exercises the GET /buckets route.
// The placeholder handler returns 500, but the route itself must be found and
// the response must carry the JSON Content-Type set by cferrors.WriteJSON.
func TestNewRouter_ListBucketsRoute(t *testing.T) {
	t.Parallel()

	h := newTestRouter(t, "test_list_buckets_route")

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/acme/demo/buckets",
		http.NoBody,
	)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	// Route is found; placeholder returns 500.
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
}

// TestNewRouter_CreateBucketRoute exercises the POST /buckets route with a
// valid JSON body so the strict handler passes request parsing.
func TestNewRouter_CreateBucketRoute(t *testing.T) {
	t.Parallel()

	h := newTestRouter(t, "test_create_bucket_route")

	body := strings.NewReader(`{"name":"my-bucket"}`)
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/acme/demo/buckets",
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	// Route is found; placeholder returns 500.
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestNewRouter_GetBucketRoute exercises the GET /buckets/{name} route.
func TestNewRouter_GetBucketRoute(t *testing.T) {
	t.Parallel()

	h := newTestRouter(t, "test_get_bucket_route")

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/acme/demo/buckets/alpha",
		http.NoBody,
	)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestNewRouter_DeleteBucketRoute exercises the DELETE /buckets/{name} route.
func TestNewRouter_DeleteBucketRoute(t *testing.T) {
	t.Parallel()

	h := newTestRouter(t, "test_delete_bucket_route")

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodDelete,
		"/acme/demo/buckets/alpha",
		http.NoBody,
	)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestNotImplemented_WritesNotImplementedJSON calls the unexported
// notImplemented helper directly and verifies it writes a 501 JSON response
// using the platform error shape. The helper is kept for future direct-HTTP
// endpoints that bypass the strict-server interface.
func TestNotImplemented_WritesNotImplementedJSON(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", http.NoBody)

	notImplemented(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, w.Body.String(), "NOT_IMPLEMENTED")
}

// TestRequestErrorHandler_WritesBadRequestJSON calls the unexported
// requestErrorHandler directly and verifies it writes a 400 JSON response
// using the platform error shape.
func TestRequestErrorHandler_WritesBadRequestJSON(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", http.NoBody)

	requestErrorHandler(w, req, errors.New("required field 'name' is missing"))

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, w.Body.String(), "BAD_REQUEST")
}
