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

// FakeClient is a configurable test double that satisfies KubectlClient.
// Each method records calls and returns pre-configured responses.
type FakeClient struct {
	// ── Apply ─────────────────────────────────────────────────────────────
	ApplyErr   error
	ApplyCalls int

	// ── WaitPodReady ──────────────────────────────────────────────────────
	WaitPodReadyPodName string
	WaitPodReadyElapsed time.Duration
	WaitPodReadyErr     error
	WaitPodReadyCalls   int

	// ── RunInPod ──────────────────────────────────────────────────────────
	// Consumed FIFO; last entry is reused when the list is exhausted.
	RunInPodResponses []RunInPodResponse
	RunInPodCalls     []RunInPodCall

	// ── GetPodsByLabel ────────────────────────────────────────────────────
	PodsByLabel    []string
	PodsByLabelErr error

	// ── PodIP ─────────────────────────────────────────────────────────────
	PodIPResult string
	PodIPErr    error
}

// RunInPodCall records arguments of a single RunInPod invocation for assertion.
type RunInPodCall struct {
	Namespace string
	Pod       string
	Container string
	Cmd       []string
}

// Compile-time interface check.
var _ KubectlClient = (*FakeClient)(nil)

// Apply records the call and returns ApplyErr.
func (f *FakeClient) Apply(_ context.Context, _ string, _ []byte) error {
	f.ApplyCalls++
	return f.ApplyErr
}

// WaitPodReady returns the pre-configured pod name, elapsed, and error.
func (f *FakeClient) WaitPodReady(
	_ context.Context, _, _, _ string, _ time.Duration,
) (string, time.Duration, error) {
	f.WaitPodReadyCalls++
	return f.WaitPodReadyPodName, f.WaitPodReadyElapsed, f.WaitPodReadyErr
}

// RunInPod pops the next response from RunInPodResponses (reuses the last one)
// and records the call.
func (f *FakeClient) RunInPod(
	_ context.Context,
	_, namespace, pod, container string,
	cmd []string,
) (string, error) {
	f.RunInPodCalls = append(f.RunInPodCalls, RunInPodCall{
		Namespace: namespace,
		Pod:       pod,
		Container: container,
		Cmd:       append([]string(nil), cmd...),
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

// GetPodsByLabel returns the pre-configured slice.
func (f *FakeClient) GetPodsByLabel(_ context.Context, _, _, _ string) ([]string, error) {
	return f.PodsByLabel, f.PodsByLabelErr
}

// PodIP returns the pre-configured IP.
func (f *FakeClient) PodIP(_ context.Context, _, _, _ string) (string, error) {
	return f.PodIPResult, f.PodIPErr
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers shared across test files
// ──────────────────────────────────────────────────────────────────────────────

// hasSubstr reports whether s contains sub. Used in test assertions.
func hasSubstr(s, sub string) bool {
	return strings.Contains(s, sub)
}
