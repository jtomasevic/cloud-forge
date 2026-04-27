package spike_test

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	natss "github.com/nats-io/nats-server/v2/server"

	"github.com/jtomasevic/cloud-forge/spikes/nats-routing/internal/spike"
)

// startFullSpikeServer starts an in-process NATS server that has ALL accounts
// required for the complete spike run:
//   - TENANT_A  (user-a / pass-a)
//   - TENANT_B  (user-b / pass-b)
//   - TENANT_C  (user-c / pass-c)
//   - TENANT_01 … TENANT_50
//
// This lets TestRunProvisioningTest and TestRun_WithServer exercise every code
// path in provisioning.go and run.go without a real Docker cluster.
func startFullSpikeServer(t *testing.T) *natss.Server {
	t.Helper()

	storeDir := t.TempDir()

	var sb bytes.Buffer
	sb.WriteString("accounts {\n")

	// Core accounts — credentials must match the DefaultTenant* constants in spike package.
	for _, acc := range []struct{ name, user, pass string }{
		{"TENANT_A", spike.DefaultTenantAUser, spike.DefaultTenantAPass},
		{"TENANT_B", spike.DefaultTenantBUser, spike.DefaultTenantBPass},
		{"TENANT_C", spike.DefaultTenantCUser, spike.DefaultTenantCPass},
	} {
		fmt.Fprintf(&sb, "  %s { users = [{ user: %q, password: %q }], jetstream: enabled }\n",
			acc.name, acc.user, acc.pass)
	}

	// Bulk accounts TENANT_01 … TENANT_50.
	for i := range 50 {
		n := i + 1
		fmt.Fprintf(&sb, "  TENANT_%02d { users = [{ user: %q, password: %q }], jetstream: enabled }\n",
			n,
			fmt.Sprintf("tenant-%02d", n),
			fmt.Sprintf("pass-%02d", n),
		)
	}

	fmt.Fprintf(&sb, "}\njetstream { store_dir: %q }\n", storeDir)

	f, err := os.CreateTemp("", "nats-full-*.conf")
	require.NoError(t, err)
	_, err = f.WriteString(sb.String())
	require.NoError(t, err)
	require.NoError(t, f.Close())
	t.Cleanup(func() { os.Remove(f.Name()) }) //nolint:errcheck

	opts, err := natss.ProcessConfigFile(f.Name())
	require.NoError(t, err)

	opts.Port = -1
	opts.StoreDir = storeDir
	opts.NoLog = true
	opts.NoSigs = true

	srv, err := natss.NewServer(opts)
	require.NoError(t, err)

	go srv.Start()
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("full-spike NATS server did not become ready within 5s")
	}
	t.Cleanup(srv.Shutdown)
	return srv
}

// ---------------------------------------------------------------------------
// RunProvisioningTest tests
// ---------------------------------------------------------------------------

// TestRunProvisioningTest_Pass exercises the full provisioning code path:
// Q1 (connect as tenant-c) and Q5 (connect to 50 accounts sequentially).
func TestRunProvisioningTest_Pass(t *testing.T) {
	t.Parallel()

	srv := startFullSpikeServer(t)
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	q1Pass, q1Detail, q5Pass, q5Dur, q5Detail := spike.RunProvisioningTest(
		ctx,
		srv.ClientURL(),
		spike.DefaultTenantCUser, spike.DefaultTenantCPass,
		"", // no confPath → demonstrateConfigReload is a no-op
		logger,
	)

	assert.True(t, q1Pass, "Q1 must pass: %s", q1Detail)
	assert.Contains(t, q1Detail, "reachable")

	assert.True(t, q5Pass, "Q5 must pass: %s", q5Detail)
	assert.Less(t, q5Dur, 2*time.Minute, "50 accounts must connect in < 2 minutes")
}

// TestRunProvisioningTest_Q1Fail verifies that Q1 is reported as false when
// tenant-c credentials are wrong.
func TestRunProvisioningTest_Q1Fail(t *testing.T) {
	t.Parallel()

	srv := startFullSpikeServer(t)
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	q1Pass, q1Detail, _, _, _ := spike.RunProvisioningTest(
		ctx,
		srv.ClientURL(),
		spike.DefaultTenantCUser, "WRONG_PASSWORD",
		"",
		logger,
	)

	assert.False(t, q1Pass, "Q1 should fail with wrong password")
	assert.Contains(t, q1Detail, "inconclusive")
}

// TestBuildAccountList_Fifty verifies the bulk list used by
// RunProvisioningTest.
func TestBuildAccountList_Fifty(t *testing.T) {
	t.Parallel()

	accounts := spike.BuildAccountList(50)
	require.Len(t, accounts, 50)
	assert.Equal(t, "TENANT_50", accounts[49].AccountName)
	assert.Equal(t, "tenant-50", accounts[49].User)
	assert.Equal(t, "pass-50", accounts[49].Password)
}

// ---------------------------------------------------------------------------
// Run integration test
// ---------------------------------------------------------------------------

// TestRun_WithServer exercises the full Run() orchestration with a real
// embedded NATS cluster.  This is the highest-value integration test because
// it covers the connection setup, Q2, Q3, Q4, and Q1/Q5 paths in run.go.
func TestRun_WithServer(t *testing.T) {
	// Not parallel — uses a shared server and may take ~0.5s.
	srv := startFullSpikeServer(t)
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	ok := spike.Run(ctx, srv.ClientURL(), "", logger)

	// All critical questions (Q1, Q2, Q4) must pass.
	assert.True(t, ok, "Run() must return true when all critical questions pass")
}

// TestRunWithTimeout_WithServer exercises RunWithTimeout success path.
func TestRunWithTimeout_WithServer(t *testing.T) {
	srv := startFullSpikeServer(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	ok := spike.RunWithTimeout(2*time.Minute, srv.ClientURL(), "", logger)
	assert.True(t, ok)
}
