package measure

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// PodCounter queries the number of running pods that back a Knative Service.
//
// Implementations are injected into WaitForScaleToZero so the poll logic can
// be tested without a real Kubernetes cluster.
type PodCounter interface {
	// ReadyPods returns the number of pods in Running phase that are owned by
	// the given Knative service in the specified namespace.
	//
	// Returns 0 and nil when the service has fully scaled to zero.
	// Returns a non-nil error only for infrastructure failures (kubectl not
	// found, API server unreachable, RBAC denied).
	ReadyPods(ctx context.Context, service, namespace string) (int, error)
}

// KubectlPodCounter is the production PodCounter implementation.
//
// It shells out to kubectl to query the live Kubernetes API.  This avoids
// adding the heavy k8s.io/client-go dependency in a spike context.
//
// NOTE: In production CF-FunctionTrigger code, replace this with a proper
// informer-based implementation using k8s.io/client-go/tools/informers.
type KubectlPodCounter struct{}

// ReadyPods calls kubectl to count pods labelled with the Knative service name
// that are currently in the Running phase.
//
// Knative attaches the label "serving.knative.dev/service=<name>" to all pods
// it creates, making the selector unambiguous even in a shared namespace.
func (k *KubectlPodCounter) ReadyPods(ctx context.Context, service, namespace string) (int, error) {
	// Build a label selector that matches exactly the pods owned by this Knative Service.
	// Knative guarantees this label is set on every pod it creates.
	selector := fmt.Sprintf("serving.knative.dev/service=%s", service)

	cmd := exec.CommandContext(ctx,
		"kubectl", "get", "pods",
		"-n", namespace,
		"-l", selector,
		"--field-selector=status.phase=Running",
		"--no-headers",
		"-o", "name",
	)

	// Capture both stdout (pod names) and stderr (error messages).
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("kubectl get pods (service=%s, ns=%s): %w — %s",
			service, namespace, err, strings.TrimSpace(errBuf.String()))
	}

	// Count non-empty lines; each line is one pod name ("pod/fn-minimal-xxxx").
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count, nil
}

// WaitForScaleToZero polls counter.ReadyPods every pollInterval until the pod
// count for service in namespace reaches zero, or ctx is cancelled.
//
// The function polls immediately on entry — if the service is already at zero
// pods (e.g. between benchmark rounds) it returns nil without sleeping.
//
// Progress is logged at INFO level so benchmark operators can follow the
// countdown in their terminal.
//
// Returns:
//   - nil when the pod count reaches zero.
//   - ctx.Err() when the context deadline is exceeded or cancelled.
//   - an error from counter.ReadyPods on infrastructure failures.
func WaitForScaleToZero(
	ctx context.Context,
	counter PodCounter,
	service, namespace string,
	pollInterval time.Duration,
	logger *slog.Logger,
) error {
	logger.Info("waiting for scale-to-zero",
		"service", service,
		"namespace", namespace,
		"poll_interval_s", pollInterval.Seconds(),
	)

	for {
		// Always poll first so a service that is already at zero returns immediately.
		count, err := counter.ReadyPods(ctx, service, namespace)
		if err != nil {
			return fmt.Errorf("poll ready pods for %s/%s: %w", namespace, service, err)
		}

		logger.Info("pod count check",
			"service", service,
			"ready_pods", strconv.Itoa(count),
		)

		if count == 0 {
			logger.Info("scale-to-zero confirmed", "service", service)
			return nil
		}

		// Wait for the next poll tick, but respect context cancellation so the
		// benchmark tool responds promptly to Ctrl-C or timeouts.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
			// Fall through to next poll.
		}
	}
}
