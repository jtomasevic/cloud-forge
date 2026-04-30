package cluster

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/cloud-forge/spikes/cilium-enforcement/internal/probe"
)

// RealClient implements probe.KubectlClient by shelling out to the kubectl binary.
// All commands are run with the supplied context for timeout/cancellation.
type RealClient struct {
	// KubectlBin is the path to the kubectl binary. Defaults to "kubectl" (PATH lookup).
	KubectlBin string
}

// NewRealClient returns a RealClient using the kubectl binary on the PATH.
func NewRealClient() *RealClient {
	return &RealClient{KubectlBin: "kubectl"}
}

// Compile-time interface check — ensures RealClient satisfies probe.KubectlClient.
var _ probe.KubectlClient = (*RealClient)(nil)

func (c *RealClient) kubectl() string {
	if c.KubectlBin != "" {
		return c.KubectlBin
	}
	return "kubectl"
}

// run executes kubectl with the given arguments and returns combined stdout+stderr.
func (c *RealClient) run(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, c.kubectl(), args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("kubectl %s: %w\noutput: %s", args[0], err, string(out))
	}
	return string(out), nil
}

// ──────────────────────────────────────────────────────────────────────────────
// probe.KubectlClient implementation
// ──────────────────────────────────────────────────────────────────────────────

// Apply applies YAML content to the cluster. Content is passed via stdin.
func (c *RealClient) Apply(ctx context.Context, _ string, yamlContent []byte) error {
	cmd := exec.CommandContext(ctx, c.kubectl(), "apply", "-f", "-")
	cmd.Stdin = bytes.NewReader(yamlContent)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl apply: %w\noutput: %s", err, out.String())
	}
	return nil
}

// WaitPodReady polls until a pod matching selector is Running/Ready or the
// timeout expires. Uses `kubectl wait` for efficiency.
func (c *RealClient) WaitPodReady(
	ctx context.Context,
	_ string,
	namespace, selector string,
	timeout time.Duration,
) (podName string, elapsed time.Duration, err error) {
	start := time.Now()

	// kubectl wait exits 0 when any matching pod satisfies the condition.
	_, err = c.run(ctx,
		"wait", "pod",
		"-n", namespace,
		"-l", selector,
		"--for=condition=Ready",
		fmt.Sprintf("--timeout=%ds", int(timeout.Seconds())),
	)
	if err != nil {
		return "", time.Since(start), fmt.Errorf("wait pod ready (ns=%s sel=%s): %w", namespace, selector, err)
	}

	// Retrieve the pod name.
	out, nameErr := c.run(ctx, "get", "pod", "-n", namespace, "-l", selector,
		"-o=jsonpath={.items[0].metadata.name}")
	if nameErr == nil {
		podName = strings.TrimSpace(out)
	}
	return podName, time.Since(start), nil
}

// RunInPod executes cmd inside a running container using kubectl exec.
// container may be empty to use the first/only container.
func (c *RealClient) RunInPod(
	ctx context.Context,
	_ string,
	namespace, pod, container string,
	cmd []string,
) (string, error) {
	args := []string{"exec", "-n", namespace, pod}
	if container != "" {
		args = append(args, "-c", container)
	}
	args = append(args, "--")
	args = append(args, cmd...)
	return c.run(ctx, args...)
}

// GetPodsByLabel returns the names of pods matching the label selector.
func (c *RealClient) GetPodsByLabel(
	ctx context.Context,
	_ string,
	namespace, selector string,
) ([]string, error) {
	out, err := c.run(ctx, "get", "pod", "-n", namespace, "-l", selector,
		"-o=jsonpath={.items[*].metadata.name}")
	if err != nil {
		return nil, err
	}
	names := strings.Fields(strings.TrimSpace(out))
	return names, nil
}

// PodIP returns the IP of the first pod matching the label selector.
func (c *RealClient) PodIP(
	ctx context.Context,
	_ string,
	namespace, selector string,
) (string, error) {
	out, err := c.run(ctx, "get", "pod", "-n", namespace, "-l", selector,
		"-o=jsonpath={.items[0].status.podIP}")
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(out)
	if ip == "" {
		return "", fmt.Errorf("no pod IP for selector %q in namespace %q", selector, namespace)
	}
	return ip, nil
}
