package cluster

import (
	"testing"
)

func TestNewRealClient_DefaultBinary(t *testing.T) {
	c := NewRealClient()
	if c.KubectlBin != "kubectl" {
		t.Errorf("KubectlBin = %q, want %q", c.KubectlBin, "kubectl")
	}
}

func TestRealClient_KubectlMethod(t *testing.T) {
	c := &RealClient{KubectlBin: "kubectl"}
	if got := c.kubectl(); got != "kubectl" {
		t.Errorf("kubectl() = %q, want kubectl", got)
	}

	// Empty KubectlBin falls back to "kubectl".
	c2 := &RealClient{}
	if got := c2.kubectl(); got != "kubectl" {
		t.Errorf("kubectl() (empty) = %q, want kubectl", got)
	}
}
