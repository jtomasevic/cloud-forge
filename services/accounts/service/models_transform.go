package service

import (
	"github.com/jtomasevic/cloud-forge/internal/accounts"
)

// ToAccountResultFromTenant converts an internal accounts.Tenant into the
// service-layer AccountResult. Keeps internal DB models from leaking upward.
func ToAccountResultFromTenant(t *accounts.Tenant) AccountResult {
	return AccountResult{
		TenantID:    t.TenantID,
		Slug:        t.Slug,
		DisplayName: t.DisplayName,
		Status:      string(t.Status),
		Plan:        t.PlanID,
		PodCIDR:     t.PodCIDR,
		ServiceCIDR: t.SvcCIDR,
		CreatedAt:   t.CreatedAt,
	}
}
