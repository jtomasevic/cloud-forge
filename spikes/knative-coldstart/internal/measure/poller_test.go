package measure

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── mock PodCounter ──────────────────────────────────────────────────────────

// mockPodCounter is a test double for PodCounter that returns a pre-configured
// sequence of (count, error) pairs on successive ReadyPods calls.
//
// Once all configured responses are consumed, it returns (0, nil) — meaning
// the service has scaled to zero.  This avoids panics in tests that call
// ReadyPods more times than expected.
type mockPodCounter struct {
	// responses is a FIFO queue of return values.
	responses []podCountResponse
	// callCount tracks how many times ReadyPods has been called.
	CallCount int
}

// podCountResponse is a single (count, error) pair returned by the mock.
type podCountResponse struct {
	count int
	err   error
}

// ReadyPods implements PodCounter by consuming the next queued response.
func (m *mockPodCounter) ReadyPods(_ context.Context, _, _ string) (int, error) {
	if m.CallCount >= len(m.responses) {
		// Default: already at zero — safe fallback for tests that over-poll.
		m.CallCount++
		return 0, nil
	}
	r := m.responses[m.CallCount]
	m.CallCount++
	return r.count, r.err
}

// silentLogger returns a slog.Logger that discards all output, keeping test
// output clean without losing the ability to call logger methods.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ─── WaitForScaleToZero tests ────────────────────────────────────────────────

// TestWaitForScaleToZero_ImmediateZero verifies that the function returns nil
// immediately when the pod count is already zero on the first poll.
func TestWaitForScaleToZero_ImmediateZero(t *testing.T) {
	counter := &mockPodCounter{responses: []podCountResponse{{count: 0}}}

	err := WaitForScaleToZero(
		context.Background(), counter,
		"fn-minimal", "default",
		time.Millisecond, // negligible poll interval for tests
		silentLogger(),
	)

	require.NoError(t, err)
	assert.Equal(t, 1, counter.CallCount, "should poll exactly once")
}

// TestWaitForScaleToZero_ScalesDown verifies that the poller waits correctly
// through multiple rounds of positive pod counts before reaching zero.
func TestWaitForScaleToZero_ScalesDown(t *testing.T) {
	// Simulate: 2 pods → 1 pod → 0 pods.
	counter := &mockPodCounter{responses: []podCountResponse{
		{count: 2},
		{count: 1},
		{count: 0},
	}}

	err := WaitForScaleToZero(
		context.Background(), counter,
		"fn-medium", "default",
		time.Millisecond,
		silentLogger(),
	)

	require.NoError(t, err)
	assert.Equal(t, 3, counter.CallCount, "should poll three times before reaching zero")
}

// TestWaitForScaleToZero_ContextCancelled verifies that a cancelled context
// causes the function to return the context error promptly.
func TestWaitForScaleToZero_ContextCancelled(t *testing.T) {
	// Pod count never reaches zero.
	counter := &mockPodCounter{responses: []podCountResponse{
		{count: 1},
		{count: 1},
		{count: 1},
		{count: 1},
		{count: 1},
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	err := WaitForScaleToZero(
		ctx, counter,
		"fn-heavy", "default",
		// Use a very short poll interval so we exercise the select path
		// (poll → sleep → ctx cancelled) in a reasonable test duration.
		2*time.Millisecond,
		silentLogger(),
	)

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled),
		"error must be a context error, got: %v", err)
}

// TestWaitForScaleToZero_CounterError verifies that an error from ReadyPods
// is propagated immediately without further polling.
func TestWaitForScaleToZero_CounterError(t *testing.T) {
	counter := &mockPodCounter{responses: []podCountResponse{
		{count: 0, err: errors.New("kubectl: connection refused")},
	}}

	err := WaitForScaleToZero(
		context.Background(), counter,
		"fn-minimal", "default",
		time.Millisecond,
		silentLogger(),
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "kubectl: connection refused")
	// Must not retry after the error.
	assert.Equal(t, 1, counter.CallCount)
}
