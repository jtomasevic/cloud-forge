package cluster

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ──────────────────────────────────────────────────────────────────────────────
// Tool prerequisites
// ──────────────────────────────────────────────────────────────────────────────

// Tool describes a required CLI binary and how to install it on macOS.
type Tool struct {
	// Binary is the executable name as it appears on the PATH.
	Binary string
	// BrewFormula is the Homebrew formula to install if Binary is missing.
	// If empty, the tool must be installed manually (no auto-install).
	BrewFormula string
}

// requiredTools is the canonical list of tools needed by the Cilium spike.
// vcluster is required only for Test 5; absence is handled as SKIP in the probe.
var requiredTools = []Tool{
	{Binary: "kubectl", BrewFormula: ""},                      // must be installed by the user
	{Binary: "k3d", BrewFormula: "k3d"},                      // auto-install via brew
	{Binary: "helm", BrewFormula: "helm"},                     // auto-install via brew
	{Binary: "cilium", BrewFormula: "cilium-cli"},             // auto-install via brew
	{Binary: "vcluster", BrewFormula: "loft-sh/tap/vcluster"}, // auto-install via brew
}

// ToolCheckResult holds the outcome of a single tool check.
type ToolCheckResult struct {
	Tool    Tool
	Found   bool
	Version string // first line of the version output; empty if not found
}

// CheckPrerequisites verifies that all required tools are present on the PATH.
// If a tool is missing and has a BrewFormula, it is installed via `brew install`.
// Returns an error if any required tool is still absent after attempted installation.
func CheckPrerequisites(ctx context.Context) ([]ToolCheckResult, error) {
	results := make([]ToolCheckResult, 0, len(requiredTools))

	for _, t := range requiredTools {
		ver := queryVersion(ctx, t.Binary)
		found := ver != ""

		if !found && t.BrewFormula != "" {
			// Auto-install via Homebrew.
			_ = exec.CommandContext(ctx, "brew", "install", t.BrewFormula).Run()
			ver = queryVersion(ctx, t.Binary)
			found = ver != ""
		}

		results = append(results, ToolCheckResult{Tool: t, Found: found, Version: ver})
	}

	// Collect names of tools that are still missing after attempted installation.
	missing := []string{}
	for _, r := range results {
		if !r.Found {
			missing = append(missing, r.Tool.Binary)
		}
	}
	if len(missing) > 0 {
		return results, fmt.Errorf("required tools not found: %s", strings.Join(missing, ", "))
	}
	return results, nil
}

// queryVersion returns the first line of `binary version` output.
// Returns an empty string if the binary is not on the PATH.
func queryVersion(ctx context.Context, binary string) string {
	out, err := exec.CommandContext(ctx, binary, "version").CombinedOutput()
	if err != nil {
		// Some tools use --version; try that as a fallback.
		out, err = exec.CommandContext(ctx, binary, "--version").CombinedOutput()
		if err != nil {
			return ""
		}
	}
	lines := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[0])
}
