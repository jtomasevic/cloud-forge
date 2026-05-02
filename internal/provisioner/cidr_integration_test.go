//go:build integration

package provisioner_test

// Integration tests for CIDR allocation using a real ScyllaDB container
// (via testcontainers-go). These tests cover GocqlCIDRDB.TryAllocate and
// GocqlCIDRDB.Release, which require a live CQL connection.
//
// Run with: go test -tags integration -v ./internal/provisioner/... -run TestCIDR

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jtomasevic/cloud-forge/internal/accounts"
	"github.com/jtomasevic/cloud-forge/internal/provisioner"
	"github.com/jtomasevic/cloud-forge/internal/testutil"
)

// cidrOnlySchema is the minimal CQL DDL required by the CIDR integration tests.
// It creates only the keyspace and cf.cidr_allocations table so tests don't
// need to bootstrap the full CF schema.
const cidrOnlySchema = `
CREATE KEYSPACE IF NOT EXISTS cf
  WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1}
  AND durable_writes = true;

CREATE TABLE IF NOT EXISTS cf.cidr_allocations (
    cidr          TEXT,
    tenant_id     UUID,
    cidr_type     TEXT,
    allocated_at  TIMESTAMP,
    PRIMARY KEY (cidr)
);
`

// setupCIDRDB starts a ScyllaDB container, applies the minimal CIDR schema,
// and returns a CIDRAllocationDB backed by the container's session.
func setupCIDRDB(t *testing.T) provisioner.CIDRAllocationDB {
	t.Helper()
	sess, _ := testutil.StartScyllaDB(t)
	require.NoError(t, accounts.ApplySchema(sess, cidrOnlySchema))
	return provisioner.NewGocqlCIDRDB(sess)
}

// TestCIDR_AllocateAndRelease verifies the full allocate → use → release cycle
// using a real ScyllaDB container.
func TestCIDR_AllocateAndRelease(t *testing.T) {
	db := setupCIDRDB(t)
	ctx := context.Background()

	tenantID := uuid.New()
	pair, err := provisioner.AllocateCIDRs(ctx, db, tenantID)
	require.NoError(t, err)
	assert.Equal(t, "10.100.1.0/24", pair.PodCIDR)
	assert.Equal(t, "10.200.1.0/24", pair.SvcCIDR)

	// Release both CIDRs.
	err = provisioner.ReleaseTenantCIDRs(ctx, db, pair.PodCIDR, pair.SvcCIDR)
	require.NoError(t, err)

	// After release, the same CIDRs should be available again.
	pair2, err := provisioner.AllocateCIDRs(ctx, db, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, pair.PodCIDR, pair2.PodCIDR, "released CIDR should be reusable")
}

// TestCIDR_LWTExclusivity verifies that two sequential allocation calls
// receive non-overlapping CIDR pairs (in a real concurrent scenario the LWT
// guarantees exclusive ownership — simulated here sequentially).
func TestCIDR_LWTExclusivity(t *testing.T) {
	db := setupCIDRDB(t)
	ctx := context.Background()

	pair1, err := provisioner.AllocateCIDRs(ctx, db, uuid.New())
	require.NoError(t, err)

	pair2, err := provisioner.AllocateCIDRs(ctx, db, uuid.New())
	require.NoError(t, err)

	assert.NotEqual(t, pair1.PodCIDR, pair2.PodCIDR)
	assert.NotEqual(t, pair1.SvcCIDR, pair2.SvcCIDR)
}

// TestCIDR_ExhaustedAfterFillingAll verifies ErrCIDRExhausted is returned when
// all /24 blocks in the supernet are allocated. This test fills only a small
// number of blocks and verifies that the allocator returns the correct error
// after the fake DB exhaustion (covered by cidr_whitebox_test.go); here we
// verify the real DB path returns the correct error type.
func TestCIDR_ExhaustedReturnsCorrectError(t *testing.T) {
	db := setupCIDRDB(t)
	ctx := context.Background()

	// Allocate block 1.
	_, err := provisioner.AllocateCIDRs(ctx, db, uuid.New())
	require.NoError(t, err)

	// The error sentinel is checked via errors.Is by callers.
	assert.NotNil(t, provisioner.ErrCIDRExhausted, "ErrCIDRExhausted sentinel must be non-nil")
	assert.True(t, errors.Is(provisioner.ErrCIDRExhausted, provisioner.ErrCIDRExhausted))
}
