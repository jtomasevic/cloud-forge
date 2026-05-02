package probe

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ── isCiliumInstalled ─────────────────────────────────────────────────────────

func TestIsCiliumInstalled_Present(t *testing.T) {
	fc := &FakeClient{PodsByLabel: []string{"cilium-abc123"}}
	ctx := context.Background()
	got, err := isCiliumInstalled(ctx, fc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("expected Cilium to be detected")
	}
}

func TestIsCiliumInstalled_Absent(t *testing.T) {
	fc := &FakeClient{PodsByLabel: nil}
	ctx := context.Background()
	got, err := isCiliumInstalled(ctx, fc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("expected Cilium to be absent")
	}
}

func TestIsCiliumInstalled_ErrorTreatedAsAbsent(t *testing.T) {
	fc := &FakeClient{PodsByLabelErr: errors.New("network error")}
	ctx := context.Background()
	got, err := isCiliumInstalled(ctx, fc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("expected false when GetPodsByLabel errors")
	}
}

// ── truncate ──────────────────────────────────────────────────────────────────

func TestTruncate(t *testing.T) {
	cases := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello…"},
		{"", 10, ""},
		{"abcde", 5, "abcde"},
		{"abcdef", 5, "abcde…"},
	}
	for _, c := range cases {
		got := truncate(c.input, c.maxLen)
		if got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.input, c.maxLen, got, c.want)
		}
	}
}

// ── RunTestCiliumEnforcement ──────────────────────────────────────────────────

func TestRunTestCiliumEnforcement_Skip_NoCilium(t *testing.T) {
	fc := &FakeClient{PodsByLabel: nil} // no Cilium pods

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestCiliumEnforcement(ctx, fc, DefaultConfig(), "vcluster-tenant-a", "vcluster-tenant-b")
	if r.Verdict != VerdictSkip {
		t.Errorf("expected SKIP when Cilium absent, got %s: %s", r.Verdict, r.Evidence)
	}
}

func TestRunTestCiliumEnforcement_Pass_ConnectionBlocked(t *testing.T) {
	fc := &FakeClient{
		PodsByLabel:         []string{"cilium-pod"}, // Cilium detected
		WaitPodReadyPodName: "cilium-probe",
		PodIPResult:         "10.96.5.100",
		RunInPodResponses: []RunInPodResponse{
			{Output: "nc: connection refused"}, // Cilium blocks it
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestCiliumEnforcement(ctx, fc, DefaultConfig(), "vcluster-tenant-a", "vcluster-tenant-b")
	if r.Verdict != VerdictPass {
		t.Errorf("expected PASS when connection blocked, got %s: %s", r.Verdict, r.Evidence)
	}
}

func TestRunTestCiliumEnforcement_Fail_ConnectionSucceeded(t *testing.T) {
	fc := &FakeClient{
		PodsByLabel:         []string{"cilium-pod"},
		WaitPodReadyPodName: "cilium-probe",
		PodIPResult:         "10.96.5.100",
		RunInPodResponses: []RunInPodResponse{
			{Output: "10.96.5.100 (10.96.5.100:8080) succeeded"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := RunTestCiliumEnforcement(ctx, fc, DefaultConfig(), "vcluster-tenant-a", "vcluster-tenant-b")
	if r.Verdict != VerdictFail {
		t.Errorf("expected FAIL when connection succeeds, got %s", r.Verdict)
	}
}
