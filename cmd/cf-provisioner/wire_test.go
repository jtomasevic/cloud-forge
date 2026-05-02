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

// restoreWireDeps resets factory variables after a test replaces them.
func restoreWireDeps() {
	newScyllaSessionFn = accounts.NewSession
	newBaoClientFn = openbao.NewClient
	sessionCloseFn = func(s *gocql.Session) { s.Close() }
}

// ── Wire() error paths ────────────────────────────────────────────────────────

// TestWire_ReturnsError_WhenScyllaDBUnreachable verifies that Wire propagates a
// ScyllaDB connection failure. Port 1 is refused immediately, so the test
// completes in milliseconds without needing real infrastructure.
func TestWire_ReturnsError_WhenScyllaDBUnreachable(t *testing.T) {
	defer restoreWireDeps()

	cfg := &appConfig{
		Scylla: accounts.Config{
			Hosts:          []string{"127.0.0.1"},
			Port:           1,
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
// by injecting a nil session factory (succeeds) and a failing bao factory.
func TestWire_ReturnsError_WhenBaoClientFails(t *testing.T) {
	defer restoreWireDeps()

	newScyllaSessionFn = func(_ *accounts.Config) (*gocql.Session, error) {
		return nil, nil
	}
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
// App with a valid Router and a callable Shutdown when given a nil session.
func TestAssembleApp_ReturnsWiredApp(t *testing.T) {
	cfg := minimalConfig()

	// Real (unconnected) OpenBao client — NewClient never dials.
	baoCfg := openbao.DefaultConfig()
	baoCfg.Address = cfg.OpenBaoAddr
	baoClient, err := openbao.NewClient(baoCfg)
	require.NoError(t, err)
	baoClient.SetToken(cfg.OpenBaoToken) //nolint:gosec // test token

	closeSessionCalled := false
	closeSession := func() { closeSessionCalled = true }

	app := assembleApp(nil, baoClient, closeSession, slog.Default())

	require.NotNil(t, app)
	assert.NotNil(t, app.Router)
	assert.NotNil(t, app.Shutdown)

	app.Shutdown()
	assert.True(t, closeSessionCalled)
}

// TestWire_HappyPath_WithNilSession exercises the full Wire path without real
// infrastructure by injecting a nil-session factory.
func TestWire_HappyPath_WithNilSession(t *testing.T) {
	defer restoreWireDeps()

	newScyllaSessionFn = func(_ *accounts.Config) (*gocql.Session, error) {
		return nil, nil
	}

	cfg := minimalConfig()
	app, err := Wire(context.Background(), cfg, slog.Default())

	require.NoError(t, err)
	require.NotNil(t, app)
	assert.NotPanics(t, app.Shutdown)
}

// TestWire_NonNilSession_ShutdownClosesSession covers the if-sess-nil branch
// inside Wire. A non-nil zero-value *gocql.Session is returned by the seam, and
// sessionCloseFn is replaced with a no-op so the zero-value session is never
// actually closed (which would panic). Calling app.Shutdown() then exercises the
// closeSession closure body, reaching 100 % on wire.go.
func TestWire_NonNilSession_ShutdownClosesSession(t *testing.T) {
	defer restoreWireDeps()

	closeCalled := false
	sessionCloseFn = func(_ *gocql.Session) { closeCalled = true }
	newScyllaSessionFn = func(_ *accounts.Config) (*gocql.Session, error) {
		return new(gocql.Session), nil // non-nil; never actually used for queries
	}

	cfg := minimalConfig()
	app, err := Wire(context.Background(), cfg, slog.Default())

	require.NoError(t, err)
	require.NotNil(t, app)

	app.Shutdown()
	assert.True(t, closeCalled, "Shutdown must invoke sessionCloseFn when the session is non-nil")
}

// TestWire_AppShutdown_IsCallable verifies the Shutdown closure is safe.
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
		assert.Error(t, err)
		return
	}

	assert.NotNil(t, app.Router)
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
		OpenBaoToken: "dev-root-token", //nolint:gosec // test config
		ListenAddr:   ":0",
	}
}
