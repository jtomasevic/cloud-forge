//go:build integration

package testutil

import (
	"context"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// opaImage is the Open Policy Agent (OPA) server image.
const opaImage = "openpolicyagent/opa:latest"

// StartOPA starts an OPA server container in REST API mode and returns the
// base URL of the OPA API (e.g. "http://localhost:32768") along with a
// cleanup function.
//
// Callers can interact with OPA by sending HTTP requests to the returned
// base URL. Common endpoints:
//   - POST {baseURL}/v1/data/{path}           — evaluate a policy
//   - PUT  {baseURL}/v1/policies/{policyID}   — upload a Rego policy
//   - GET  {baseURL}/v1/health                — readiness check
//
// There is no official testcontainers-go module for OPA, so we use the
// generic container API. No auth is configured — the server runs in
// anonymous mode suitable for testing.
//
// The cleanup function terminates the container and is also registered with
// t.Cleanup.
func StartOPA(t *testing.T) (baseURL string, cleanup func()) {
	t.Helper()

	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        opaImage,
		ExposedPorts: []string{"8181/tcp"},
		// Start OPA in run mode with the REST API enabled and listening on
		// all interfaces so Docker's port mapping works correctly.
		Cmd: []string{
			"run",
			"--server",
			"--addr", "0.0.0.0:8181",
			// Enable bundle polling via the data directory (optional, but
			// allows tests to mount policies if needed in the future).
			"--log-level", "error",
		},
		// Wait until OPA's health endpoint responds with 200 OK.
		// The /health path returns 200 when OPA is ready to serve requests.
		WaitingFor: wait.ForHTTP("/health").WithPort("8181/tcp"),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("testutil: starting opa container: %v", err)
	}

	// Resolve the host and mapped port.
	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("testutil: getting opa container host: %v", err)
	}

	port, err := container.MappedPort(ctx, "8181")
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("testutil: getting opa container port: %v", err)
	}

	url := "http://" + host + ":" + port.Port()

	cleanupFn := func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("testutil: warning: failed to terminate opa container: %v", err)
		}
	}

	t.Cleanup(cleanupFn)

	return url, cleanupFn
}
