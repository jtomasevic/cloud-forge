package probe

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// FakeClient — configurable test double for KubectlClient
// ──────────────────────────────────────────────────────────────────────────────

// RunInPodResponse captures the scripted response for a RunInPod call.
type RunInPodResponse struct {
	Output string
	Err    error
}

// FakeClient is a simple, configurable test double that satisfies KubectlClient.
// Each method records calls and returns pre-configured responses.
//
// Usage in tests:
//
//	fc := &FakeClient{}
//	fc.RunInPodResponses = []RunInPodResponse{{Output: "nc: connect refused"}}
//	result := RunTestNetworkIsolation(ctx, fc, cfg)
type FakeClient struct {
	// ── RunInPod ────────────────────────────────────────────────────────────
	RunInPodResponses []RunInPodResponse // consumed FIFO; last entry is reused
	RunInPodCalls     []RunInPodCall

	// ── Apply ───────────────────────────────────────────────────────────────
	ApplyErr   error
	ApplyCalls int

	// ── WaitPodReady ────────────────────────────────────────────────────────
	WaitPodReadyPodName string
	WaitPodReadyElapsed time.Duration
	WaitPodReadyErr     error
	WaitPodReadyCalls   int

	// ── DeletePod ───────────────────────────────────────────────────────────
	DeletePodErr   error
	DeletePodCalls []string // pod names

	// ── GetPodResources ─────────────────────────────────────────────────────
	PodResources    []PodResource
	PodResourcesErr error

	// ── GetPodsByLabel ──────────────────────────────────────────────────────
	PodsByLabel    []string
	PodsByLabelErr error

	// ── PodIP ───────────────────────────────────────────────────────────────
	PodIPResult string
	PodIPErr    error
}

// RunInPodCall records the arguments of a single RunInPod invocation.
type RunInPodCall struct {
	KubeconfigPath string
	Namespace      string
	Pod            string
	Container      string
	Cmd            []string
}

// compile-time interface check
var _ KubectlClient = (*FakeClient)(nil)

// RunInPod pops the next response from RunInPodResponses (reuses the last one
// when the list is exhausted) and records the call.
func (f *FakeClient) RunInPod(
	_ context.Context,
	kubeconfigPath, namespace, pod, container string,
	cmd []string,
) (string, error) {
	f.RunInPodCalls = append(f.RunInPodCalls, RunInPodCall{
		KubeconfigPath: kubeconfigPath,
		Namespace:      namespace,
		Pod:            pod,
		Container:      container,
		Cmd:            append([]string(nil), cmd...),
	})
	if len(f.RunInPodResponses) == 0 {
		return "", fmt.Errorf("fake: no RunInPod response configured")
	}
	idx := len(f.RunInPodCalls) - 1
	if idx >= len(f.RunInPodResponses) {
		idx = len(f.RunInPodResponses) - 1
	}
	r := f.RunInPodResponses[idx]
	return r.Output, r.Err
}

// Apply records the call and returns the pre-configured error.
func (f *FakeClient) Apply(_ context.Context, _ string, _ []byte) error {
	f.ApplyCalls++
	return f.ApplyErr
}

// WaitPodReady returns the pre-configured pod name, elapsed time, and error.
func (f *FakeClient) WaitPodReady(
	_ context.Context,
	_, _, _ string,
	_ time.Duration,
) (string, time.Duration, error) {
	f.WaitPodReadyCalls++
	return f.WaitPodReadyPodName, f.WaitPodReadyElapsed, f.WaitPodReadyErr
}

// DeletePod records the pod name and returns the pre-configured error.
func (f *FakeClient) DeletePod(_ context.Context, _, _, pod string) error {
	f.DeletePodCalls = append(f.DeletePodCalls, pod)
	return f.DeletePodErr
}

// GetPodResources returns the pre-configured slice.
func (f *FakeClient) GetPodResources(_ context.Context, _, _, _ string) ([]PodResource, error) {
	return f.PodResources, f.PodResourcesErr
}

// GetPodsByLabel returns the pre-configured slice.
func (f *FakeClient) GetPodsByLabel(_ context.Context, _, _, _ string) ([]string, error) {
	return f.PodsByLabel, f.PodsByLabelErr
}

// PodIP returns the pre-configured IP.
func (f *FakeClient) PodIP(_ context.Context, _, _, _ string) (string, error) {
	return f.PodIPResult, f.PodIPErr
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers used in multiple test files
// ──────────────────────────────────────────────────────────────────────────────

// hasSubstr reports whether s contains sub.
func hasSubstr(s, sub string) bool {
	return strings.Contains(s, sub)
}

// newPassResult builds a passing TestResult for test helper assertions.
func newPassResult(name TestName, evidence string) TestResult {
	return TestResult{
		Name:     name,
		Verdict:  VerdictPass,
		Evidence: evidence,
		Metrics:  map[string]string{},
	}
}

// newFailResult builds a failing TestResult for test helper assertions.
func newFailResult(name TestName, evidence string) TestResult {
	return TestResult{
		Name:     name,
		Verdict:  VerdictFail,
		Evidence: evidence,
		Metrics:  map[string]string{},
	}
}
