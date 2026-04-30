package probe

import (
	"context"
	"testing"
	"time"
)

func TestCountByVerdict(t *testing.T) {
	results := []TestResult{
		{Verdict: VerdictPass},
		{Verdict: VerdictPass},
		{Verdict: VerdictFail},
		{Verdict: VerdictSkip},
	}
	counts := CountByVerdict(results)
	if counts[VerdictPass] != 2 {
		t.Errorf("PASS count = %d, want 2", counts[VerdictPass])
	}
	if counts[VerdictFail] != 1 {
		t.Errorf("FAIL count = %d, want 1", counts[VerdictFail])
	}
	if counts[VerdictSkip] != 1 {
		t.Errorf("SKIP count = %d, want 1", counts[VerdictSkip])
	}
}

func TestAllPassed_True(t *testing.T) {
	results := []TestResult{
		{Verdict: VerdictPass},
		{Verdict: VerdictSkip},
	}
	if !AllPassed(results) {
		t.Error("AllPassed should return true for PASS+SKIP results")
	}
}

func TestAllPassed_False(t *testing.T) {
	results := []TestResult{
		{Verdict: VerdictPass},
		{Verdict: VerdictFail},
	}
	if AllPassed(results) {
		t.Error("AllPassed should return false when any result is FAIL")
	}
}

func TestOverallVerdict_Pass(t *testing.T) {
	results := []TestResult{{Verdict: VerdictPass}, {Verdict: VerdictSkip}}
	if got := OverallVerdict(results); got != VerdictPass {
		t.Errorf("OverallVerdict = %s, want PASS", got)
	}
}

func TestOverallVerdict_Fail(t *testing.T) {
	results := []TestResult{{Verdict: VerdictFail}}
	if got := OverallVerdict(results); got != VerdictFail {
		t.Errorf("OverallVerdict = %s, want FAIL", got)
	}
}

func TestOverallVerdict_AllSkip(t *testing.T) {
	results := []TestResult{{Verdict: VerdictSkip}}
	if got := OverallVerdict(results); got != VerdictSkip {
		t.Errorf("OverallVerdict = %s, want SKIP", got)
	}
}

func TestRunOne_UnknownTest(t *testing.T) {
	ctx := context.Background()
	fc := &FakeClient{}
	r := runOne(ctx, fc, DefaultConfig(), TestName("unknown"))
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL for unknown test name, got %s", r.Verdict)
	}
}

func TestRunAll_ReturnsAllResults(t *testing.T) {
	// RunAll calls all 5 tests via runOne. Configure FakeClient to return
	// blocked-curl responses so most paths complete quickly.
	fc := &FakeClient{
		WaitPodReadyPodName: "echo",
		PodIPResult:         "10.0.0.5",
		// GetPodsByLabel for policy trace (cilium pod lookup)
		PodsByLabel: []string{"cilium-abc"},
		// RunInPod:
		//  calls 1+ for each test; last entry is reused
		RunInPodResponses: []RunInPodResponse{
			{Output: "curl: (28) Operation timed out", Err: errFake("exit 28")}, // deny
			{Output: "pong\n", Err: nil},                                        // allow
			{Output: "curl: (28) Operation timed out", Err: errFake("exit 28")}, // platform
			{Output: "Final verdict: DENIED\n", Err: nil},                       // trace
			// coexist: depends on vcluster binary — may FAIL or SKIP
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results := RunAll(ctx, fc, DefaultConfig())
	if len(results) != len(allTests) {
		t.Errorf("RunAll returned %d results, want %d", len(results), len(allTests))
	}
	// Verify each result has the expected TestName.
	for i, r := range results {
		if r.Name != allTests[i] {
			t.Errorf("results[%d].Name = %s, want %s", i, r.Name, allTests[i])
		}
	}
}

func TestAllTests_Length(t *testing.T) {
	got := AllTests()
	if len(got) != 5 {
		t.Errorf("AllTests() length = %d, want 5", len(got))
	}
}
