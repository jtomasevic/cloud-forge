package provisioner_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"

	. "github.com/jtomasevic/cloud-forge/services/provisioner"
)

// TestNewRouter_HealthzProbe verifies the /healthz endpoint is served without
// going through the business handler.
func TestNewRouter_HealthzProbe(t *testing.T) {
	reg := prometheus.NewRegistry()
	router := NewRouter(NewHandler(&fakeProvisionerService{}), silentProvLog(), reg, "provisioner_test_svc")

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
	router := NewRouter(NewHandler(&fakeProvisionerService{}), silentProvLog(), reg, "provisioner_test_svc")

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/jobs/not-a-uuid", http.NoBody)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func silentProvLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}
