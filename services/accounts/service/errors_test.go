package service_test

import (
	"errors"
	"testing"

	. "github.com/jtomasevic/cloud-forge/services/accounts/service"
)

// TestSentinelErrors_AreDistinct ensures that every sentinel error is unique
// and cannot be confused with another via errors.Is.
func TestSentinelErrors_AreDistinct(t *testing.T) {
	sentinels := []error{
		ErrAccountNotFound,
		ErrAccountAlreadyExists,
		ErrAccountNotActive,
		ErrKeyNotFound,
	}
	for i, a := range sentinels {
		for j, b := range sentinels {
			if i == j {
				continue
			}
			if errors.Is(a, b) {
				t.Errorf("sentinels[%d] and sentinels[%d] are equal: %v / %v", i, j, a, b)
			}
		}
	}
}

// TestSentinelErrors_MatchThemselves ensures errors.Is works for each sentinel.
func TestSentinelErrors_MatchThemselves(t *testing.T) {
	for _, e := range []error{
		ErrAccountNotFound,
		ErrAccountAlreadyExists,
		ErrAccountNotActive,
		ErrKeyNotFound,
	} {
		if !errors.Is(e, e) {
			t.Errorf("errors.Is(%v, %v) returned false", e, e)
		}
	}
}
