package cluster

import (
	"testing"
)

func TestRequiredToolsNotEmpty(t *testing.T) {
	if len(requiredTools) == 0 {
		t.Error("requiredTools must not be empty")
	}
}

func TestRequiredToolsHaveNames(t *testing.T) {
	for _, tool := range requiredTools {
		if tool.Binary == "" {
			t.Errorf("tool entry has empty Binary: %+v", tool)
		}
		if tool.InstallCmd == "" {
			t.Errorf("tool %q has empty InstallCmd", tool.Binary)
		}
	}
}

func TestKubectlIsRequired(t *testing.T) {
	for _, tool := range requiredTools {
		if tool.Binary == "kubectl" && tool.Required {
			return
		}
	}
	t.Error("kubectl must be listed as a required tool")
}

func TestVClusterIsRequired(t *testing.T) {
	for _, tool := range requiredTools {
		if tool.Binary == "vcluster" && tool.Required {
			return
		}
	}
	t.Error("vcluster must be listed as a required tool")
}
