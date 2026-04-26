//go:build integration

package testutil

import (
	"context"
	"testing"

	openbao "github.com/openbao/openbao/api/v2"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// openBaoImage is the OpenBao server image. OpenBao is an open-source fork of
// HashiCorp Vault and exposes the same REST API, so the official Vault client
// SDK (openbao/api) works without modification.
const openBaoImage = "quay.io/openbao/openbao:latest"

// openBaoRootToken is the root token used to authenticate with the dev-mode
// OpenBao server started for integration tests. Dev mode starts with a single
// root token, no persistence, and all mounts pre-enabled.
//
// This token must never be used outside of a test container.
const openBaoRootToken = "root-test-token"

// StartOpenBao starts an OpenBao server in dev mode and returns a pre-authenticated
// *openbao.Client (using the root token) along with a cleanup function.
//
// Dev mode characteristics:
//   - Unsealed immediately on start
//   - In-memory storage (no disk writes)
//   - Root token is fixed to openBaoRootToken
//   - All secret engines pre-mounted (KV, PKI, etc.)
//
// The cleanup function terminates the container and is also registered with
// t.Cleanup.
func StartOpenBao(t *testing.T) (*openbao.Client, func()) {
	t.Helper()

	ctx := context.Background()

	// Start OpenBao in dev mode using a generic testcontainer.
	// No official testcontainers module exists for OpenBao, so we use the
	// generic container API and configure it via environment variables and
	// command arguments.
	req := testcontainers.ContainerRequest{
		Image:        openBaoImage,
		ExposedPorts: []string{"8200/tcp"},
		Env: map[string]string{
			// BAO_DEV_ROOT_TOKEN_ID pins the root token to our constant so
			// the test client can authenticate without dynamic token discovery.
			"BAO_DEV_ROOT_TOKEN_ID": openBaoRootToken,
		},
		// Start OpenBao in dev mode. The -dev flag disables all persistence
		// and starts an already-unsealed server with a root token.
		Cmd: []string{"server", "-dev",
			"-dev-root-token-id=" + openBaoRootToken,
			"-dev-listen-address=0.0.0.0:8200",
		},
		// Wait until OpenBao prints its "Core initialized" message, which
		// indicates the server is ready to accept API requests.
		WaitingFor: wait.ForLog("Development mode should NOT be used in production installations!"),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("testutil: starting openbao container: %v", err)
	}

	// Resolve the API address from the mapped port.
	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("testutil: getting openbao container host: %v", err)
	}

	port, err := container.MappedPort(ctx, "8200")
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("testutil: getting openbao container port: %v", err)
	}

	addr := "http://" + host + ":" + port.Port()

	// Create an OpenBao client. The openbao/api client is fully compatible
	// with the Vault HTTP API, which OpenBao implements.
	cfg := openbao.DefaultConfig()
	cfg.Address = addr

	client, err := openbao.NewClient(cfg)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("testutil: creating openbao client: %v", err)
	}

	// Authenticate with the root token. In production, services use short-lived
	// AppRole or Kubernetes auth tokens — the root token is only appropriate
	// for integration tests where secrets are ephemeral.
	client.SetToken(openBaoRootToken)

	// Verify the client can reach the server.
	if _, err := client.Sys().Health(); err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("testutil: pinging openbao: %v", err)
	}

	cleanup := func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("testutil: warning: failed to terminate openbao container: %v", err)
		}
	}

	t.Cleanup(cleanup)

	return client, cleanup
}
