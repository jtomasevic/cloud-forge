package service

import "errors"

// ErrTenantAlreadyExists is returned by Provision when the requested
// tenant_id is already present in the account store.
// Maps to HTTP 409 Conflict at the REST layer.
var ErrTenantAlreadyExists = errors.New("provisioner: tenant already exists")

// ErrJobNotFound is returned by GetJob when no provisioning job exists
// for the given ID.
// Maps to HTTP 404 Not Found at the REST layer.
var ErrJobNotFound = errors.New("provisioner: job not found")

// ErrCIDRExhausted is returned by Provision when all available /24 CIDR
// blocks in the pod and service supernets have been allocated.
// Maps to HTTP 503 Service Unavailable at the REST layer.
var ErrCIDRExhausted = errors.New("provisioner: CIDR address space exhausted")

// ErrTenantNotFound is returned by Deprovision when the requested
// tenant_id does not exist in the account store.
// Maps to HTTP 404 Not Found at the REST layer.
var ErrTenantNotFound = errors.New("provisioner: tenant not found")
