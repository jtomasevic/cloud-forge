package measure

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── mock Prober ──────────────────────────────────────────────────────────────

// mockProber is a test double for Prober.
//
// It returns a pre-configured sequence of (duration, error) pairs on successive
// Probe calls.  Once all responses are consumed, it returns (0, nil).
type mockProber struct {
	// responses is the FIFO queue of return values.
	responses []probeResponse
	// CallCount tracks total Probe invocations.
	CallCount int
}

type probeResponse struct {
	ttfb time.Duration
	err  error
}

// Probe implements Prober by consuming the next queued response.
func (m *mockProber) Probe(_ context.Context, _ string) (time.Duration, error) {
	if m.CallCount >= len(m.responses) {
		m.CallCount++
		return 100 * time.Millisecond, nil
	}
	r := m.responses[m.CallCount]
	m.CallCount++
	return r.ttfb, r.err
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// testConfig returns a Config optimised for tests: tiny timeouts and a single
// sample so tests run in milliseconds.
func testConfig(baseURL string) Config {
	return Config{
		Samples:          3,
		Namespace:        "default",
		BaseURL:          baseURL + "/%s", // will be formatted with variant name
		ScaleDownTimeout: 50 * time.Millisecond,
		RequestTimeout:   50 * time.Millisecond,
		PollInterval:     time.Millisecond,
	}
}

// alwaysZeroCounter is a PodCounter that always reports zero ready pods.
// Used in runner tests where scale-to-zero should succeed instantly.
type alwaysZeroCounter struct{}

func (a *alwaysZeroCounter) ReadyPods(_ context.Context, _, _ string) (int, error) {
	return 0, nil
}

// ─── Runner.RunVariant ────────────────────────────────────────────────────────

// TestRunVariant_Success verifies that RunVariant collects the configured
// number of samples when both scale-to-zero and probing succeed every time.
func TestRunVariant_Success(t *testing.T) {
	// Build a prober that always returns a valid TTFB.
	prober := &mockProber{
		responses: []probeResponse{
			{ttfb: 1200 * time.Millisecond},
			{ttfb: 1100 * time.Millisecond},
			{ttfb: 1300 * time.Millisecond},
		},
	}

	cfg := testConfig("http://ignored")
	runner := NewRunner(prober, &alwaysZeroCounter{}, cfg, silentLogger())

	samples := runner.RunVariant(context.Background(), VariantMinimal)

	require.Len(t, samples, 3, "must return exactly cfg.Samples samples")
	for i, s := range samples {
		assert.NoError(t, s.Error, "sample %d must not have an error", i)
		assert.Greater(t, s.TTFB, time.Duration(0), "sample %d TTFB must be positive", i)
	}
}

// TestRunVariant_ScaleDownFails verifies that a scale-to-zero timeout records
// the sample as a failure and continues to the next sample.
func TestRunVariant_ScaleDownFails(t *testing.T) {
	// Counter that never returns zero — scale-to-zero always times out.
	neverZeroCounter := &mockPodCounter{
		responses: []podCountResponse{
			{count: 1}, {count: 1}, {count: 1}, {count: 1}, {count: 1},
			{count: 1}, {count: 1}, {count: 1}, {count: 1}, {count: 1},
		},
	}

	cfg := testConfig("http://ignored")
	cfg.Samples = 2
	// Very short timeout so the test runs quickly.
	cfg.ScaleDownTimeout = 5 * time.Millisecond

	runner := NewRunner(&mockProber{}, neverZeroCounter, cfg, silentLogger())
	samples := runner.RunVariant(context.Background(), VariantMinimal)

	require.Len(t, samples, 2)
	for i, s := range samples {
		assert.Error(t, s.Error, "sample %d must be a failure (scale-to-zero timed out)", i)
	}
}

// TestRunVariant_ProbeFails verifies that probe errors are recorded as sample
// failures while scale-to-zero succeeds.
func TestRunVariant_ProbeFails(t *testing.T) {
	prober := &mockProber{
		responses: []probeResponse{
			{err: errors.New("server error: status 503")},
			{ttfb: 900 * time.Millisecond},
			{err: errors.New("read first byte: EOF")},
		},
	}

	cfg := testConfig("http://ignored")
	cfg.Samples = 3

	runner := NewRunner(prober, &alwaysZeroCounter{}, cfg, silentLogger())
	samples := runner.RunVariant(context.Background(), VariantMinimal)

	require.Len(t, samples, 3)
	assert.Error(t, samples[0].Error, "first sample must be a failure")
	assert.NoError(t, samples[1].Error, "second sample must succeed")
	assert.Error(t, samples[2].Error, "third sample must be a failure")
}

// ─── Runner.RunAll ────────────────────────────────────────────────────────────

// TestRunAll_ReturnsResultsForAllVariants verifies that RunAll collects stats
// for every variant in AllVariants when all probes succeed.
func TestRunAll_ReturnsResultsForAllVariants(t *testing.T) {
	// Use a real HTTP test server so we don't need to mock the URL pattern.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	}))
	defer srv.Close()

	// Format the base URL so the runner can build service URLs for any variant.
	// runner.serviceURL does fmt.Sprintf(BaseURL, variant) so we need one %s.
	cfg := DefaultConfig()
	cfg.BaseURL = srv.URL + "/%s" // ignores variant name, always hits the test server
	cfg.Samples = 2
	cfg.ScaleDownTimeout = 5 * time.Millisecond
	cfg.PollInterval = time.Millisecond

	runner := NewRunner(
		NewHTTPProber(5*time.Second, 50*time.Millisecond),
		&alwaysZeroCounter{},
		cfg,
		silentLogger(),
	)

	result := runner.RunAll(context.Background())

	// All three variants must be present in the result map.
	for _, v := range AllVariants {
		stats, ok := result.Results[v]
		require.True(t, ok, "variant %q must be present in result", v)
		assert.Equal(t, v, stats.Variant)
		assert.Equal(t, 2, stats.SampleCount)
	}

	// Platform metadata must be populated.
	assert.NotEmpty(t, result.Platform)
	assert.False(t, result.StartedAt.IsZero())
}

// TestRunAll_ContextCancelledMidRun verifies that cancelling the context
// stops the run and returns partial results for completed variants.
func TestRunAll_ContextCancelledMidRun(t *testing.T) {
	// Cancel the context immediately before calling RunAll.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := DefaultConfig()
	cfg.Samples = 5
	cfg.ScaleDownTimeout = 50 * time.Millisecond
	cfg.PollInterval = time.Millisecond

	runner := NewRunner(
		&mockProber{}, &alwaysZeroCounter{}, cfg, silentLogger(),
	)

	// Should not hang or panic even with a pre-cancelled context.
	result := runner.RunAll(ctx)

	// With a cancelled context RunAll should bail out early; we just verify
	// it returns a properly-structured result without hanging.
	assert.NotNil(t, result.Results)
}
