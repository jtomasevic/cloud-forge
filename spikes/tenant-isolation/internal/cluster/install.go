package cluster

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ──────────────────────────────────────────────────────────────────────────────
// Prerequisite check + installation
// ──────────────────────────────────────────────────────────────────────────────

// Tool represents a CLI tool that must be present for the spike to run.
type Tool struct {
	// Binary is the command name used for both PATH lookup and display.
	Binary string
	// InstallCmd is a human-readable hint shown when the tool is missing.
	InstallCmd string
	// Required indicates that the spike cannot run without this tool.
	// Optional tools are checked but their absence is reported as a warning only.
	Required bool
}

// requiredTools is the list of tools the spike needs.
// The Makefile handles installation automatically; this list is used for
// runtime validation and user-facing error messages.
var requiredTools = []Tool{
	{Binary: "kubectl", InstallCmd: "brew install kubectl", Required: true},
	{Binary: "helm", InstallCmd: "brew install helm", Required: true},
	{Binary: "vcluster", InstallCmd: "brew install loft-sh/tap/vcluster", Required: true},
	{Binary: "k3d", InstallCmd: "brew install k3d", Required: true},
}

// CheckResult holds the outcome for a single tool check.
type CheckResult struct {
	Tool    Tool
	Found   bool
	Version string
}

// CheckPrerequisites verifies that every required tool is on the PATH.
// Returns a slice of results and a combined error for any missing required tools.
func CheckPrerequisites(ctx context.Context) ([]CheckResult, error) {
	results := make([]CheckResult, len(requiredTools))
	var missing []string

	for i, tool := range requiredTools {
		path, err := exec.LookPath(tool.Binary)
		results[i] = CheckResult{Tool: tool, Found: err == nil}
		if err == nil {
			results[i].Version = queryVersion(ctx, path, tool.Binary)
		}
		if err != nil && tool.Required {
			missing = append(missing, fmt.Sprintf("  %s  (install: %s)", tool.Binary, tool.InstallCmd))
		}
	}

	if len(missing) > 0 {
		return results, fmt.Errorf("missing required tools:\n%s", strings.Join(missing, "\n"))
	}
	return results, nil
}

// queryVersion runs `<binary> version --short` or `<binary> version` and returns
// the first non-empty output line. Returns an empty string on failure.
func queryVersion(ctx context.Context, path, binary string) string {
	// Try --short first (works for kubectl, vcluster)
	for _, args := range [][]string{{"version", "--short"}, {"version"}, {"--version"}} {
		cmd := exec.CommandContext(ctx, path, args...)
		out, err := cmd.Output()
		if err == nil && len(out) > 0 {
			// Return first non-empty line
			for _, line := range strings.Split(string(out), "\n") {
				line = strings.TrimSpace(line)
				if line != "" {
					return line
				}
			}
		}
	}
	return ""
}
