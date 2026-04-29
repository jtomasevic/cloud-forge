package cluster

import (
	"testing"
)

// ── KubeconfigPath ────────────────────────────────────────────────────────────

func TestKubeconfigPath(t *testing.T) {
	cases := []struct {
		dir  string
		name string
		want string
	}{
		{"kubeconfigs", "tenant-a", "kubeconfigs/tenant-a.kubeconfig"},
		{"./kc", "tenant-b", "./kc/tenant-b.kubeconfig"},
		{"/tmp/spike", "foo", "/tmp/spike/foo.kubeconfig"},
	}
	for _, c := range cases {
		got := KubeconfigPath(c.dir, c.name)
		if got != c.want {
			t.Errorf("KubeconfigPath(%q,%q) = %q, want %q", c.dir, c.name, got, c.want)
		}
	}
}

// ── dirOf ─────────────────────────────────────────────────────────────────────

func TestDirOf(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"kubeconfigs/tenant-a.kubeconfig", "kubeconfigs"},
		{"/tmp/foo/bar.yaml", "/tmp/foo"},
		{"nodir", "."},
		{"a/b/c/d", "a/b/c"},
	}
	for _, c := range cases {
		got := dirOf(c.path)
		if got != c.want {
			t.Errorf("dirOf(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// ── isVClusterRunning (string search logic) ───────────────────────────────────

func TestIsVClusterRunningStringCheck(t *testing.T) {
	// Simulate the JSON output that vcluster list returns
	jsonWithRunning := `[{"name":"tenant-a","namespace":"vcluster-tenant-a","status":"Running"}]`
	jsonWithoutRunning := `[{"name":"tenant-a","namespace":"vcluster-tenant-a","status":"Pending"}]`
	jsonEmpty := `[]`

	cases := []struct {
		output string
		name   string
		want   bool
	}{
		{jsonWithRunning, "tenant-a", true},
		{jsonWithoutRunning, "tenant-a", false},
		{jsonEmpty, "tenant-a", false},
		{jsonWithRunning, "tenant-b", false}, // different name
	}
	for _, c := range cases {
		// Replicate the string-search logic from isVClusterRunning
		got := containsName(c.output, c.name) && containsRunning(c.output)
		if got != c.want {
			t.Errorf("running check for %q in %q = %v, want %v", c.name, c.output, got, c.want)
		}
	}
}

func containsName(output, name string) bool {
	return len(output) > 0 && len(name) > 0 &&
		func() bool {
			for i := 0; i <= len(output)-len(name); i++ {
				if output[i:i+len(name)] == name {
					return true
				}
			}
			return false
		}()
}

func containsRunning(output string) bool {
	const target = "Running"
	for i := 0; i <= len(output)-len(target); i++ {
		if output[i:i+len(target)] == target {
			return true
		}
	}
	return false
}
