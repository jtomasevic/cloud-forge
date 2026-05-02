package provisioner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsVClusterNotFound verifies the not-found detection heuristic for the
// vcluster CLI. The CLI does not return structured errors, so we match on
// well-known substrings in the error message.
func TestIsVClusterNotFound(t *testing.T) {
	tests := []struct {
		name    string
		errMsg  string
		want    bool
	}{
		{"nil error", "", false},
		{"not found", "VirtualCluster not found", true},
		{"no virtual cluster", "no virtual cluster with name acme exists", true},
		{"does not exist", "vcluster acme-corp does not exist", true},
		{"unrelated error", "connection refused", false},
		{"timeout", "context deadline exceeded", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.errMsg != "" {
				err = errString(tc.errMsg)
			}
			got := isVClusterNotFound(err)
			assert.Equal(t, tc.want, got, "isVClusterNotFound(%q)", tc.errMsg)
		})
	}
}

// TestValidateVClusterConfig_MissingTenantID verifies that CreateVCluster
// rejects an empty TenantID without making any system calls.
func TestValidateVClusterConfig_MissingTenantID(t *testing.T) {
	_, err := CreateVCluster(context.Background(), VClusterConfig{
		HostNamespace: "tenant-acme",
		PodCIDR:       "10.100.1.0/24",
		SvcCIDR:       "10.200.1.0/24",
	})
	assert.Error(t, err)
}

// TestValidateVClusterConfig_MissingNamespace verifies that CreateVCluster
// rejects an empty HostNamespace.
func TestValidateVClusterConfig_MissingNamespace(t *testing.T) {
	_, err := CreateVCluster(context.Background(), VClusterConfig{
		TenantID: "acme-corp",
		PodCIDR:  "10.100.1.0/24",
		SvcCIDR:  "10.200.1.0/24",
	})
	assert.Error(t, err)
}

// TestValidateVClusterConfig_MissingCIDRs verifies that CreateVCluster
// rejects configs with empty CIDR fields.
func TestValidateVClusterConfig_MissingCIDRs(t *testing.T) {
	_, err := CreateVCluster(context.Background(), VClusterConfig{
		TenantID:      "acme-corp",
		HostNamespace: "tenant-acme-corp",
	})
	assert.Error(t, err)
}

// errString is a simple error type for tests that need a non-nil error with a
// specific message string.
type errString string

func (e errString) Error() string { return string(e) }
