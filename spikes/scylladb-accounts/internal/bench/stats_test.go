package bench

import (
	"sort"
	"testing"
	"time"
)

// ─── Percentile ───────────────────────────────────────────────────────────────

func TestPercentile_Empty(t *testing.T) {
	t.Parallel()
	if got := Percentile(Samples{}, 50); got != 0 {
		t.Fatalf("expected 0 for empty samples, got %v", got)
	}
}

func TestPercentile_SingleElement(t *testing.T) {
	t.Parallel()
	s := Samples{5 * time.Millisecond}
	for _, p := range []float64{0, 50, 99, 100} {
		if got := Percentile(s, p); got != 5*time.Millisecond {
			t.Errorf("p%.0f: want 5ms, got %v", p, got)
		}
	}
}

func TestPercentile_KnownValues(t *testing.T) {
	t.Parallel()
	// 100 samples: 1ms, 2ms, …, 100ms
	s := make(Samples, 100)
	for i := range s {
		s[i] = time.Duration(i+1) * time.Millisecond
	}

	cases := []struct {
		p    float64
		want time.Duration
	}{
		{50, 50 * time.Millisecond},
		{95, 95 * time.Millisecond},
		{99, 99 * time.Millisecond},
		{100, 100 * time.Millisecond},
	}

	for _, tc := range cases {
		// Percentile sorts in place; provide a fresh copy each time.
		cp := make(Samples, len(s))
		copy(cp, s)
		got := Percentile(cp, tc.p)
		if got != tc.want {
			t.Errorf("p%.0f: want %v, got %v", tc.p, tc.want, got)
		}
	}
}

func TestPercentile_UnsortedInput(t *testing.T) {
	t.Parallel()
	// Input deliberately out of order.
	s := Samples{10 * time.Millisecond, 1 * time.Millisecond, 5 * time.Millisecond}
	// After sorting: [1ms, 5ms, 10ms]
	// p99 → rank = ceil(0.99 * 3) = ceil(2.97) = 3 → index 2 → 10ms
	got := Percentile(s, 99)
	if got != 10*time.Millisecond {
		t.Errorf("p99 want 10ms, got %v", got)
	}
}

// ─── MinDuration / MaxDuration ────────────────────────────────────────────────

func TestMinMaxDuration_Empty(t *testing.T) {
	t.Parallel()
	if got := MinDuration(Samples{}); got != 0 {
		t.Fatalf("MinDuration empty: want 0, got %v", got)
	}
	if got := MaxDuration(Samples{}); got != 0 {
		t.Fatalf("MaxDuration empty: want 0, got %v", got)
	}
}

func TestMinMaxDuration_Values(t *testing.T) {
	t.Parallel()
	s := Samples{3 * time.Millisecond, 1 * time.Millisecond, 7 * time.Millisecond, 2 * time.Millisecond}
	if got := MinDuration(s); got != 1*time.Millisecond {
		t.Errorf("min: want 1ms, got %v", got)
	}
	if got := MaxDuration(s); got != 7*time.Millisecond {
		t.Errorf("max: want 7ms, got %v", got)
	}
}

// ─── BuildResult ──────────────────────────────────────────────────────────────

func TestBuildResult_Percentiles(t *testing.T) {
	t.Parallel()
	s := make(Samples, 100)
	for i := range s {
		s[i] = time.Duration(i+1) * time.Millisecond
	}
	// Sort a separate copy to avoid mutating the test data.
	sorted := make(Samples, len(s))
	copy(sorted, s)
	sort.Sort(sorted)

	r := BuildResult(BenchAPIKeyQuorum, s, 0, 200*time.Millisecond)

	if r.Ops != 100 {
		t.Errorf("Ops: want 100, got %d", r.Ops)
	}
	if r.P50 != 50*time.Millisecond {
		t.Errorf("P50: want 50ms, got %v", r.P50)
	}
	if r.P99 != 99*time.Millisecond {
		t.Errorf("P99: want 99ms, got %v", r.P99)
	}
	if r.Min != 1*time.Millisecond {
		t.Errorf("Min: want 1ms, got %v", r.Min)
	}
	if r.Max != 100*time.Millisecond {
		t.Errorf("Max: want 100ms, got %v", r.Max)
	}
	if r.Errors != 0 {
		t.Errorf("Errors: want 0, got %d", r.Errors)
	}
	if r.TotalDuration != 200*time.Millisecond {
		t.Errorf("TotalDuration: want 200ms, got %v", r.TotalDuration)
	}
}

func TestBuildResult_Errors(t *testing.T) {
	t.Parallel()
	r := BuildResult(BenchAPIKeyOne, Samples{1 * time.Millisecond}, 5, time.Second)
	if r.Errors != 5 {
		t.Errorf("want 5 errors, got %d", r.Errors)
	}
}

// ─── Throughput ───────────────────────────────────────────────────────────────

func TestResult_Throughput_Normal(t *testing.T) {
	t.Parallel()
	r := Result{Ops: 1000, TotalDuration: time.Second}
	if got := r.Throughput(); got != 1000.0 {
		t.Errorf("want 1000 ops/s, got %.2f", got)
	}
}

func TestResult_Throughput_ZeroDuration(t *testing.T) {
	t.Parallel()
	r := Result{Ops: 100, TotalDuration: 0}
	if got := r.Throughput(); got != 0.0 {
		t.Errorf("want 0 for zero duration, got %.2f", got)
	}
}

// ─── LWTResult.Correct ────────────────────────────────────────────────────────

func TestLWTResult_Correct(t *testing.T) {
	t.Parallel()
	cases := []struct {
		winners int
		want    bool
	}{
		{1, true},
		{0, false},
		{2, false},
	}
	for _, tc := range cases {
		r := LWTResult{Winners: tc.winners}
		if got := r.Correct(); got != tc.want {
			t.Errorf("winners=%d: Correct()=%v, want %v", tc.winners, got, tc.want)
		}
	}
}

// ─── Samples sort interface ───────────────────────────────────────────────────

func TestSamples_SortInterface(t *testing.T) {
	t.Parallel()
	s := Samples{3 * time.Millisecond, 1 * time.Millisecond, 2 * time.Millisecond}
	sort.Sort(s)
	expected := Samples{1 * time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond}
	for i, got := range s {
		if got != expected[i] {
			t.Errorf("index %d: got %v, want %v", i, got, expected[i])
		}
	}
}

// ─── DefaultConfig ────────────────────────────────────────────────────────────

func TestDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()

	if len(cfg.Hosts) == 0 {
		t.Error("Hosts must not be empty")
	}
	if cfg.Port == 0 {
		t.Error("Port must not be zero")
	}
	if cfg.Keyspace == "" {
		t.Error("Keyspace must not be empty")
	}
	if cfg.Ops <= 0 {
		t.Error("Ops must be positive")
	}
	if cfg.Concurrency <= 0 {
		t.Error("Concurrency must be positive")
	}
	if cfg.LWTWriters <= 0 {
		t.Error("LWTWriters must be positive")
	}
	if cfg.ConnectTimeout <= 0 {
		t.Error("ConnectTimeout must be positive")
	}
	if cfg.QueryTimeout <= 0 {
		t.Error("QueryTimeout must be positive")
	}
}
