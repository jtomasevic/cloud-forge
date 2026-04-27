package spike_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jtomasevic/cloud-forge/spikes/nats-routing/internal/spike"
)

// TestPassLabel_True verifies the human-readable pass label.
func TestPassLabel_True(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "PASS ✓", spike.PassLabel(true))
}

// TestPassLabel_False verifies the human-readable fail label.
func TestPassLabel_False(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "FAIL ✗", spike.PassLabel(false))
}

// TestPrintResults_AllPass verifies that PrintResults returns true and writes
// "✓ All critical" to the output when Q1, Q2, and Q4 are true.
func TestPrintResults_AllPass(t *testing.T) {
	t.Parallel()

	result := spike.SpikeResult{
		Q1Pass:   true,
		Q1Detail: "tenant-c reachable",
		Q2Pass:   true,
		Q2Detail: "no cross-account leakage",
		Q3Stats: spike.LatencyStats{
			P50:        1 * time.Millisecond,
			P95:        2 * time.Millisecond,
			P99:        3 * time.Millisecond,
			Min:        500 * time.Microsecond,
			Max:        10 * time.Millisecond,
			Throughput: 9000,
		},
		Q3Detail:   "10000 messages published",
		Q4Pass:     true,
		Q4Detail:   "dispatcher worked",
		Q5Pass:     true,
		Q5Duration: 15 * time.Second,
		Q5Detail:   "50 accounts in 15s",
	}

	var buf bytes.Buffer
	ok := spike.PrintResults(result, &buf, testLogger)

	require.True(t, ok, "PrintResults should return true when all critical questions pass")
	output := buf.String()
	assert.Contains(t, output, "PASS ✓")
	assert.Contains(t, output, "✓ All critical spike questions passed.")
	assert.Contains(t, output, "SPIKE 0.6")
	// Q3 latency values should appear.
	assert.Contains(t, output, "1ms")
}

// TestPrintResults_Q1Fail verifies that PrintResults returns false and writes
// "✗ One or more critical" when Q1 is false.
func TestPrintResults_Q1Fail(t *testing.T) {
	t.Parallel()

	result := spike.SpikeResult{
		Q1Pass: false,
		Q2Pass: true,
		Q4Pass: true,
	}

	var buf bytes.Buffer
	ok := spike.PrintResults(result, &buf, testLogger)

	assert.False(t, ok)
	assert.Contains(t, buf.String(), "✗ One or more critical spike questions failed")
}

// TestPrintResults_Q2Fail verifies that a failed Q2 also causes a false return.
func TestPrintResults_Q2Fail(t *testing.T) {
	t.Parallel()

	result := spike.SpikeResult{Q1Pass: true, Q2Pass: false, Q4Pass: true}
	var buf bytes.Buffer
	ok := spike.PrintResults(result, &buf, testLogger)
	assert.False(t, ok)
}

// TestPrintResults_Q4Fail verifies that a failed Q4 also causes a false return.
func TestPrintResults_Q4Fail(t *testing.T) {
	t.Parallel()

	result := spike.SpikeResult{Q1Pass: true, Q2Pass: true, Q4Pass: false}
	var buf bytes.Buffer
	ok := spike.PrintResults(result, &buf, testLogger)
	assert.False(t, ok)
}

// TestPrintResults_P99PassLabel verifies the p99 threshold label is PASS when
// p99 < 5ms and FAIL when p99 ≥ 5ms.
func TestPrintResults_P99PassLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		p99  time.Duration
		want string
	}{
		{4 * time.Millisecond, "PASS ✓"},
		{5 * time.Millisecond, "FAIL ✗"},
		{10 * time.Millisecond, "FAIL ✗"},
	}
	for _, tc := range tests {
		t.Run(tc.p99.String(), func(t *testing.T) {
			t.Parallel()
			result := spike.SpikeResult{
				Q1Pass: true, Q2Pass: true, Q4Pass: true,
				Q3Stats: spike.LatencyStats{P99: tc.p99},
			}
			var buf bytes.Buffer
			spike.PrintResults(result, &buf, testLogger)
			assert.Contains(t, buf.String(), tc.want)
		})
	}
}
