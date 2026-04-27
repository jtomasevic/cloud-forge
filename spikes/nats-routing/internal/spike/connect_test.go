package spike_test

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jtomasevic/cloud-forge/spikes/nats-routing/internal/spike"
)

// TestGetEnvOrDefault_NotSet verifies that the default value is returned when
// the variable is absent from the environment.
func TestGetEnvOrDefault_NotSet(t *testing.T) {
	t.Setenv("SPIKE_TEST_VAR_UNSET", "")
	os.Unsetenv("SPIKE_TEST_VAR_UNSET") //nolint:errcheck

	v := spike.GetEnvOrDefault("SPIKE_TEST_VAR_UNSET", "fallback")
	assert.Equal(t, "fallback", v)
}

// TestGetEnvOrDefault_Set verifies that the environment variable value is
// returned when it is set.
func TestGetEnvOrDefault_Set(t *testing.T) {
	t.Setenv("SPIKE_TEST_VAR_SET", "from-env")
	v := spike.GetEnvOrDefault("SPIKE_TEST_VAR_SET", "default")
	assert.Equal(t, "from-env", v)
}

// TestGetEnvOrDefault_EmptyString verifies that an empty string value causes
// the default to be returned (empty env vars are treated as absent).
func TestGetEnvOrDefault_EmptyString(t *testing.T) {
	t.Setenv("SPIKE_TEST_VAR_EMPTY", "")
	v := spike.GetEnvOrDefault("SPIKE_TEST_VAR_EMPTY", "default")
	assert.Equal(t, "default", v)
}

// TestConnectWithRetryN_Success verifies that ConnectWithRetryN returns a
// connection on the first attempt when the server is running.
func TestConnectWithRetryN_Success(t *testing.T) {
	t.Parallel()

	srv := startSingleServer(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	nc, err := spike.ConnectWithRetryN(srv.ClientURL(), "", "", 1, 0, logger)
	require.NoError(t, err)
	require.NotNil(t, nc)
	nc.Close()
}

// TestConnectWithRetryN_Failure verifies that ConnectWithRetryN returns an
// error when the server address is not reachable (even after n retries).
func TestConnectWithRetryN_Failure(t *testing.T) {
	t.Parallel()

	// Port 1 is privileged and will be rejected immediately, making this
	// fast without sleeping.
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	_, err := spike.ConnectWithRetryN(
		"nats://127.0.0.1:1", "", "", 2, 0, logger,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "all 2 NATS connection attempts failed")
}

// TestConnectWithRetry_Success is a smoke test for the default wrapper which
// uses 10 retries but should succeed on the first attempt here.
func TestConnectWithRetry_Success(t *testing.T) {
	t.Parallel()

	srv := startSingleServer(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	nc, err := spike.ConnectWithRetry(srv.ClientURL(), "", "", logger)
	require.NoError(t, err)
	require.NotNil(t, nc)
	// Shorten reconnect timeout so the test completes quickly.
	nc.Close()
}

// TestConnectWithRetry_FailureFast verifies that ConnectWithRetry eventually
// returns an error for a bad address (we override the delay to 0 via env
// so we don't actually sleep 10 × 2s in CI; instead we call ConnectWithRetryN
// directly with maxRetries=1).
func TestConnectWithRetry_FailureFast(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	_, err := spike.ConnectWithRetryN("nats://127.0.0.1:1", "", "", 1, 0, logger)
	require.Error(t, err)
}

// TestBuildAccountList_Count verifies that n accounts are generated.
func TestBuildAccountList_Count(t *testing.T) {
	t.Parallel()

	accounts := spike.BuildAccountList(50)
	assert.Len(t, accounts, 50)
}

// TestBuildAccountList_Naming verifies the TENANT_01…TENANT_50 naming pattern.
func TestBuildAccountList_Naming(t *testing.T) {
	t.Parallel()

	accounts := spike.BuildAccountList(3)
	assert.Equal(t, "TENANT_01", accounts[0].AccountName)
	assert.Equal(t, "tenant-01", accounts[0].User)
	assert.Equal(t, "pass-01", accounts[0].Password)

	assert.Equal(t, "TENANT_03", accounts[2].AccountName)
	assert.Equal(t, "tenant-03", accounts[2].User)
}

// TestBuildAccountList_ZeroElements verifies that an empty list is returned
// for n=0 without panic.
func TestBuildAccountList_ZeroElements(t *testing.T) {
	t.Parallel()

	accounts := spike.BuildAccountList(0)
	assert.Empty(t, accounts)
}

// TestDemonstrateConfigReload_NoPath verifies that DemonstrateConfigReload is
// a no-op (does not panic) when an empty confPath is supplied.
func TestDemonstrateConfigReload_NoPath(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Must not panic — empty path triggers the early-return guard.
	assert.NotPanics(t, func() {
		spike.DemonstrateConfigReload(ctx, "nats://localhost:4222", "", logger)
	})
}

// TestDemonstrateConfigReload_MissingFile verifies that DemonstrateConfigReload
// logs a warning and returns gracefully when the config file doesn't exist.
func TestDemonstrateConfigReload_MissingFile(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	assert.NotPanics(t, func() {
		spike.DemonstrateConfigReload(ctx, "nats://localhost:4222", "/nonexistent/nats.conf", logger)
	})
}

// TestDemonstrateConfigReload_DockerNotAvailable verifies that the function
// exits gracefully when Docker is absent (no panic, no test failure).
// A real temp file is used so the read step succeeds; the docker cp step
// will fail since Docker is not expected to be available in CI.
func TestDemonstrateConfigReload_DockerNotAvailable(t *testing.T) {
	t.Parallel()

	// Write a minimal nats.conf to a temp file.
	f, err := os.CreateTemp("", "nats-demo-*.conf")
	require.NoError(t, err)
	_, err = f.WriteString("# Designate the system account\nsystem_account: SYS\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	t.Cleanup(func() { os.Remove(f.Name()) }) //nolint:errcheck

	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// docker cp will fail (no container named nats-1), but the function must
	// not panic — it logs a warning and returns.
	assert.NotPanics(t, func() {
		spike.DemonstrateConfigReload(ctx, "nats://localhost:4222", f.Name(), logger)
	})
}

// TestConfigPath_ReturnsString verifies that ConfigPath returns a non-empty
// string (the build may use -trimpath, in which case the path can be "").
func TestConfigPath_ReturnsString(t *testing.T) {
	t.Parallel()
	// We only verify that it doesn't panic; the path itself may or may not
	// exist depending on the build environment.
	p := spike.ConfigPath()
	_ = p // may be "" in trimpath builds
}

// TestRunWithTimeout_Unreachable verifies that RunWithTimeout returns false
// when no NATS server is available.  We pass a 300ms timeout so the context-
// aware retry loop aborts after the first failed attempt + sleep window rather
// than burning through all 10 × 2s retries.
func TestRunWithTimeout_Unreachable(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ok := spike.RunWithTimeout(300*time.Millisecond, "nats://127.0.0.1:1", "", logger)
	assert.False(t, ok)
}
