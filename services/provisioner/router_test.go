package provisioner_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"

	. "github.com/jtomasevic/cloud-forge/services/provisioner"
)

// TestNewRouter_HealthzProbe verifies the /healthz endpoint is served without
// going through the business handler.
func TestNewRouter_HealthzProbe(t *testing.T) {
	reg := prometheus.NewRegistry()
	router := NewRouter(NewHandler(&fakeProvisionerService{}), silentProvLog(), reg, "provisioner_test_svc", nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", http.NoBody)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

// TestOpapiErrorHandler_ProvisionerInvalidPathParam verifies that a non-UUID
// job_id triggers the opapiErrorHandler, covering that function.
// The generated wrapper tries to parse job_id as uuid.UUID and calls
// ErrorHandlerFunc on failure, returning 400.
func TestOpapiErrorHandler_ProvisionerInvalidPathParam(t *testing.T) {
	reg := prometheus.NewRegistry()
	router := NewRouter(NewHandler(&fakeProvisionerService{}), silentProvLog(), reg, "provisioner_test_svc", nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/jobs/not-a-uuid", http.NoBody)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// TestNewRouter_OpenAPISpecFound verifies that GET /api/v1/openapi.yaml serves
// the file contents with the correct Content-Type when the spec is present.
func TestNewRouter_OpenAPISpecFound(t *testing.T) {
	dir := t.TempDir()
	specDir := dir + "/api/provisioner/v1"
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(specDir+"/openapi.yaml", []byte("openapi: 3.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	reg := prometheus.NewRegistry()
	router := NewRouter(NewHandler(&fakeProvisionerService{}), silentProvLog(), reg, "provisioner_test_svc", nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/openapi.yaml", http.NoBody)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/yaml", rr.Header().Get("Content-Type"))
}

// TestNewRouter_OpenAPISpecNotFound verifies that GET /api/v1/openapi.yaml
// returns 404 when the spec file is absent (true in CI and unit-test
// environments where the api/ directory is not present).
func TestNewRouter_OpenAPISpecNotFound(t *testing.T) {
	reg := prometheus.NewRegistry()
	router := NewRouter(NewHandler(&fakeProvisionerService{}), silentProvLog(), reg, "provisioner_test_svc", nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/openapi.yaml", http.NoBody)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// TestNewRouter_SwaggerUIPage verifies that GET /api/v1/docs returns an HTML
// page containing the Swagger UI bootstrap code.
func TestNewRouter_SwaggerUIPage(t *testing.T) {
	reg := prometheus.NewRegistry()
	router := NewRouter(NewHandler(&fakeProvisionerService{}), silentProvLog(), reg, "provisioner_test_svc", nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/docs", http.NoBody)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "text/html; charset=utf-8", rr.Header().Get("Content-Type"))
}

// TestNewRouter_CORSHeadersApplied verifies that the CORS middleware is wired
// when corsOrigins is non-empty and the expected header is present.
func TestNewRouter_CORSHeadersApplied(t *testing.T) {
	const origin = "http://localhost:8096"
	reg := prometheus.NewRegistry()
	router := NewRouter(NewHandler(&fakeProvisionerService{}), silentProvLog(), reg, "provisioner_test_svc", []string{origin})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", http.NoBody)
	req.Header.Set("Origin", origin)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, origin, rr.Header().Get("Access-Control-Allow-Origin"))
}

func silentProvLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}
