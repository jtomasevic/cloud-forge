package provisioner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// vClusterReadyTimeout is the maximum time to wait for the vCluster API server
// to become ready after creation. Validated in the tenant-isolation spike:
// p95 ~8.7s (cold images), ~2.5s (warm cache). 90s gives generous headroom.
const vClusterReadyTimeout = 90 * time.Second

// vClusterPollInterval is the delay between readiness polls.
const vClusterPollInterval = 3 * time.Second

// VClusterConfig holds the parameters for creating a tenant vCluster.
type VClusterConfig struct {
	// TenantID is the CloudForge tenant identifier. The vCluster is created
	// with this name inside the host namespace tenant-{TenantID}.
	TenantID string

	// HostNamespace is the Kubernetes host namespace where the vCluster runs.
	// Convention: "tenant-{tenant-id}".
	HostNamespace string

	// PodCIDR is the pod network CIDR for the vCluster, e.g. "10.100.3.0/24".
	// Must not overlap with any other vCluster or the host pod CIDR.
	PodCIDR string

	// SvcCIDR is the service network CIDR for the vCluster, e.g. "10.200.3.0/24".
	SvcCIDR string
}

// VClusterResult holds the outputs of a successful vCluster creation.
type VClusterResult struct {
	// KubeconfigYAML is the kubeconfig for the tenant's vCluster API server.
	// The provisioner stores this in OpenBao immediately after creation.
	// The raw kubeconfig must not be logged.
	KubeconfigYAML string
}

// CreateVCluster creates a new vCluster for a tenant using the vcluster CLI.
//
// It runs:
//
//	vcluster create {tenantID} \
//	  --namespace {hostNamespace} \
//	  --chart-values <pod/svc-cidr values> \
//	  --connect=false
//
// Then waits for the API server to become ready and exports the kubeconfig.
//
// Prerequisites: the vcluster CLI must be installed (check with `vcluster version`).
// In the dev environment, install with: brew install vcluster
//
// The context deadline governs the total allowed duration including the wait
// for readiness. Callers should set a timeout of at least 2 minutes.
func CreateVCluster(ctx context.Context, cfg VClusterConfig) (*VClusterResult, error) {
	if err := validateTenantID(cfg.TenantID); err != nil {
		return nil, err
	}
	if cfg.HostNamespace == "" {
		return nil, errors.New("provisioner: vCluster host namespace must not be empty")
	}
	if cfg.PodCIDR == "" || cfg.SvcCIDR == "" {
		return nil, errors.New("provisioner: vCluster pod and service CIDRs must not be empty")
	}

	// Build vcluster create arguments.
	// --chart-values inline passes Helm values that set the pod and service
	// CIDRs for the inner k3s control plane.
	helmValues := fmt.Sprintf(
		"vcluster:\n  extraArgs:\n    - --cluster-cidr=%s\n    - --service-cidr=%s\n",
		cfg.PodCIDR, cfg.SvcCIDR,
	)

	args := []string{
		"create", cfg.TenantID,
		"--namespace", cfg.HostNamespace,
		"--chart-values", helmValues,
		"--connect=false",
		"--update-current=false", // do not modify the host kubeconfig
	}

	if err := runVClusterCLI(ctx, args...); err != nil {
		return nil, fmt.Errorf("provisioner: create vCluster for tenant %q: %w", cfg.TenantID, err)
	}

	// Wait for the vCluster API server to become ready.
	if err := waitVClusterReady(ctx, cfg.TenantID, cfg.HostNamespace); err != nil {
		return nil, fmt.Errorf("provisioner: wait vCluster ready for tenant %q: %w", cfg.TenantID, err)
	}

	// Export the kubeconfig. --server specifies the in-cluster DNS address so
	// the stored kubeconfig works from inside the host cluster (provisioner pod)
	// rather than pointing at 127.0.0.1 (which only works during a live
	// vcluster connect session).
	inClusterServer := fmt.Sprintf(
		"https://%s.%s.svc.cluster.local:443",
		cfg.TenantID, cfg.HostNamespace,
	)
	kubeconfigYAML, err := exportVClusterKubeconfig(ctx, cfg.TenantID, cfg.HostNamespace, inClusterServer)
	if err != nil {
		return nil, fmt.Errorf("provisioner: export kubeconfig for tenant %q: %w", cfg.TenantID, err)
	}

	return &VClusterResult{KubeconfigYAML: kubeconfigYAML}, nil
}

// DeleteVCluster removes the vCluster for a tenant and deletes the host
// namespace. Safe to call on a partially-created or already-deleted vCluster
// (the CLI returns a benign error for missing resources, which is suppressed).
func DeleteVCluster(ctx context.Context, tenantID, hostNamespace string) error {
	if err := validateTenantID(tenantID); err != nil {
		return err
	}

	err := runVClusterCLI(ctx, "delete", tenantID, "--namespace", hostNamespace)
	if err != nil {
		// Treat "not found" as success (idempotent delete).
		if isVClusterNotFound(err) {
			return nil
		}
		return fmt.Errorf("provisioner: delete vCluster for tenant %q: %w", tenantID, err)
	}
	return nil
}

// vclusterRunner is a package-level variable that wraps the vcluster CLI call.
// Tests replace it with a fake to avoid executing the real binary.
var vclusterRunner = defaultVClusterRunner

// defaultVClusterRunner executes the real vcluster CLI binary.
func defaultVClusterRunner(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "vcluster", args...) //nolint:gosec // args are validated by CreateVCluster before reaching this runner
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		details := strings.TrimSpace(stderr.String())
		if details == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, details)
	}
	return nil
}

// runVClusterCLI delegates to vclusterRunner so tests can intercept the call.
func runVClusterCLI(ctx context.Context, args ...string) error {
	return vclusterRunner(ctx, args...)
}

// waitVClusterReady polls until the vCluster's API server pod is Running and
// Ready, or until the context deadline is exceeded.
//
// It uses `kubectl wait` against the vCluster StatefulSet in the host namespace.
func waitVClusterReady(ctx context.Context, tenantID, hostNamespace string) error {
	waitCtx, cancel := context.WithTimeout(ctx, vClusterReadyTimeout)
	defer cancel()

	for {
		// Check if vCluster pod is running by inspecting the StatefulSet rollout.
		err := runKubectl(waitCtx,
			"rollout", "status",
			"statefulset/"+tenantID,
			"-n", hostNamespace,
			"--timeout=5s",
		)
		if err == nil {
			return nil
		}

		select {
		case <-waitCtx.Done():
			return fmt.Errorf("vCluster %q in namespace %q not ready after %s: %w",
				tenantID, hostNamespace, vClusterReadyTimeout, waitCtx.Err())
		case <-time.After(vClusterPollInterval):
		}
	}
}

// kubeconfigExporter is a package-level variable for exporting kubeconfigs.
// Tests replace it with a fake that returns a fixed YAML string.
var kubeconfigExporter = defaultKubeconfigExporter

// defaultKubeconfigExporter runs `vcluster connect --print` to retrieve the
// kubeconfig YAML for the tenant's vCluster API server.
//
// inClusterServer overrides the server address to the Kubernetes-internal DNS
// name so the stored kubeconfig works from inside the host cluster.
func defaultKubeconfigExporter(ctx context.Context, tenantID, hostNamespace, inClusterServer string) (string, error) {
	cmd := exec.CommandContext(ctx, "vcluster", "connect", tenantID, //nolint:gosec // tenantID is validated by validateTenantID before this call
		"--namespace", hostNamespace,
		"--server", inClusterServer,
		"--print",
		"--update-current=false",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		details := strings.TrimSpace(stderr.String())
		return "", fmt.Errorf("vcluster connect --print: %w: %s", err, details)
	}

	kc := strings.TrimSpace(stdout.String())
	if kc == "" {
		return "", errors.New("provisioner: vcluster connect --print returned empty output")
	}
	return kc, nil
}

// exportVClusterKubeconfig delegates to kubeconfigExporter so tests can
// substitute a fake implementation.
func exportVClusterKubeconfig(ctx context.Context, tenantID, hostNamespace, inClusterServer string) (string, error) {
	return kubeconfigExporter(ctx, tenantID, hostNamespace, inClusterServer)
}

// kubectlRunner is a package-level variable for kubectl readiness checks.
// Tests replace it with a fake to avoid real cluster calls.
var kubectlRunner = defaultKubectlRunner

func defaultKubectlRunner(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "kubectl", args...) //nolint:gosec // args are caller-controlled internal kubectl sub-commands
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl %v: %w: %s", args, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// runKubectl delegates to kubectlRunner so tests can substitute a fake.
func runKubectl(ctx context.Context, args ...string) error {
	return kubectlRunner(ctx, args...)
}

// isVClusterNotFound returns true if the vcluster CLI error indicates that the
// named vCluster does not exist (safe to ignore during idempotent deletes).
func isVClusterNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") ||
		strings.Contains(msg, "no virtual cluster") ||
		strings.Contains(msg, "does not exist")
}
