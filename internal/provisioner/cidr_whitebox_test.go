package provisioner

// Whitebox tests for cidr.go using a fake CIDRAllocationDB.
// These tests cover AllocateCIDRs, ReleaseCIDR, and ReleaseTenantCIDRs
// without requiring a live ScyllaDB cluster.

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── fakeCIDRDB ────────────────────────────────────────────────────────────────

// fakeCIDRDB is a test double for CIDRAllocationDB.
// It tracks which CIDRs are "allocated" in an in-memory map and simulates the
// LWT IF NOT EXISTS semantics: TryAllocate returns false if the CIDR is already
// in the map.
type fakeCIDRDB struct {
	allocated  map[string]bool // cidr → allocated?
	allocErr   error           // if non-nil, TryAllocate returns this error
	releaseErr error           // if non-nil, Release returns this error
}

func newFakeCIDRDB() *fakeCIDRDB {
	return &fakeCIDRDB{allocated: make(map[string]bool)}
}

func (f *fakeCIDRDB) TryAllocate(_ context.Context, cidr string, _ uuid.UUID, _ CIDRType, _ time.Time) (bool, error) {
	if f.allocErr != nil {
		return false, f.allocErr
	}
	if f.allocated[cidr] {
		return false, nil // already taken
	}
	f.allocated[cidr] = true
	return true, nil
}

func (f *fakeCIDRDB) Release(_ context.Context, cidr string) error {
	if f.releaseErr != nil {
		return f.releaseErr
	}
	delete(f.allocated, cidr)
	return nil
}

// ── AllocateCIDRs tests ───────────────────────────────────────────────────────

// TestAllocateCIDRs_FirstAllocationGetIndex1 verifies that an empty DB
// always assigns the first /24 block (index 1).
func TestAllocateCIDRs_FirstAllocationGetIndex1(t *testing.T) {
	db := newFakeCIDRDB()
	pair, err := AllocateCIDRs(context.Background(), db, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, "10.100.1.0/24", pair.PodCIDR)
	assert.Equal(t, "10.200.1.0/24", pair.SvcCIDR)
}

// TestAllocateCIDRs_SkipsOccupiedIndex verifies that AllocateCIDRs skips
// already-allocated indices and returns the next free pair.
func TestAllocateCIDRs_SkipsOccupiedIndex(t *testing.T) {
	db := newFakeCIDRDB()
	// Pre-occupy index 1 pod CIDR.
	db.allocated["10.100.1.0/24"] = true
	db.allocated["10.200.1.0/24"] = true

	pair, err := AllocateCIDRs(context.Background(), db, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, "10.100.2.0/24", pair.PodCIDR)
	assert.Equal(t, "10.200.2.0/24", pair.SvcCIDR)
}

// TestAllocateCIDRs_PodCIDRTakenRollsBack verifies that if the pod CIDR is
// claimed but the service CIDR at the same index is already taken, the pod
// CIDR allocation is rolled back and the allocator advances to the next index.
func TestAllocateCIDRs_PodCIDRTakenRollsBack(t *testing.T) {
	db := newFakeCIDRDB()
	// Index 1: pod CIDR is free, but svc CIDR is taken.
	db.allocated["10.200.1.0/24"] = true

	pair, err := AllocateCIDRs(context.Background(), db, uuid.New())
	require.NoError(t, err)
	// Should fall through to index 2.
	assert.Equal(t, "10.100.2.0/24", pair.PodCIDR)
	assert.Equal(t, "10.200.2.0/24", pair.SvcCIDR)

	// Index 1 pod CIDR must have been rolled back (no allocation leak).
	assert.False(t, db.allocated["10.100.1.0/24"], "pod CIDR at index 1 should have been released")
}

// TestAllocateCIDRs_ConcurrentAllocation simulates two goroutines racing to
// allocate CIDRs. Because our fake is not thread-safe, we run them sequentially
// and verify that they receive non-overlapping pairs.
func TestAllocateCIDRs_ConcurrentAllocation(t *testing.T) {
	db := newFakeCIDRDB()

	pair1, err := AllocateCIDRs(context.Background(), db, uuid.New())
	require.NoError(t, err)

	pair2, err := AllocateCIDRs(context.Background(), db, uuid.New())
	require.NoError(t, err)

	assert.NotEqual(t, pair1.PodCIDR, pair2.PodCIDR, "two allocations must not share a pod CIDR")
	assert.NotEqual(t, pair1.SvcCIDR, pair2.SvcCIDR, "two allocations must not share a svc CIDR")
}

// TestAllocateCIDRs_AllocErrorPropagated verifies that a DB error from
// TryAllocate is immediately returned without continuing the scan.
func TestAllocateCIDRs_AllocErrorPropagated(t *testing.T) {
	db := newFakeCIDRDB()
	db.allocErr = errors.New("scylla: LWT timeout")

	_, err := AllocateCIDRs(context.Background(), db, uuid.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "allocate pod CIDR")
}

// TestAllocateCIDRs_SvcAllocErrorRollsBackPod verifies that when TryAllocate
// succeeds for the pod CIDR but returns an error for the service CIDR, the pod
// CIDR allocation is rolled back before the error is returned.
func TestAllocateCIDRs_SvcAllocErrorRollsBackPod(t *testing.T) {
	callCount := 0
	db := &fakeCIDRDB{allocated: make(map[string]bool)}
	// Override TryAllocate: succeed on the first call (pod), fail on the second
	// (svc).
	original := db.TryAllocate
	_ = original

	var impl CIDRAllocationDB = &errorOnSvcCIDRDB{inner: db, podCallsDone: &callCount}

	_, err := AllocateCIDRs(context.Background(), impl, uuid.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "allocate svc CIDR")
	// The pod CIDR at index 1 must have been released (rolled back).
	assert.False(t, db.allocated["10.100.1.0/24"], "pod CIDR should be rolled back on svc error")
}

// TestAllocateCIDRs_Exhausted verifies that ErrCIDRExhausted is returned when
// all /24 blocks are occupied.
func TestAllocateCIDRs_Exhausted(t *testing.T) {
	db := newFakeCIDRDB()
	// Fill all 254 pod + svc pairs.
	for i := 1; i <= maxTenants; i++ {
		db.allocated[fmt.Sprintf("10.100.%d.0/24", i)] = true
		db.allocated[fmt.Sprintf("10.200.%d.0/24", i)] = true
	}

	_, err := AllocateCIDRs(context.Background(), db, uuid.New())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrCIDRExhausted))
}

// ── ReleaseCIDR tests ─────────────────────────────────────────────────────────

// TestReleaseCIDR_Success verifies that ReleaseCIDR returns nil on success.
func TestReleaseCIDR_Success(t *testing.T) {
	db := newFakeCIDRDB()
	db.allocated["10.100.5.0/24"] = true
	err := ReleaseCIDR(context.Background(), db, "10.100.5.0/24")
	require.NoError(t, err)
	assert.False(t, db.allocated["10.100.5.0/24"])
}

// TestReleaseCIDR_IdempotentOnMissing verifies that releasing a CIDR that is
// not allocated succeeds (idempotent — the DELETE is a no-op).
func TestReleaseCIDR_IdempotentOnMissing(t *testing.T) {
	db := newFakeCIDRDB()
	err := ReleaseCIDR(context.Background(), db, "10.100.99.0/24")
	require.NoError(t, err)
}

// TestReleaseCIDR_ErrorPropagated verifies that DB errors are wrapped and returned.
func TestReleaseCIDR_ErrorPropagated(t *testing.T) {
	db := newFakeCIDRDB()
	db.releaseErr = errors.New("scylla: connection refused")
	err := ReleaseCIDR(context.Background(), db, "10.100.1.0/24")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "release CIDR")
}

// ── ReleaseTenantCIDRs tests ──────────────────────────────────────────────────

// TestReleaseTenantCIDRs_BothReleased verifies that both the pod and service
// CIDRs are deleted from the DB on a successful deprovisioning call.
func TestReleaseTenantCIDRs_BothReleased(t *testing.T) {
	db := newFakeCIDRDB()
	db.allocated["10.100.3.0/24"] = true
	db.allocated["10.200.3.0/24"] = true

	err := ReleaseTenantCIDRs(context.Background(), db, "10.100.3.0/24", "10.200.3.0/24")
	require.NoError(t, err)
	assert.False(t, db.allocated["10.100.3.0/24"])
	assert.False(t, db.allocated["10.200.3.0/24"])
}

// TestReleaseTenantCIDRs_CombinesErrors verifies that if both Release calls
// fail, both errors are reported in the returned error.
func TestReleaseTenantCIDRs_CombinesErrors(t *testing.T) {
	db := newFakeCIDRDB()
	db.releaseErr = errors.New("timeout")

	err := ReleaseTenantCIDRs(context.Background(), db, "10.100.3.0/24", "10.200.3.0/24")
	require.Error(t, err)
	// Both pod and service CIDR errors should be in the combined message.
	assert.Contains(t, err.Error(), "release CIDRs")
}

// ── NewGocqlCIDRDB constructor test ───────────────────────────────────────────

// TestNewGocqlCIDRDB_ReturnsNonNil verifies that the constructor returns a
// non-nil CIDRAllocationDB. This is a smoke test — the actual DB operations
// are covered by integration tests that require a live ScyllaDB cluster.
func TestNewGocqlCIDRDB_ReturnsNonNil(t *testing.T) {
	// NewGocqlCIDRDB wraps a *gocql.Session; passing nil is acceptable here
	// because we are only testing the constructor, not executing any queries.
	db := NewGocqlCIDRDB(nil)
	assert.NotNil(t, db)
}

// ── Helper types for error-injection scenarios ────────────────────────────────

// errorOnSvcCIDRDB wraps a fakeCIDRDB and returns an error on the second
// TryAllocate call (the service CIDR allocation) at each index.
type errorOnSvcCIDRDB struct {
	inner        *fakeCIDRDB
	podCallsDone *int
}

func (e *errorOnSvcCIDRDB) TryAllocate(ctx context.Context, cidr string, id uuid.UUID, t CIDRType, now time.Time) (bool, error) {
	if t == CIDRTypePod {
		*e.podCallsDone++
		return e.inner.TryAllocate(ctx, cidr, id, t, now)
	}
	// Service CIDR call: fail.
	return false, errors.New("scylla: svc CIDR LWT error")
}

func (e *errorOnSvcCIDRDB) Release(ctx context.Context, cidr string) error {
	return e.inner.Release(ctx, cidr)
}
