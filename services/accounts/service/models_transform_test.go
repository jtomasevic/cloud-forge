package service_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jtomasevic/cloud-forge/internal/accounts"
	. "github.com/jtomasevic/cloud-forge/services/accounts/service"
)

func TestToAccountResultFromTenant_FullRecord(t *testing.T) {
	id := uuid.New()
	now := time.Now().UTC()
	tenant := &accounts.Tenant{
		TenantID:    id,
		Slug:        "acme",
		DisplayName: "Acme Corp",
		Status:      accounts.TenantStatusActive,
		PlanID:      "pro",
		PodCIDR:     "10.100.1.0/24",
		SvcCIDR:     "10.200.1.0/24",
		CreatedAt:   now,
	}

	result := ToAccountResultFromTenant(tenant)

	if result.TenantID != id {
		t.Errorf("tenant_id: got %s, want %s", result.TenantID, id)
	}
	if result.Slug != "acme" {
		t.Errorf("slug: got %q", result.Slug)
	}
	if result.DisplayName != "Acme Corp" {
		t.Errorf("display_name: got %q", result.DisplayName)
	}
	if result.Status != string(accounts.TenantStatusActive) {
		t.Errorf("status: got %q", result.Status)
	}
	if result.Plan != "pro" {
		t.Errorf("plan: got %q", result.Plan)
	}
	if result.PodCIDR != "10.100.1.0/24" {
		t.Errorf("pod_cidr: got %q", result.PodCIDR)
	}
	if result.ServiceCIDR != "10.200.1.0/24" {
		t.Errorf("service_cidr: got %q", result.ServiceCIDR)
	}
	if !result.CreatedAt.Equal(now) {
		t.Errorf("created_at: got %v, want %v", result.CreatedAt, now)
	}
}

func TestToAccountResultFromTenant_ProvisiningStatus(t *testing.T) {
	tenant := &accounts.Tenant{
		TenantID: uuid.New(),
		Slug:     "new-tenant",
		Status:   accounts.TenantStatusProvisioning,
		PlanID:   "starter",
	}

	result := ToAccountResultFromTenant(tenant)

	if result.Status != string(accounts.TenantStatusProvisioning) {
		t.Errorf("status: got %q, want PROVISIONING", result.Status)
	}
	// CIDRs are empty until provisioning completes.
	if result.PodCIDR != "" {
		t.Errorf("pod_cidr should be empty before ACTIVE, got %q", result.PodCIDR)
	}
}
