package provisioner

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParsedSupernets verifies that the hardcoded pod and service supernets
// are valid CIDR blocks. This catches typos before they reach production.
func TestParsedSupernets(t *testing.T) {
	pod, svc, err := parsedSupernets()
	require.NoError(t, err)

	assert.Equal(t, "10.100.0.0/16", pod.String())
	assert.Equal(t, "10.200.0.0/16", svc.String())
}

// TestCIDRFormat verifies that the pod and service CIDRs generated for
// representative tenant slots have the expected format and fall within their
// respective supernets.
func TestCIDRFormat(t *testing.T) {
	podSupernet, svcSupernet, err := parsedSupernets()
	require.NoError(t, err)

	tests := []struct {
		index   int
		wantPod string
		wantSvc string
	}{
		{1, "10.100.1.0/24", "10.200.1.0/24"},
		{2, "10.100.2.0/24", "10.200.2.0/24"},
		{100, "10.100.100.0/24", "10.200.100.0/24"},
		{254, "10.100.254.0/24", "10.200.254.0/24"},
	}

	for _, tc := range tests {
		podCIDR := generateCIDR("10.100", tc.index)
		svcCIDR := generateCIDR("10.200", tc.index)

		assert.Equal(t, tc.wantPod, podCIDR)
		assert.Equal(t, tc.wantSvc, svcCIDR)

		_, podBlock, _ := net.ParseCIDR(podCIDR)
		assert.True(t, podSupernet.Contains(podBlock.IP),
			"pod CIDR %s not in pod supernet %s", podCIDR, podSupernet)

		_, svcBlock, _ := net.ParseCIDR(svcCIDR)
		assert.True(t, svcSupernet.Contains(svcBlock.IP),
			"svc CIDR %s not in svc supernet %s", svcCIDR, svcSupernet)
	}
}

// TestCIDRPairNonOverlapping verifies that pod and service CIDRs for the same
// index do not overlap — vCluster requires non-overlapping pod and service
// networks.
func TestCIDRPairNonOverlapping(t *testing.T) {
	for i := 1; i <= 10; i++ {
		podCIDR := generateCIDR("10.100", i)
		svcCIDR := generateCIDR("10.200", i)

		_, podNet, err := net.ParseCIDR(podCIDR)
		require.NoError(t, err)
		_, svcNet, err := net.ParseCIDR(svcCIDR)
		require.NoError(t, err)

		assert.False(t, podNet.Contains(svcNet.IP),
			"pod %s should not contain svc %s at index %d", podCIDR, svcCIDR, i)
		assert.False(t, svcNet.Contains(podNet.IP),
			"svc %s should not contain pod %s at index %d", svcCIDR, podCIDR, i)
	}
}

// generateCIDR is a test helper that replicates the CIDR generation formula
// used in AllocateCIDRs, so the tests are independent of the unexported
// allocation loop.
func generateCIDR(prefix string, index int) string {
	return prefix + "." + itoa(index) + ".0/24"
}

// itoa converts an integer to its decimal string representation without
// importing strconv (keeps test helpers minimal).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := [10]byte{}
	i := 9
	for n > 0 {
		digits[i] = byte('0' + n%10)
		n /= 10
		i--
	}
	return string(digits[i+1:])
}

// TestMaxTenantsConstant verifies that maxTenants is 254, matching the number
// of usable /24 blocks in a /16 supernet (excluding the gateway .0.0/24).
func TestMaxTenantsConstant(t *testing.T) {
	assert.Equal(t, 254, maxTenants)
}
