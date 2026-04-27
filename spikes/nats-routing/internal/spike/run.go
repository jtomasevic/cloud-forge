package spike

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Default NATS credentials — match config/nats.conf.
const (
	DefaultNATSURL = "nats://localhost:4222"

	DefaultTenantAUser = "user-a"
	DefaultTenantAPass = "password-a"
	DefaultTenantBUser = "user-b"
	DefaultTenantBPass = "password-b"
	DefaultTenantCUser = "user-c"
	DefaultTenantCPass = "password-c"

	// IsolationSubject is the core NATS subject used for the Q2 isolation test.
	IsolationSubject = "events.secret"
)

// Run orchestrates the full Spike 0.6 test sequence and prints the results
// table to stdout.  It exits non-zero if any critical question fails.
//
// url is the NATS server endpoint (default "nats://localhost:4222").
// confPath is the path to config/nats.conf; pass "" to skip the config-reload
// demo.
func Run(ctx context.Context, url, confPath string, logger *slog.Logger) bool {
	logger.Info("spike starting", "nats_url", url)

	// ── Connect as tenant-a ─────────────────────────────────────────────────
	ncA, err := ConnectWithRetryCtx(ctx, url, DefaultTenantAUser, DefaultTenantAPass, 10, 2*time.Second, logger)
	if err != nil {
		logger.Error("cannot connect as tenant-a", "error", err)
		return false
	}
	defer ncA.Drain() //nolint:errcheck

	// ── Connect as tenant-b ─────────────────────────────────────────────────
	ncB, err := ConnectWithRetryCtx(ctx, url, DefaultTenantBUser, DefaultTenantBPass, 10, 2*time.Second, logger)
	if err != nil {
		logger.Error("cannot connect as tenant-b", "error", err)
		return false
	}
	defer ncB.Drain() //nolint:errcheck

	var result SpikeResult

	// Q2: Cross-account isolation.
	logger.Info("── Q2: cross-account isolation test ──────────────────────────")
	result.Q2Pass, result.Q2Detail = RunIsolationTest(ctx, ncA, ncB, IsolationSubject, logger)

	// Q4: Content-based routing.
	logger.Info("── Q4: content-based routing test ────────────────────────────")
	result.Q4Pass, result.Q4Detail = RunContentBasedRouting(ctx, ncA, nil, logger)

	// Q3: Latency benchmark.
	logger.Info("── Q3: latency benchmark ─────────────────────────────────────")
	result.Q3Stats, result.Q3Detail = RunLatencyBenchmark(ctx, ncA, 0, logger)

	// Q1 / Q5: Dynamic provisioning.
	logger.Info("── Q1 / Q5: dynamic provisioning test ────────────────────────")
	result.Q1Pass, result.Q1Detail,
		result.Q5Pass, result.Q5Duration, result.Q5Detail =
		RunProvisioningTest(ctx, url, DefaultTenantCUser, DefaultTenantCPass, confPath, logger)

	return PrintResults(result, os.Stdout, logger)
}

// ConfigPath returns the absolute path to config/nats.conf relative to this
// source file.  It uses runtime.Caller so the path is correct whether the
// binary is built with -trimpath or not (in trimpath builds srcFile is empty
// and filepath.Dir returns ".").
func ConfigPath() string {
	_, srcFile, _, _ := runtime.Caller(0)
	// run.go lives in internal/spike/; config/ is two levels up.
	return filepath.Join(filepath.Dir(srcFile), "..", "..", "config", "nats.conf")
}

// RunWithTimeout is a convenience wrapper for tests and CLI tools that want a
// fixed timeout without constructing their own context.
func RunWithTimeout(timeout time.Duration, url, confPath string, logger *slog.Logger) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return Run(ctx, url, confPath, logger)
}
