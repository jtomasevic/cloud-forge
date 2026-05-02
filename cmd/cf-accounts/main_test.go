package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── run() tests ───────────────────────────────────────────────────────────────

func TestMain_ExitsWithCode1_WhenRunFails(t *testing.T) {
	t.Setenv("SCYLLA_PORT", "not-a-number")
	defer restoreWireFunc()
	defer func() { osExit = os.Exit }()

	var got int
	osExit = func(code int) { got = code }

	main()

	assert.Equal(t, 1, got, "main() must call osExit(1) when run() returns an error")
}

func TestRun_ReturnsError_WhenConfigInvalid(t *testing.T) {
	t.Setenv("SCYLLA_PORT", "not-a-number")
	defer restoreWireFunc()

	err := run()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid SCYLLA_PORT")
}

func TestRun_ReturnsError_WhenWireFails(t *testing.T) {
	t.Setenv("SCYLLA_PORT", "")
	defer restoreWireFunc()

	wireFunc = func(_ context.Context, _ *appConfig, _ *slog.Logger) (*App, error) {
		return nil, errors.New("scylladb: connection refused")
	}

	err := run()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

func TestRun_StartsAndShutsDownOnSIGTERM(t *testing.T) {
	t.Setenv("LISTEN_ADDR", ":0")
	t.Setenv("SCYLLA_PORT", "")
	defer restoreWireFunc()

	shutdownCalled := false
	wireFunc = func(_ context.Context, _ *appConfig, _ *slog.Logger) (*App, error) {
		return &App{
			Router:   http.NewServeMux(),
			Log:      slog.Default(),
			Shutdown: func() { shutdownCalled = true },
		}, nil
	}

	// run() starts ListenAndServe which will block; we send SIGTERM shortly
	// after to exercise the graceful-shutdown goroutine and the ErrServerClosed
	// path.
	done := make(chan error, 1)
	go func() {
		done <- run()
	}()

	// Give the server a moment to start binding before we signal it.
	time.Sleep(50 * time.Millisecond)
	proc, _ := os.FindProcess(os.Getpid())
	require.NoError(t, proc.Signal(syscall.SIGTERM))

	select {
	case err := <-done:
		assert.NoError(t, err, "clean SIGTERM should return nil")
	case <-time.After(5 * time.Second):
		t.Fatal("run() did not exit within 5 seconds after SIGTERM")
	}
	assert.True(t, shutdownCalled, "Shutdown must be called on SIGTERM")
}

// TestRun_ReturnsError_WhenPortAlreadyInUse exercises the path where
// srv.ListenAndServe() returns a non-ErrServerClosed error (EADDRINUSE).
// We pre-occupy the port with a raw net.Listener, then call run() which tries
// to bind to the same address and fails immediately.
func TestRun_ReturnsError_WhenPortAlreadyInUse(t *testing.T) {
	// Bind a port so the OS refuses re-use.
	lc := &net.ListenConfig{}
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	// Extract the port the OS assigned.
	addr := ln.Addr().String()
	t.Setenv("LISTEN_ADDR", addr)
	t.Setenv("SCYLLA_PORT", "")
	defer restoreWireFunc()

	wireFunc = func(_ context.Context, _ *appConfig, _ *slog.Logger) (*App, error) {
		return &App{
			Router:   http.NewServeMux(),
			Log:      slog.Default(),
			Shutdown: func() {},
		}, nil
	}

	// run() tries to ListenAndServe on addr — port is taken → EADDRINUSE.
	runErr := run()

	require.Error(t, runErr, "run() must return error when port is already bound")
}

// restoreWireFunc resets wireFunc to its default (calls Wire) after a test.
func restoreWireFunc() {
	wireFunc = Wire
}
