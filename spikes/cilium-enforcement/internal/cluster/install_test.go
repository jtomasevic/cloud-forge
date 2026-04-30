package cluster

import (
	"testing"
)

func TestRequiredTools_List(t *testing.T) {
	binaries := map[string]bool{
		"kubectl":  true,
		"k3d":      true,
		"helm":     true,
		"cilium":   true,
		"vcluster": true,
	}
	if len(requiredTools) != len(binaries) {
		t.Fatalf("requiredTools length = %d, want %d", len(requiredTools), len(binaries))
	}
	for _, tool := range requiredTools {
		if !binaries[tool.Binary] {
			t.Errorf("unexpected tool in requiredTools: %q", tool.Binary)
		}
	}
}

func TestRequiredTools_KubectlHasNoFormula(t *testing.T) {
	for _, tool := range requiredTools {
		if tool.Binary == "kubectl" && tool.BrewFormula != "" {
			t.Errorf("kubectl should have no BrewFormula (user must install), got %q", tool.BrewFormula)
		}
	}
}

func TestRequiredTools_CiliumFormula(t *testing.T) {
	for _, tool := range requiredTools {
		if tool.Binary == "cilium" {
			if tool.BrewFormula != "cilium-cli" {
				t.Errorf("cilium BrewFormula = %q, want %q", tool.BrewFormula, "cilium-cli")
			}
			return
		}
	}
	t.Error("cilium not found in requiredTools")
}
