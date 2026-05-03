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

// TestMain_ExitsWithCode1_WhenRunFails verifies that main() calls osExit(1)
// when run() returns an error.  The osExit variable is replaced with a stub
// so the test process does not actually terminate.
func TestMain_ExitsWithCode1_WhenRunFails(t *testing.T) {
	t.Setenv("SCYLLA_PORT", "not-a-number") // forces configFromEnv to fail
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

	done := make(chan error, 1)
	go func() {
		done <- run()
	}()

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

// TestRun_ReturnsError_WhenPortAlreadyInUse verifies that run() propagates a
// non-ErrServerClosed ListenAndServe error (port already in use).
func TestRun_ReturnsError_WhenPortAlreadyInUse(t *testing.T) {
	lc := &net.ListenConfig{}
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

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

	runErr := run()
	require.Error(t, runErr, "run() must return error when port is already bound")
}

func restoreWireFunc() {
	wireFunc = Wire
}
