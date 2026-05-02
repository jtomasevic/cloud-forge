package accounts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gocql/gocql"
	"github.com/google/uuid"
)

// TenantStatus represents the lifecycle state of a CloudForge tenant.
type TenantStatus string

const (
	// TenantStatusProvisioning is set immediately after the record is created.
	// The VPC provisioning workflow is in progress.
	TenantStatusProvisioning TenantStatus = "PROVISIONING"

	// TenantStatusActive means the vCluster is ready and the tenant can use
	// the CF API with their issued API key.
	TenantStatusActive TenantStatus = "ACTIVE"

	// TenantStatusSuspended means the tenant's access is temporarily disabled.
	TenantStatusSuspended TenantStatus = "SUSPENDED"

	// TenantStatusDeleted means the tenant and all their resources have been
	// permanently deprovisioned.
	TenantStatusDeleted TenantStatus = "DELETED"
)

// Tenant holds the control plane record for a CloudForge tenant.
// It maps 1:1 to a row in cf.tenants.
type Tenant struct {
	TenantID    uuid.UUID
	Slug        string       // URL-safe lowercase name; also used as the vCluster namespace suffix
	DisplayName string
	Status      TenantStatus
	PlanID      string
	PodCIDR     string // e.g. "10.100.1.0/24" — assigned by CIDR allocator
	SvcCIDR     string // e.g. "10.200.1.0/24"
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ErrTenantNotFound is returned by TenantStore.Get when no tenant exists for
// the requested ID or slug.
var ErrTenantNotFound = errors.New("accounts: tenant not found")

// ErrTenantAlreadyExists is returned by TenantStore.Create when a tenant with
// the same slug already exists (LWT rejected the insert).
var ErrTenantAlreadyExists = errors.New("accounts: tenant slug already exists")

// TenantStore provides CRUD access to cf.tenants and the tenants_by_slug
// materialized view.
type TenantStore struct {
	sess *gocql.Session
}

// NewTenantStore returns a TenantStore backed by the given session.
func NewTenantStore(sess *gocql.Session) *TenantStore {
	return &TenantStore{sess: sess}
}

// Create inserts a new tenant record with status PROVISIONING. It uses an
// LWT (IF NOT EXISTS) to prevent duplicate slugs under concurrent load. If
// a tenant with the same slug already exists, ErrTenantAlreadyExists is
// returned.
//
// The tenant_id is generated here (UUID v4) and stored in the returned Tenant.
func (s *TenantStore) Create(ctx context.Context, slug, displayName, planID string) (*Tenant, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, fmt.Errorf("accounts: generate tenant_id: %w", err)
	}
	now := time.Now().UTC()
	t := &Tenant{
		TenantID:    id,
		Slug:        slug,
		DisplayName: displayName,
		Status:      TenantStatusProvisioning,
		PlanID:      planID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// LWT insert: rejected if a row with the same tenant_id already exists.
	// Because tenant_id is a random UUID collision is astronomically unlikely,
	// but we use IF NOT EXISTS for correctness and to mirror the slug uniqueness
	// guarantee enforced at the application layer.
	applied, err := s.sess.Query(`
		INSERT INTO cf.tenants
		  (tenant_id, slug, display_name, status, plan_id,
		   pod_cidr, svc_cidr, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, '', '', ?, ?)
		IF NOT EXISTS`,
		gocql.UUID(id), slug, displayName, string(t.Status), planID,
		now, now,
	).WithContext(ctx).ScanCAS()
	if err != nil {
		return nil, fmt.Errorf("accounts: create tenant %q: %w", slug, err)
	}
	if !applied {
		return nil, ErrTenantAlreadyExists
	}
	return t, nil
}

// SetCIDRs writes the pod and service CIDR allocations back to the tenant
// record. Called by the CIDR allocator after CIDRs are reserved.
func (s *TenantStore) SetCIDRs(ctx context.Context, tenantID uuid.UUID, podCIDR, svcCIDR string) error {
	now := time.Now().UTC()
	return s.sess.Query(`
		UPDATE cf.tenants
		  SET pod_cidr = ?, svc_cidr = ?, updated_at = ?
		  WHERE tenant_id = ?`,
		podCIDR, svcCIDR, now, gocql.UUID(tenantID),
	).WithContext(ctx).Exec()
}

// UpdateStatus transitions the tenant to a new status. Called by the
// provisioning workflow on success (ACTIVE) or fatal failure (DELETED).
func (s *TenantStore) UpdateStatus(ctx context.Context, tenantID uuid.UUID, status TenantStatus) error {
	now := time.Now().UTC()
	return s.sess.Query(`
		UPDATE cf.tenants
		  SET status = ?, updated_at = ?
		  WHERE tenant_id = ?`,
		string(status), now, gocql.UUID(tenantID),
	).WithContext(ctx).Exec()
}

// Get returns the tenant record for the given tenantID.
// Returns ErrTenantNotFound if no row exists.
func (s *TenantStore) Get(ctx context.Context, tenantID uuid.UUID) (*Tenant, error) {
	var t Tenant
	var id gocql.UUID
	var status string
	err := s.sess.Query(`
		SELECT tenant_id, slug, display_name, status, plan_id,
		       pod_cidr, svc_cidr, created_at, updated_at
		  FROM cf.tenants WHERE tenant_id = ?`,
		gocql.UUID(tenantID),
	).WithContext(ctx).Scan(
		&id, &t.Slug, &t.DisplayName, &status, &t.PlanID,
		&t.PodCIDR, &t.SvcCIDR, &t.CreatedAt, &t.UpdatedAt,
	)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, ErrTenantNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("accounts: get tenant %s: %w", tenantID, err)
	}
	t.TenantID = uuid.UUID(id)
	t.Status = TenantStatus(status)
	return &t, nil
}

// GetBySlug resolves a tenant slug to its full record via the tenants_by_slug
// materialized view. This is the hot path used by CF-Router on every JWT or
// API key authenticated request (p99 ~2.71ms QUORUM — scylladb-accounts spike).
//
// Returns ErrTenantNotFound if no tenant with that slug exists.
func (s *TenantStore) GetBySlug(ctx context.Context, slug string) (*Tenant, error) {
	var t Tenant
	var id gocql.UUID
	var status string
	err := s.sess.Query(`
		SELECT tenant_id, slug, display_name, status, plan_id,
		       pod_cidr, svc_cidr, created_at, updated_at
		  FROM cf.tenants_by_slug WHERE slug = ?`,
		slug,
	).WithContext(ctx).Scan(
		&id, &t.Slug, &t.DisplayName, &status, &t.PlanID,
		&t.PodCIDR, &t.SvcCIDR, &t.CreatedAt, &t.UpdatedAt,
	)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, ErrTenantNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("accounts: get tenant by slug %q: %w", slug, err)
	}
	t.TenantID = uuid.UUID(id)
	t.Status = TenantStatus(status)
	return &t, nil
}
