package provisioner

// Whitebox tests for internal vcluster functions. These tests have access
// to unexported package-level variables (vclusterRunner, kubeconfigExporter,
// kubectlRunner) and inject fake implementations to avoid executing real
// binaries. This keeps the test suite fast and deterministic without a live
// Kubernetes cluster.

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withFakeVCluster replaces the package-level CLI runners with fakes for the
// duration of a test. It restores the originals via t.Cleanup.
//
// Parameters:
//   - createErr   returned by vclusterRunner when called with "create"
//   - kubeconfigYAML  returned by kubeconfigExporter on success (empty → error)
//   - kubectlErr  returned by kubectlRunner (controls readiness poll result)
func withFakeVCluster(t *testing.T, createErr error, kubeconfigYAML string, kubectlErr error) {
	t.Helper()

	origRunner := vclusterRunner
	origExporter := kubeconfigExporter
	origKubectl := kubectlRunner
	t.Cleanup(func() {
		vclusterRunner = origRunner
		kubeconfigExporter = origExporter
		kubectlRunner = origKubectl
	})

	vclusterRunner = func(_ context.Context, args ...string) error {
		if len(args) > 0 && args[0] == "create" {
			return createErr
		}
		if len(args) > 0 && args[0] == "delete" {
			return createErr // reuse the same error for delete tests
		}
		return nil
	}

	kubeconfigExporter = func(_ context.Context, _, _, _ string) (string, error) {
		if kubeconfigYAML == "" {
			return "", errors.New("fake: empty kubeconfig")
		}
		return kubeconfigYAML, nil
	}

	kubectlRunner = func(_ context.Context, _ ...string) error {
		return kubectlErr
	}
}

// ── CreateVCluster unit tests (via fake runners) ──────────────────────────────

// TestCreateVCluster_Success verifies the happy path: all three steps
// (create, wait, export) succeed and the kubeconfig is returned.
func TestCreateVCluster_Success(t *testing.T) {
	withFakeVCluster(t, nil, "apiVersion: v1\nkind: Config", nil)

	result, err := CreateVCluster(context.Background(), VClusterConfig{
		TenantID:      "acme-corp",
		HostNamespace: "tenant-acme-corp",
		PodCIDR:       "10.100.1.0/24",
		SvcCIDR:       "10.200.1.0/24",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "apiVersion: v1\nkind: Config", result.KubeconfigYAML)
}

// TestCreateVCluster_CreateFails verifies that a vcluster create failure is
// wrapped and returned before the wait or export steps are attempted.
func TestCreateVCluster_CreateFails(t *testing.T) {
	withFakeVCluster(t, fmt.Errorf("helm install failed"), "", nil)

	_, err := CreateVCluster(context.Background(), VClusterConfig{
		TenantID:      "acme-corp",
		HostNamespace: "tenant-acme-corp",
		PodCIDR:       "10.100.1.0/24",
		SvcCIDR:       "10.200.1.0/24",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create vCluster")
}

// TestCreateVCluster_WaitFails verifies that a kubectl readiness timeout is
// propagated as an error.
func TestCreateVCluster_WaitFails(t *testing.T) {
	withFakeVCluster(t, nil, "", fmt.Errorf("statefulset not found"))

	_, err := CreateVCluster(context.Background(), VClusterConfig{
		TenantID:      "acme-corp",
		HostNamespace: "tenant-acme-corp",
		PodCIDR:       "10.100.1.0/24",
		SvcCIDR:       "10.200.1.0/24",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wait vCluster")
}

// TestCreateVCluster_ExportFails verifies that a kubeconfig export failure is
// returned after the vCluster is ready.
func TestCreateVCluster_ExportFails(t *testing.T) {
	withFakeVCluster(t, nil, "", nil) // kubectl succeeds, but empty kubeconfigYAML triggers exporter error

	_, err := CreateVCluster(context.Background(), VClusterConfig{
		TenantID:      "acme-corp",
		HostNamespace: "tenant-acme-corp",
		PodCIDR:       "10.100.1.0/24",
		SvcCIDR:       "10.200.1.0/24",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "export kubeconfig")
}

// ── DeleteVCluster unit tests (via fake runners) ──────────────────────────────

// TestDeleteVCluster_Success verifies the happy path for deletion.
func TestDeleteVCluster_Success(t *testing.T) {
	withFakeVCluster(t, nil, "", nil)

	err := DeleteVCluster(context.Background(), "acme-corp", "tenant-acme-corp")
	require.NoError(t, err)
}

// TestDeleteVCluster_NotFoundIsIgnored verifies that a "not found" error from
// the vcluster CLI is silently suppressed (idempotent delete).
func TestDeleteVCluster_NotFoundIsIgnored(t *testing.T) {
	withFakeVCluster(t, fmt.Errorf("VirtualCluster not found"), "", nil)

	err := DeleteVCluster(context.Background(), "acme-corp", "tenant-acme-corp")
	require.NoError(t, err, "not found errors should be suppressed for idempotent delete")
}

// TestDeleteVCluster_RealErrorIsPropagated verifies that non-not-found errors
// are returned to the caller.
func TestDeleteVCluster_RealErrorIsPropagated(t *testing.T) {
	withFakeVCluster(t, fmt.Errorf("etcd connection refused"), "", nil)

	err := DeleteVCluster(context.Background(), "acme-corp", "tenant-acme-corp")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete vCluster")
}

// TestDeleteVCluster_InvalidTenantIDIsRejected verifies input validation.
func TestDeleteVCluster_InvalidTenantIDIsRejected(t *testing.T) {
	err := DeleteVCluster(context.Background(), "INVALID_ID", "tenant-invalid")
	require.Error(t, err)
}

// ── waitVClusterReady (via fake kubectlRunner) ────────────────────────────────

// TestWaitVClusterReady_SucceedsOnFirstPoll verifies that the wait function
// returns immediately when kubectl reports success.
func TestWaitVClusterReady_SucceedsOnFirstPoll(t *testing.T) {
	orig := kubectlRunner
	defer func() { kubectlRunner = orig }()
	kubectlRunner = func(_ context.Context, _ ...string) error { return nil }

	err := waitVClusterReady(context.Background(), "acme-corp", "tenant-acme-corp")
	require.NoError(t, err)
}

// TestWaitVClusterReady_DeadlineExceeded verifies that the function returns an
// error when the context is cancelled before kubectl succeeds.
func TestWaitVClusterReady_DeadlineExceeded(t *testing.T) {
	orig := kubectlRunner
	defer func() { kubectlRunner = orig }()
	kubectlRunner = func(_ context.Context, _ ...string) error {
		return fmt.Errorf("kubectl: connection refused")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*vClusterPollInterval)
	defer cancel()
	cancel() // immediately cancel

	err := waitVClusterReady(ctx, "acme-corp", "tenant-acme-corp")
	require.Error(t, err)
}

// ── Default runner smoke tests ────────────────────────────────────────────────

// TestDefaultVClusterRunner_FailsOnBadSubcommand verifies that
// defaultVClusterRunner returns an error when called with a subcommand that
// vcluster does not recognise. This confirms the exec and error-capture path
// works. The binary may or may not be installed; the test accepts both "binary
// not found" and "unknown command" as valid failure modes.
func TestDefaultVClusterRunner_FailsOnBadSubcommand(t *testing.T) {
	err := defaultVClusterRunner(context.Background(), "definitely-not-a-real-vcluster-subcommand-xyz")
	// Either "binary not found" or "unknown command" — both are errors.
	// If vcluster is installed and returns exit 0 for unknown commands we skip.
	if err == nil {
		t.Skip("vcluster binary accepted unknown subcommand — skipping exec-path test")
	}
	assert.Error(t, err)
}

// TestDefaultKubectlRunner_FailsOnInvalidArgs verifies that defaultKubectlRunner
// returns an error when called with arguments that kubectl rejects (e.g. an
// invalid resource type). This exercises the exec path without requiring a
// live cluster.
func TestDefaultKubectlRunner_FailsOnInvalidArgs(t *testing.T) {
	origKubectl := kubectlRunner
	kubectlRunner = defaultKubectlRunner
	defer func() { kubectlRunner = origKubectl }()

	// Pass a deliberately broken subcommand — kubectl returns exit status 1.
	err := defaultKubectlRunner(context.Background(), "get", "nonexistent-resource-type-12345")
	assert.Error(t, err)
}

// TestDefaultKubeconfigExporter_FailsWhenBinaryMissing verifies that
// defaultKubeconfigExporter returns an error when the vcluster binary cannot
// be found or the connect command fails.
func TestDefaultKubeconfigExporter_FailsWhenBinaryMissing(t *testing.T) {
	origExp := kubeconfigExporter
	kubeconfigExporter = defaultKubeconfigExporter
	defer func() { kubeconfigExporter = origExp }()

	_, err := defaultKubeconfigExporter(context.Background(), "acme-corp", "tenant-acme-corp", "https://localhost:6443")
	assert.Error(t, err)
}
