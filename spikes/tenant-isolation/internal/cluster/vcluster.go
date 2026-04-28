package cluster

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// VCluster lifecycle helpers
// ──────────────────────────────────────────────────────────────────────────────

// VClusterInfo describes a running vCluster instance.
type VClusterInfo struct {
	// Name is the vCluster name (e.g. "tenant-a").
	Name string
	// Namespace is the host cluster namespace that holds the vCluster pods.
	Namespace string
	// KubeconfigPath is the path to the generated kubeconfig for this vCluster.
	KubeconfigPath string
}

// CreateVCluster provisions a new vCluster in the given namespace.
//
// It calls `vcluster create <name> -n <namespace> --connect=false`
// and waits up to timeout for the API server to become Ready.
// The kubeconfig is written to kubeconfigPath.
//
// Returns the elapsed time from command start to API ready.
func CreateVCluster(
	ctx context.Context,
	name, namespace, kubeconfigPath string,
	timeout time.Duration,
) (elapsed time.Duration, err error) {
	start := time.Now()

	// Create namespace if it does not exist (idempotent)
	if err := ensureNamespace(ctx, namespace); err != nil {
		return 0, fmt.Errorf("ensure namespace %q: %w", namespace, err)
	}

	// Create the vCluster
	createArgs := []string{
		"create", name,
		"-n", namespace,
		"--connect=false",
		"--update-current=false",
	}
	if err := runVCluster(ctx, createArgs...); err != nil {
		return 0, fmt.Errorf("vcluster create %q: %w", name, err)
	}

	// Wait for the vCluster's k8s API to become healthy
	if err := waitVClusterReady(ctx, name, namespace, timeout); err != nil {
		return time.Since(start), fmt.Errorf("vcluster %q not ready: %w", name, err)
	}

	// Export kubeconfig
	if err := exportKubeconfig(ctx, name, namespace, kubeconfigPath); err != nil {
		return time.Since(start), fmt.Errorf("export kubeconfig for %q: %w", name, err)
	}

	return time.Since(start), nil
}

// DeleteVCluster removes a vCluster and its host namespace.
func DeleteVCluster(ctx context.Context, name, namespace string) error {
	args := []string{"delete", name, "-n", namespace, "--delete-namespace"}
	if err := runVCluster(ctx, args...); err != nil {
		// Ignore "not found" errors — idempotent cleanup
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "not exist") {
			return nil
		}
		return fmt.Errorf("vcluster delete %q: %w", name, err)
	}
	return nil
}

// KubeconfigPath returns the canonical kubeconfig path for a vCluster in the
// given directory.
func KubeconfigPath(dir, name string) string {
	return fmt.Sprintf("%s/%s.kubeconfig", dir, name)
}

// ──────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────────────────────────────────────

// ensureNamespace creates the namespace if it does not already exist.
func ensureNamespace(ctx context.Context, namespace string) error {
	cmd := exec.CommandContext(ctx, "kubectl", "create", "namespace", namespace, "--dry-run=client", "-o", "yaml")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("generate namespace YAML: %w — %s", err, buf.String())
	}
	applyCmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", "-")
	applyCmd.Stdin = &buf
	var out bytes.Buffer
	applyCmd.Stdout = &out
	applyCmd.Stderr = &out
	applyCmd.Env = os.Environ()
	if err := applyCmd.Run(); err != nil {
		return fmt.Errorf("apply namespace: %w — %s", err, out.String())
	}
	return nil
}

// waitVClusterReady polls vcluster list until the target vCluster shows as "Running".
func waitVClusterReady(ctx context.Context, name, namespace string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	pollInterval := 5 * time.Second

	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		running, err := isVClusterRunning(ctx, name, namespace)
		if err == nil && running {
			return nil
		}
		time.Sleep(pollInterval)
	}
	return fmt.Errorf("timed out after %s waiting for vCluster %q to be Running", timeout, name)
}

// isVClusterRunning returns true if `vcluster list` shows the vCluster in Running state.
func isVClusterRunning(ctx context.Context, name, namespace string) (bool, error) {
	cmd := exec.CommandContext(ctx, "vcluster", "list", "-n", namespace, "--output", "json")
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		// Fall back to simple presence check via kubectl
		return isVClusterPodRunning(ctx, name, namespace)
	}
	// Simple string search — the JSON output contains the name and a STATUS field
	output := string(out)
	return strings.Contains(output, name) && strings.Contains(output, "Running"), nil
}

// isVClusterPodRunning checks whether the vcluster StatefulSet pod is Running.
func isVClusterPodRunning(ctx context.Context, name, namespace string) (bool, error) {
	cmd := exec.CommandContext(ctx,
		"kubectl", "get", "pod",
		"-n", namespace,
		"-l", fmt.Sprintf("app=vcluster,release=%s", name),
		"--no-headers",
		"-o", "custom-columns=STATUS:.status.phase",
	)
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return strings.Contains(strings.TrimSpace(string(out)), "Running"), nil
}

// exportKubeconfig writes the vCluster kubeconfig to kubeconfigPath using
// `vcluster connect <name> --print --update-current=false`.
func exportKubeconfig(ctx context.Context, name, namespace, kubeconfigPath string) error {
	cmd := exec.CommandContext(ctx,
		"vcluster", "connect", name,
		"-n", namespace,
		"--print",
		"--update-current=false",
	)
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("vcluster connect --print: %w", err)
	}
	if err := os.MkdirAll(dirOf(kubeconfigPath), 0700); err != nil {
		return fmt.Errorf("create kubeconfig dir: %w", err)
	}
	if err := os.WriteFile(kubeconfigPath, out, 0600); err != nil {
		return fmt.Errorf("write kubeconfig: %w", err)
	}
	return nil
}

// runVCluster executes the vcluster CLI with the given args and returns an error
// that includes combined stdout+stderr on failure.
func runVCluster(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "vcluster", args...)
	cmd.Env = os.Environ()
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w — output: %s", err, out.String())
	}
	return nil
}

// dirOf returns the directory portion of a file path.
func dirOf(path string) string {
	idx := strings.LastIndexByte(path, '/')
	if idx < 0 {
		return "."
	}
	return path[:idx]
}
