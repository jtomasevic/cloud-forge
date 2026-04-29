package cluster

import (
	"testing"

	"github.com/cloud-forge/spikes/tenant-isolation/internal/probe"
)

// ── parsePodNameFromWait ──────────────────────────────────────────────────────

func TestParsePodNameFromWait(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{
			input: "pod/echo-server-abc123 condition met",
			want:  "echo-server-abc123",
		},
		{
			input: "pod/vcluster-0 condition met\n",
			want:  "vcluster-0",
		},
		{
			input: "some other output without pod/",
			want:  "",
		},
		{
			input: "",
			want:  "",
		},
		{
			input: "pod/ condition met", // edge: empty name after slash
			want:  "",
		},
	}
	for _, c := range cases {
		got := parsePodNameFromWait(c.input)
		if got != c.want {
			t.Errorf("parsePodNameFromWait(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// ── parsePodResources ─────────────────────────────────────────────────────────

func TestParsePodResources(t *testing.T) {
	input := `
vcluster-tenant-a-0   25m   128Mi
vcluster-tenant-a-etcd-0   10m   64Mi
coredns-abc   5m   32Mi
`
	got := parsePodResources(input)
	if len(got) != 3 {
		t.Fatalf("expected 3 resources, got %d", len(got))
	}
	assertPod(t, got[0], "vcluster-tenant-a-0", 25, 128)
	assertPod(t, got[1], "vcluster-tenant-a-etcd-0", 10, 64)
	assertPod(t, got[2], "coredns-abc", 5, 32)
}

func TestParsePodResources_Empty(t *testing.T) {
	got := parsePodResources("")
	if len(got) != 0 {
		t.Errorf("expected 0 resources on empty input, got %d", len(got))
	}
}

func TestParsePodResources_MalformedLines(t *testing.T) {
	input := "only-one-field\ntwo fields\nname cpu mem extra"
	got := parsePodResources(input)
	// "name cpu mem extra" has 4 fields — valid; others are skipped
	if len(got) != 1 {
		t.Errorf("expected 1 valid resource, got %d: %v", len(got), got)
	}
}

func assertPod(t *testing.T, got probe.PodResource, name string, cpu, mem int64) {
	t.Helper()
	if got.Name != name {
		t.Errorf("pod name: got %q, want %q", got.Name, name)
	}
	if got.CPUMilli != cpu {
		t.Errorf("pod %q CPUMilli: got %d, want %d", name, got.CPUMilli, cpu)
	}
	if got.MemMB != mem {
		t.Errorf("pod %q MemMB: got %d, want %d", name, got.MemMB, mem)
	}
}

// ── kubeconfigArgs ────────────────────────────────────────────────────────────

func TestKubeconfigArgs(t *testing.T) {
	c := &RealClient{}
	if got := c.kubeconfigArgs(""); len(got) != 0 {
		t.Errorf("expected empty args for empty kubeconfig, got %v", got)
	}
	args := c.kubeconfigArgs("/tmp/kc.yaml")
	if len(args) != 2 || args[0] != "--kubeconfig" || args[1] != "/tmp/kc.yaml" {
		t.Errorf("unexpected kubeconfig args: %v", args)
	}
}

// ── NewRealClient / kubectl() ─────────────────────────────────────────────────

func TestNewRealClient_DefaultBin(t *testing.T) {
	c := NewRealClient()
	if c.KubectlBin != "kubectl" {
		t.Errorf("NewRealClient KubectlBin = %q, want %q", c.KubectlBin, "kubectl")
	}
}

func TestRealClient_kubectl_CustomBin(t *testing.T) {
	c := &RealClient{KubectlBin: "/usr/local/bin/kubectl"}
	if got := c.kubectl(); got != "/usr/local/bin/kubectl" {
		t.Errorf("kubectl() = %q, want %q", got, "/usr/local/bin/kubectl")
	}
}

func TestRealClient_kubectl_EmptyBinFallback(t *testing.T) {
	c := &RealClient{KubectlBin: ""}
	if got := c.kubectl(); got != "kubectl" {
		t.Errorf("kubectl() with empty bin = %q, want %q", got, "kubectl")
	}
}
