package spike_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	natss "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
)

// startSingleServer starts a minimal in-process NATS server with JetStream
// enabled but without account isolation.  Use it for benchmark and routing
// tests where only one set of credentials is needed.
func startSingleServer(t *testing.T) *natss.Server {
	t.Helper()

	opts := &natss.Options{
		// Port -1 tells the server to pick an available port automatically.
		Port:      -1,
		JetStream: true,
		StoreDir:  t.TempDir(),
		NoLog:     true,
		NoSigs:    true,
	}

	srv, err := natss.NewServer(opts)
	require.NoError(t, err, "failed to create embedded NATS server")

	// Start the server in the background.
	go srv.Start()

	// Wait until the server is ready to accept connections.
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("embedded NATS server did not become ready within 5s")
	}

	// Register cleanup so the server is always shut down after the test.
	t.Cleanup(srv.Shutdown)
	return srv
}

// startMultiAccountServer starts an in-process NATS server with JetStream and
// two isolated tenant accounts (TENANT_A and TENANT_B).
//
// Credentials:
//
//	TENANT_A: user-a / pass-a
//	TENANT_B: user-b / pass-b
//
// Use this server for cross-account isolation tests (Q2) and for any test
// that needs to verify that messages stay within their account namespace.
func startMultiAccountServer(t *testing.T) *natss.Server {
	t.Helper()

	storeDir := t.TempDir()

	// Write a minimal nats.conf with two accounts to a temp file.
	// ProcessConfigFile is the official API for reading NATS server options
	// from a config file string.
	conf := fmt.Sprintf(`
accounts {
  TENANT_A {
    users = [{ user: "user-a", password: "pass-a" }]
    jetstream: enabled
  }
  TENANT_B {
    users = [{ user: "user-b", password: "pass-b" }]
    jetstream: enabled
  }
}
jetstream {
  store_dir: %q
}
`, storeDir)

	f, err := os.CreateTemp("", "nats-test-*.conf")
	require.NoError(t, err, "create temp config")
	_, err = f.WriteString(conf)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	t.Cleanup(func() { os.Remove(f.Name()) }) //nolint:errcheck

	opts, err := natss.ProcessConfigFile(f.Name())
	require.NoError(t, err, "ProcessConfigFile")

	// Override the port and store dir so test instances never collide.
	opts.Port = -1
	opts.StoreDir = storeDir
	opts.NoLog = true
	opts.NoSigs = true

	srv, err := natss.NewServer(opts)
	require.NoError(t, err, "NewServer")

	go srv.Start()
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("multi-account NATS server did not become ready within 5s")
	}

	t.Cleanup(srv.Shutdown)
	return srv
}

// connectAs creates a NATS connection to srv using the given credentials.
// Tests that need bare connections (no JetStream) use this helper.
func connectAs(t *testing.T, srv *natss.Server, user, pass string) *nats.Conn {
	t.Helper()

	nc, err := nats.Connect(srv.ClientURL(),
		nats.UserInfo(user, pass),
		nats.Timeout(3*time.Second),
	)
	require.NoError(t, err, "nats.Connect user=%s", user)
	t.Cleanup(func() { nc.Drain() }) //nolint:errcheck
	return nc
}

// connectAnon creates an anonymous NATS connection to srv (no credentials).
// Use this when the server has no account configuration.
func connectAnon(t *testing.T, srv *natss.Server) *nats.Conn {
	t.Helper()

	nc, err := nats.Connect(srv.ClientURL(), nats.Timeout(3*time.Second))
	require.NoError(t, err, "nats.Connect anon")
	t.Cleanup(func() { nc.Drain() }) //nolint:errcheck
	return nc
}
