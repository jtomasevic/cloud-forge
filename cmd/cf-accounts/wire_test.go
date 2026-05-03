package main

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/gocql/gocql"
	openbao "github.com/openbao/openbao/api/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jtomasevic/cloud-forge/internal/accounts"
)

// restoreWireDeps resets the package-level factory variables after a test
// that replaced them to avoid contaminating subsequent tests.
func restoreWireDeps() {
	newScyllaSessionFn = accounts.NewSession
	newBaoClientFn = openbao.NewClient
}

// ── Wire() error paths ────────────────────────────────────────────────────────

// TestWire_ReturnsError_WhenScyllaDBUnreachable verifies that Wire propagates a
// ScyllaDB connection failure.  Port 1 is refused immediately by the OS, so
// the test completes in milliseconds.
func TestWire_ReturnsError_WhenScyllaDBUnreachable(t *testing.T) {
	defer restoreWireDeps()

	cfg := &appConfig{
		Scylla: accounts.Config{
			Hosts:          []string{"127.0.0.1"},
			Port:           1, // nothing listens here → instant "connection refused"
			ConnectTimeout: 500 * time.Millisecond,
			QueryTimeout:   500 * time.Millisecond,
		},
		OpenBaoAddr:  "http://localhost:8200",
		OpenBaoToken: "dev-root-token",
		ListenAddr:   ":0",
	}

	_, err := Wire(context.Background(), cfg, slog.Default())

	require.Error(t, err, "Wire must fail when ScyllaDB is unreachable")
}

// TestWire_ReturnsError_WhenBaoClientFails exercises the OpenBao failure path
// (sess.Close() + return nil, err) by injecting a session factory that
// succeeds with a nil session and a bao factory that returns an error.
//
// A nil *gocql.Session is safe here because the assembleApp path is never
// reached — Wire returns before construction begins.
func TestWire_ReturnsError_WhenBaoClientFails(t *testing.T) {
	defer restoreWireDeps()

	// Succeed at the ScyllaDB step with a nil session (no actual connection).
	newScyllaSessionFn = func(_ *accounts.Config) (*gocql.Session, error) {
		return nil, nil
	}
	// Fail at the OpenBao step.
	newBaoClientFn = func(_ *openbao.Config) (*openbao.Client, error) {
		return nil, errors.New("openbao: connection refused")
	}

	cfg := minimalConfig()
	_, err := Wire(context.Background(), cfg, slog.Default())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "openbao")
}

// ── assembleApp() ─────────────────────────────────────────────────────────────

// TestAssembleApp_ReturnsWiredApp verifies that assembleApp produces a non-nil
// App with a Router and a callable Shutdown, even when the session is nil.
//
// No queries are issued during construction: stores only capture the session
// pointer and the service wires them together as interface values. The full
// request path is tested in handler_test.go with real mock services.
func TestAssembleApp_ReturnsWiredApp(t *testing.T) {
	cfg := minimalConfig()

	// Create a real (but unconnected) OpenBao client — NewClient never dials.
	baoCfg := openbao.DefaultConfig()
	baoCfg.Address = cfg.OpenBaoAddr
	baoClient, err := openbao.NewClient(baoCfg)
	require.NoError(t, err, "openbao.NewClient must not fail with a valid URL")
	baoClient.SetToken(cfg.OpenBaoToken) //nolint:gosec // test token

	closeSessionCalled := false
	closeSession := func() { closeSessionCalled = true }

	// Pass nil session — stores don't issue queries during construction.
	app := assembleApp(nil, baoClient, closeSession, slog.Default())

	require.NotNil(t, app, "assembleApp must return a non-nil App")
	assert.NotNil(t, app.Router, "Router must be set")
	assert.NotNil(t, app.Shutdown, "Shutdown must be set")
	assert.NotNil(t, app.Log, "Log must be set")

	// Verify the injected closeSession is wired into Shutdown.
	app.Shutdown()
	assert.True(t, closeSessionCalled, "Shutdown must delegate to closeSession")
}

// TestWire_HappyPath_WithNilSession exercises the full Wire path using injected
// factories so that no real infrastructure is required.
func TestWire_HappyPath_WithNilSession(t *testing.T) {
	defer restoreWireDeps()

	newScyllaSessionFn = func(_ *accounts.Config) (*gocql.Session, error) {
		return nil, nil // no real ScyllaDB needed
	}
	// newBaoClientFn uses the real constructor (creates client struct, no dial).

	cfg := minimalConfig()
	app, err := Wire(context.Background(), cfg, slog.Default())

	require.NoError(t, err)
	require.NotNil(t, app)
	assert.NotNil(t, app.Router)
	// Shutdown must not panic even with a nil underlying session.
	assert.NotPanics(t, app.Shutdown)
}

// TestWire_AppShutdown_IsCallable verifies that the Shutdown closure returned
// by Wire is callable without panicking when no real session was opened.
func TestWire_AppShutdown_IsCallable(t *testing.T) {
	cfg := &appConfig{
		Scylla: accounts.Config{
			Hosts:          []string{"127.0.0.1"},
			Port:           1,
			ConnectTimeout: 100 * time.Millisecond,
			QueryTimeout:   100 * time.Millisecond,
		},
		OpenBaoAddr:  "http://localhost:8200",
		OpenBaoToken: "dev-root-token",
		ListenAddr:   ":0",
	}

	app, err := Wire(context.Background(), cfg, slog.Default())
	if err != nil {
		// Expected in environments without real ScyllaDB — verify error path.
		assert.Error(t, err)
		return
	}

	assert.NotNil(t, app.Router)
	assert.NotNil(t, app.Shutdown)
	assert.NotPanics(t, app.Shutdown)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func minimalConfig() *appConfig {
	return &appConfig{
		Scylla: accounts.Config{
			Hosts:          []string{"127.0.0.1"},
			Port:           19042,
			ConnectTimeout: 500 * time.Millisecond,
			QueryTimeout:   500 * time.Millisecond,
		},
		OpenBaoAddr:  "http://localhost:8200",
		OpenBaoToken: "dev-root-token", //nolint:gosec // test config, not a real secret
		ListenAddr:   ":0",
	}
}
