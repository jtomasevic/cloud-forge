package provisioner

// Whitebox tests for unexported helpers in cnp.go.
// By being in the provisioner package, these tests can directly call
// renderCNP and inspect the parsedAccessTemplate variable.

import (
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRenderCNP_ErrorCoverage injects an invalid template to exercise the
// template.Execute error path in renderCNP. This path cannot be triggered in
// production (template is pre-validated via template.Must), but it is tested
// here for completeness.
func TestRenderCNP_ErrorCoverage(t *testing.T) {
	// Replace the package-level template with one that always returns an error.
	orig := parsedCNPTemplate
	parsedCNPTemplate = template.Must(template.New("broken").Parse(`{{ .NonexistentField.NestedCall }}`))
	defer func() { parsedCNPTemplate = orig }()

	_, err := renderCNP("test-ns", "test-policy")
	// The error should come from template execution, not namespace validation.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "render CNP template")
}

// TestProvisionerAccessPolicy_TemplateBrokenCoverage exercises the
// parsedAccessTemplate.Execute error path. Replaces the template with a
// broken one to simulate a template execution failure.
func TestProvisionerAccessPolicy_TemplateBrokenCoverage(t *testing.T) {
	orig := parsedAccessTemplate
	parsedAccessTemplate = template.Must(template.New("bad").Parse(`{{ .Bad.Nested }}`))
	defer func() { parsedAccessTemplate = orig }()

	_, err := ProvisionerAccessPolicy("valid-ns")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "render provisioner-access CNP template")
}
