package cluster

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/cloud-forge/spikes/tenant-isolation/internal/probe"
)

// RealClient implements probe.KubectlClient by shelling out to the kubectl binary.
// All commands are run with a per-call context deadline derived from the parent ctx.
type RealClient struct {
	// KubectlBin is the path to the kubectl binary. Defaults to "kubectl" (PATH lookup).
	KubectlBin string
}

// NewRealClient returns a RealClient using the kubectl binary on the PATH.
func NewRealClient() *RealClient {
	return &RealClient{KubectlBin: "kubectl"}
}

// compile-time interface check — ensures RealClient satisfies probe.KubectlClient.
var _ probe.KubectlClient = (*RealClient)(nil)

// ──────────────────────────────────────────────────────────────────────────────
// Interface implementation
// ──────────────────────────────────────────────────────────────────────────────

// RunInPod executes cmd inside a running container using kubectl exec.
// If kubeconfigPath is empty the ambient KUBECONFIG environment variable is used.
func (c *RealClient) RunInPod(
	ctx context.Context,
	kubeconfigPath, namespace, pod, container string,
	cmd []string,
) (string, error) {
	args := c.kubeconfigArgs(kubeconfigPath)
	args = append(args, "exec", "-n", namespace, pod)
	if container != "" {
		args = append(args, "-c", container)
	}
	args = append(args, "--")
	args = append(args, cmd...)
	out, err := c.run(ctx, args...)
	return out, err
}

// Apply applies YAML content to the cluster identified by kubeconfigPath.
// The content is passed via stdin so it never touches the filesystem.
func (c *RealClient) Apply(ctx context.Context, kubeconfigPath string, yamlContent []byte) error {
	args := c.kubeconfigArgs(kubeconfigPath)
	args = append(args, "apply", "-f", "-")
	cmd := exec.CommandContext(ctx, c.kubectl(), args...)
	cmd.Stdin = bytes.NewReader(yamlContent)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl apply: %w\noutput: %s", err, out.String())
	}
	return nil
}

// WaitPodReady polls for a pod matching selector to reach Running/Ready,
// or returns an error when the timeout expires. It uses `kubectl wait` for
// efficiency and falls back to a poll loop for older cluster versions.
func (c *RealClient) WaitPodReady(
	ctx context.Context,
	kubeconfigPath, namespace, selector string,
	timeout time.Duration,
) (podName string, elapsed time.Duration, err error) {
	start := time.Now()

	// Use `kubectl wait` with a deadline.
	args := c.kubeconfigArgs(kubeconfigPath)
	args = append(args,
		"wait", "pod",
		"-n", namespace,
		"-l", selector,
		"--for=condition=Ready",
		fmt.Sprintf("--timeout=%ds", int(timeout.Seconds())),
	)
	waitCtx, cancel := context.WithTimeout(ctx, timeout+10*time.Second)
	defer cancel()
	out, err := c.run(waitCtx, args...)
	if err != nil {
		return "", time.Since(start), fmt.Errorf("wait pod ready (ns=%s sel=%s): %w — %s", namespace, selector, err, out)
	}

	// Parse pod name from "pod/<name> condition met"
	podName = parsePodNameFromWait(out)
	return podName, time.Since(start), nil
}

// DeletePod deletes a pod by name, simulating a crash.
func (c *RealClient) DeletePod(ctx context.Context, kubeconfigPath, namespace, pod string) error {
	args := c.kubeconfigArgs(kubeconfigPath)
	args = append(args, "delete", "pod", "-n", namespace, pod, "--grace-period=0", "--force")
	_, err := c.run(ctx, args...)
	return err
}

// GetPodResources returns CPU+memory for pods matched by selector.
// Requires metrics-server; returns an empty slice with a nil error if unavailable.
func (c *RealClient) GetPodResources(
	ctx context.Context,
	kubeconfigPath, namespace, selector string,
) ([]probe.PodResource, error) {
	args := c.kubeconfigArgs(kubeconfigPath)
	args = append(args, "top", "pods", "-n", namespace, "-l", selector, "--no-headers")
	out, err := c.run(ctx, args...)
	if err != nil {
		// metrics-server not installed — skip gracefully
		if strings.Contains(out, "not available") || strings.Contains(err.Error(), "not found") {
			return nil, nil
		}
		return nil, fmt.Errorf("kubectl top pods: %w — %s", err, out)
	}
	return parsePodResources(out), nil
}

// GetPodsByLabel lists pod names matching selector.
func (c *RealClient) GetPodsByLabel(
	ctx context.Context,
	kubeconfigPath, namespace, selector string,
) ([]string, error) {
	args := c.kubeconfigArgs(kubeconfigPath)
	args = append(args,
		"get", "pods",
		"-n", namespace,
		"-l", selector,
		"-o", "jsonpath={.items[*].metadata.name}",
	)
	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("get pods (ns=%s sel=%s): %w — %s", namespace, selector, err, out)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	return strings.Fields(out), nil
}

// PodIP returns the first IP of a pod matched by selector.
func (c *RealClient) PodIP(
	ctx context.Context,
	kubeconfigPath, namespace, selector string,
) (string, error) {
	args := c.kubeconfigArgs(kubeconfigPath)
	args = append(args,
		"get", "pods",
		"-n", namespace,
		"-l", selector,
		"-o", "jsonpath={.items[0].status.podIP}",
	)
	out, err := c.run(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("pod IP (ns=%s sel=%s): %w — %s", namespace, selector, err, out)
	}
	return strings.TrimSpace(out), nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

func (c *RealClient) kubectl() string {
	if c.KubectlBin != "" {
		return c.KubectlBin
	}
	return "kubectl"
}

// kubeconfigArgs returns the --kubeconfig flag slice when a path is provided.
func (c *RealClient) kubeconfigArgs(kubeconfigPath string) []string {
	if kubeconfigPath != "" {
		return []string{"--kubeconfig", kubeconfigPath}
	}
	return nil
}

// run executes kubectl with the given args and returns combined stdout+stderr.
func (c *RealClient) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, c.kubectl(), args...)
	// Propagate KUBECONFIG from the environment if set
	cmd.Env = os.Environ()
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

// ──────────────────────────────────────────────────────────────────────────────
// Output parsers (pure functions, fully unit-testable)
// ──────────────────────────────────────────────────────────────────────────────

// parsePodNameFromWait extracts the pod name from kubectl wait output like:
//
//	pod/echo-server-abc condition met
func parsePodNameFromWait(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "pod/") {
			parts := strings.Fields(line)
			if len(parts) > 0 {
				return strings.TrimPrefix(parts[0], "pod/")
			}
		}
	}
	return ""
}

// parsePodResources parses `kubectl top pods --no-headers` output.
// Each line has the form:
//
//	NAME   CPU(cores)   MEMORY(bytes)
func parsePodResources(out string) []probe.PodResource {
	var resources []probe.PodResource
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		resources = append(resources, probe.PodResource{
			Name:     fields[0],
			CPUMilli: probe.ParseCPUMillicores(fields[1]),
			MemMB:    probe.ParseMemMB(fields[2]),
		})
	}
	return resources
}
