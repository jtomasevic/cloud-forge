package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jtomasevic/cloud-forge/internal/middleware"
)

// okHandler is a trivial next handler that records it was called.
func okHandler(called *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*called = true
		w.WriteHeader(http.StatusOK)
	})
}

// TestCORS_NoOriginHeader verifies that when no Origin header is present the
// middleware adds no CORS headers and passes the request to the next handler.
func TestCORS_NoOriginHeader(t *testing.T) {
	called := false
	h := middleware.CORS([]string{"http://localhost:8096"})(okHandler(&called))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.True(t, called, "next handler must be called")
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Methods"))
}

// TestCORS_AllowedOrigin verifies that a request from a known origin receives
// the correct CORS response headers.
func TestCORS_AllowedOrigin(t *testing.T) {
	const origin = "http://localhost:8096"
	called := false
	h := middleware.CORS([]string{origin, "http://localhost:3000"})(okHandler(&called))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api", http.NoBody)
	req.Header.Set("Origin", origin)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.True(t, called)
	assert.Equal(t, origin, w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "GET, POST, PUT, DELETE, OPTIONS", w.Header().Get("Access-Control-Allow-Methods"))
	assert.Contains(t, w.Header().Get("Access-Control-Allow-Headers"), "Authorization")
	assert.Equal(t, "86400", w.Header().Get("Access-Control-Max-Age"))
}

// TestCORS_DisallowedOrigin verifies that a request from an origin not in the
// allow-list does not receive an Access-Control-Allow-Origin header, but the
// other CORS headers are still set (allowing the browser to see the rejection).
func TestCORS_DisallowedOrigin(t *testing.T) {
	called := false
	h := middleware.CORS([]string{"http://localhost:8096"})(okHandler(&called))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api", http.NoBody)
	req.Header.Set("Origin", "http://evil.example.com")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.True(t, called)
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "GET, POST, PUT, DELETE, OPTIONS", w.Header().Get("Access-Control-Allow-Methods"))
}

// TestCORS_WildcardAllowsAnyOrigin verifies that passing "*" in the origins
// list causes all origins to receive the Allow-Origin header.
func TestCORS_WildcardAllowsAnyOrigin(t *testing.T) {
	called := false
	h := middleware.CORS([]string{"*"})(okHandler(&called))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api", http.NoBody)
	req.Header.Set("Origin", "http://any.domain.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.True(t, called)
	assert.Equal(t, "http://any.domain.example", w.Header().Get("Access-Control-Allow-Origin"))
}

// TestCORS_PreflightReturns204 verifies that an OPTIONS preflight request
// receives a 204 No Content response and does NOT call the next handler.
func TestCORS_PreflightReturns204(t *testing.T) {
	called := false
	h := middleware.CORS([]string{"http://localhost:8096"})(okHandler(&called))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodOptions, "/api/v1/resource", http.NoBody)
	req.Header.Set("Origin", "http://localhost:8096")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
	assert.False(t, called, "next handler must NOT be called for preflight")
	assert.Equal(t, "http://localhost:8096", w.Header().Get("Access-Control-Allow-Origin"))
}

// TestCORS_PreflightWithoutOriginReturns204 verifies that an OPTIONS request
// without an Origin header still returns 204 (preflight gate) even though no
// CORS headers are added.
func TestCORS_PreflightWithoutOriginReturns204(t *testing.T) {
	called := false
	h := middleware.CORS([]string{"http://localhost:8096"})(okHandler(&called))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodOptions, "/api/v1/resource", http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.False(t, called)
}
