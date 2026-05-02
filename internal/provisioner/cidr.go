package provisioner

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/gocql/gocql"
	"github.com/google/uuid"
)

// CIDRAllocationDB is the narrow interface that AllocateCIDRs and
// ReleaseCIDR need from the ScyllaDB session. By depending on this interface
// rather than *gocql.Session directly, tests can inject a fake implementation
// without requiring a live ScyllaDB cluster.
//
// *GocqlCIDRDB (returned by NewGocqlCIDRDB) satisfies this interface for
// production use.
type CIDRAllocationDB interface {
	// TryAllocate attempts to atomically reserve a CIDR block for a tenant.
	// It returns (true, nil) if the allocation succeeded, (false, nil) if
	// the CIDR was already taken, or (false, err) on a database error.
	TryAllocate(ctx context.Context, cidr string, tenantID uuid.UUID, cidrType CIDRType, now time.Time) (bool, error)

	// Release deletes the CIDR allocation row, making the block available
	// for future tenants. Safe to call on a missing CIDR (no-op).
	Release(ctx context.Context, cidr string) error
}

// GocqlCIDRDB wraps a *gocql.Session and implements CIDRAllocationDB.
type GocqlCIDRDB struct{ sess *gocql.Session }

// NewGocqlCIDRDB returns a CIDRAllocationDB backed by the given session.
func NewGocqlCIDRDB(sess *gocql.Session) CIDRAllocationDB {
	return &GocqlCIDRDB{sess: sess}
}

// TryAllocate runs an LWT INSERT IF NOT EXISTS to reserve the CIDR.
func (g *GocqlCIDRDB) TryAllocate(ctx context.Context, cidr string, tenantID uuid.UUID, cidrType CIDRType, now time.Time) (bool, error) {
	dest := make(map[string]interface{})
	applied, err := g.sess.Query(`
		INSERT INTO cf.cidr_allocations (cidr, tenant_id, cidr_type, allocated_at)
		VALUES (?, ?, ?, ?)
		IF NOT EXISTS`,
		cidr, gocql.UUID(tenantID), string(cidrType), now,
	).WithContext(ctx).MapScanCAS(dest)
	if err != nil {
		return false, fmt.Errorf("provisioner: cidr LWT for %s: %w", cidr, err)
	}
	return applied, nil
}

// Release deletes the CIDR row.
func (g *GocqlCIDRDB) Release(ctx context.Context, cidr string) error {
	return g.sess.Query(
		`DELETE FROM cf.cidr_allocations WHERE cidr = ?`, cidr,
	).WithContext(ctx).Exec()
}

// CIDRType distinguishes pod-network CIDR allocations from service-network
// CIDR allocations. Both use the same table but come from different supernets.
type CIDRType string

const (
	// CIDRTypePod is the allocation type for vCluster pod network CIDRs.
	// Allocated from supernet 10.100.0.0/16 as /24 blocks:
	// 10.100.0.0/24, 10.100.1.0/24, … 10.100.254.0/24 (254 tenants max per /16).
	CIDRTypePod CIDRType = "POD"

	// CIDRTypeService is the allocation type for vCluster service network CIDRs.
	// Allocated from supernet 10.200.0.0/16 as /24 blocks.
	CIDRTypeService CIDRType = "SERVICE"
)

// podSupernet is the host-level supernet from which /24 pod CIDRs are carved.
const podSupernet = "10.100.0.0/16"

// svcSupernet is the host-level supernet from which /24 service CIDRs are carved.
const svcSupernet = "10.200.0.0/16"

// maxTenants is the maximum number of /24 blocks available in a /16 supernet.
// Index 0 is reserved (10.x.0.0/24 is the gateway range), leaving 254 slots.
const maxTenants = 254

// CIDRPair holds both CIDR allocations for a single tenant vCluster.
type CIDRPair struct {
	PodCIDR string // e.g. "10.100.3.0/24"
	SvcCIDR string // e.g. "10.200.3.0/24"
}

// ErrCIDRExhausted is returned when all /24 blocks in a supernet have been
// allocated. This indicates the platform has reached its per-region tenant
// capacity for the current supernet configuration.
var ErrCIDRExhausted = errors.New("provisioner: CIDR space exhausted — all /24 blocks in supernet are allocated")

// AllocateCIDRs finds the next available /24 index in both the pod and service
// supernets, reserves both blocks using LWT (IF NOT EXISTS), and returns the
// allocated pair.
//
// Concurrent callers are safe: LWT ensures exactly one caller wins each index.
// If another goroutine claims a particular index first, AllocateCIDRs advances
// to the next index automatically.
//
// db is normally a *GocqlCIDRDB (wrapping a live *gocql.Session). Tests
// substitute a fake CIDRAllocationDB to avoid needing a live ScyllaDB cluster.
//
// The tenantID is stored in cf.cidr_allocations alongside the CIDR so that
// deprovisioning can release the block by deleting the row.
func AllocateCIDRs(ctx context.Context, db CIDRAllocationDB, tenantID uuid.UUID) (CIDRPair, error) {
	for i := 1; i <= maxTenants; i++ {
		podCIDR := fmt.Sprintf("10.100.%d.0/24", i)
		svcCIDR := fmt.Sprintf("10.200.%d.0/24", i)

		// Validate that these are well-formed CIDRs (defensive check).
		if _, _, err := net.ParseCIDR(podCIDR); err != nil {
			return CIDRPair{}, fmt.Errorf("provisioner: invalid pod CIDR %s: %w", podCIDR, err)
		}
		if _, _, err := net.ParseCIDR(svcCIDR); err != nil {
			return CIDRPair{}, fmt.Errorf("provisioner: invalid svc CIDR %s: %w", svcCIDR, err)
		}

		now := time.Now().UTC()

		// Attempt to claim the pod CIDR. LWT: only proceeds if the CIDR is free.
		podApplied, err := db.TryAllocate(ctx, podCIDR, tenantID, CIDRTypePod, now)
		if err != nil {
			return CIDRPair{}, fmt.Errorf("provisioner: allocate pod CIDR %s: %w", podCIDR, err)
		}
		if !podApplied {
			// Another tenant owns this pod CIDR — try the next index.
			continue
		}

		// Pod CIDR secured. Now attempt the matching service CIDR.
		svcApplied, err := db.TryAllocate(ctx, svcCIDR, tenantID, CIDRTypeService, now)
		if err != nil {
			// Roll back pod CIDR allocation before returning the error.
			_ = ReleaseCIDR(ctx, db, podCIDR)
			return CIDRPair{}, fmt.Errorf("provisioner: allocate svc CIDR %s: %w", svcCIDR, err)
		}
		if !svcApplied {
			// Service CIDR at index i is already taken. Roll back the pod
			// allocation and continue scanning.
			_ = ReleaseCIDR(ctx, db, podCIDR)
			continue
		}

		return CIDRPair{PodCIDR: podCIDR, SvcCIDR: svcCIDR}, nil
	}
	return CIDRPair{}, ErrCIDRExhausted
}

// ReleaseCIDR deletes a CIDR allocation row, making the block available for
// reuse. Called during tenant deprovisioning. Safe to call on an already-
// released CIDR (DELETE is a no-op if the row does not exist).
func ReleaseCIDR(ctx context.Context, db CIDRAllocationDB, cidr string) error {
	if err := db.Release(ctx, cidr); err != nil {
		return fmt.Errorf("provisioner: release CIDR %s: %w", cidr, err)
	}
	return nil
}

// ReleaseTenantCIDRs releases both the pod and service CIDR allocations for
// a given tenant. Called at the start of the deprovisioning workflow.
// Both deletions are attempted regardless of individual errors; all errors are
// combined and returned.
func ReleaseTenantCIDRs(ctx context.Context, db CIDRAllocationDB, podCIDR, svcCIDR string) error {
	var errs []error
	if err := ReleaseCIDR(ctx, db, podCIDR); err != nil {
		errs = append(errs, err)
	}
	if err := ReleaseCIDR(ctx, db, svcCIDR); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("provisioner: release CIDRs: %v", errs)
	}
	return nil
}

// parsedSupernets returns the parsed supernet CIDRs for validation purposes.
// Exported for use by tests.
func parsedSupernets() (pod, svc *net.IPNet, err error) {
	_, pod, err = net.ParseCIDR(podSupernet)
	if err != nil {
		return nil, nil, err
	}
	_, svc, err = net.ParseCIDR(svcSupernet)
	return pod, svc, err
}
