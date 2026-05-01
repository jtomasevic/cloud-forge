package provisioner

import (
	"context"
	"errors"
	"fmt"
	"strings"

	openbao "github.com/openbao/openbao/api/v2"
)

// ErrNotFound is returned by Retrieve when no kubeconfig has been stored for
// the requested tenant. Callers should test for this with errors.Is:
//
//	kc, err := provisioner.Retrieve(ctx, client, tenantID)
//	if errors.Is(err, provisioner.ErrNotFound) {
//	    // tenant was never provisioned, or Revoke was already called
//	}
var ErrNotFound = errors.New("provisioner: kubeconfig not found")

// kvMount is the KV v2 secret engine mount that CloudForge uses for all
// tenant and platform secrets. In dev mode, OpenBao pre-mounts "secret" as
// KV v2. In production, this mount is created during platform bootstrap.
const kvMount = "secret"

// kubeconfigKey is the field name within the KV v2 secret data map that holds
// the kubeconfig YAML string. Storing the kubeconfig under a named key (rather
// than treating the entire map as the value) lets us add sibling metadata fields
// in the future — e.g. vcluster_version, created_at, rotated_at — without
// changing the path structure or migrating existing secrets.
const kubeconfigKey = "kubeconfig"

// kvPath returns the canonical KV v2 path for a tenant's kubeconfig.
//
// Path anatomy:
//
//	cf/tenants/{tenant-id}/kubeconfig
//	│   │       │           └─ distinguishes kubeconfig from other future secrets
//	│   │       └─────────── tenant namespace (Kubernetes-valid, lowercase)
//	│   └─────────────────── tenant secrets subtree
//	└─────────────────────── CloudForge secrets root
//
// This structure matches the OpenBao policy model described in
// docs/3-Introduce-CF-VPC.md §5.3: one policy per tenant scoped to
// "secret/data/cf/tenants/{tenant-id}/*" gives the provisioner read/write
// access to exactly that tenant's secrets and nothing else.
func kvPath(tenantID string) string {
	return "cf/tenants/" + tenantID + "/kubeconfig"
}

// Store saves a tenant's vCluster kubeconfig YAML into OpenBao at the path
// cf/tenants/{tenantID}/kubeconfig.
//
// Store is idempotent: repeated calls for the same tenant create a new KV v2
// version, which OpenBao retains (default: last 10 versions). This means a
// rotation workflow can call Store with the new kubeconfig, verify connectivity,
// then call Revoke on the old version if needed — without a window of zero
// availability.
//
// The provisioner calls Store immediately after the vCluster API server becomes
// ready and a kubeconfig is issued by the vCluster CLI. If Store fails,
// provisioning should be retried; an un-stored kubeconfig means the provisioner
// cannot communicate with the tenant's vCluster on the next request.
func Store(ctx context.Context, client *openbao.Client, tenantID, kubeconfigYAML string) error {
	if err := validateTenantID(tenantID); err != nil {
		return err
	}
	if kubeconfigYAML == "" {
		return errors.New("provisioner: kubeconfigYAML must not be empty")
	}

	if _, err := client.KVv2(kvMount).Put(ctx, kvPath(tenantID), map[string]interface{}{
		kubeconfigKey: kubeconfigYAML,
	}); err != nil {
		return fmt.Errorf("provisioner: store kubeconfig for tenant %q: %w", tenantID, err)
	}
	return nil
}

// Retrieve fetches the kubeconfig YAML for a tenant from OpenBao.
//
// Returns ErrNotFound (test with errors.Is) if no kubeconfig has been stored
// for the tenant. This is the normal state before provisioning or after
// Revoke is called.
//
// Any other error indicates an OpenBao connectivity, authentication, or
// permission failure and should be surfaced to the operator.
//
// The provisioner calls Retrieve at the start of every provisioning job to
// obtain the connection details for the tenant's vCluster API server.
func Retrieve(ctx context.Context, client *openbao.Client, tenantID string) (string, error) {
	if err := validateTenantID(tenantID); err != nil {
		return "", err
	}

	// KVv2.Get returns (nil, nil) when the path has never been written to.
	// A non-nil error indicates an OpenBao API failure (network, auth, etc).
	secret, err := client.KVv2(kvMount).Get(ctx, kvPath(tenantID))
	if err != nil {
		// Translate 404 (e.g. path existed but all versions were deleted) into
		// the typed ErrNotFound so callers can distinguish it from other failures.
		if isNotFound(err) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("provisioner: retrieve kubeconfig for tenant %q: %w", tenantID, err)
	}
	if secret == nil || secret.Data == nil {
		// Path was never written to — OpenBao KV v2 returns nil for missing paths.
		return "", ErrNotFound
	}

	val, ok := secret.Data[kubeconfigKey].(string)
	if !ok || val == "" {
		// The secret exists but lacks the expected field. This should not happen
		// in normal operation (Store always writes kubeconfigKey), but is possible
		// if a secret was written manually with the wrong structure.
		return "", fmt.Errorf("provisioner: kubeconfig field %q missing or empty for tenant %q", kubeconfigKey, tenantID)
	}
	return val, nil
}

// Revoke permanently deletes a tenant's kubeconfig from OpenBao, including all
// historical versions.
//
// Revoke is idempotent: calling it for a tenant that has no stored kubeconfig
// returns nil. This lets deprovisioning workflows call Revoke unconditionally
// in their cleanup sequence, even if provisioning failed before Store was called.
//
// Important: Revoke removes the kubeconfig from OpenBao but does NOT invalidate
// the vCluster service account token embedded in that kubeconfig. Full access
// revocation also requires deleting the Kubernetes ServiceAccount inside the
// vCluster, which is handled by the deprovisioning workflow as a separate step.
func Revoke(ctx context.Context, client *openbao.Client, tenantID string) error {
	if err := validateTenantID(tenantID); err != nil {
		return err
	}

	// DeleteMetadata removes the KV v2 key and ALL its historical versions.
	// Regular Delete only soft-deletes the latest version, leaving metadata and
	// older versions recoverable. For deprovisioning we want a hard delete so
	// credentials cannot be recovered after a tenant is removed.
	//
	// OpenBao returns HTTP 204 No Content for DELETE /metadata/... even when the
	// path never existed, so idempotence is guaranteed by the API itself and no
	// explicit not-found check is required.
	if err := client.KVv2(kvMount).DeleteMetadata(ctx, kvPath(tenantID)); err != nil {
		return fmt.Errorf("provisioner: revoke kubeconfig for tenant %q: %w", tenantID, err)
	}
	return nil
}

// validateTenantID returns an error if id is not a valid CloudForge tenant
// identifier. Tenant IDs are used directly as Kubernetes namespace names, so
// they must follow Kubernetes namespace naming rules (RFC 1123 DNS label):
// lowercase alphanumeric and hyphens, not starting or ending with a hyphen.
//
// validateTenantID delegates to validateNamespace, which is the canonical
// implementation of that rule in this package, shared with CNP rendering.
func validateTenantID(id string) error {
	if err := validateNamespace(id); err != nil {
		return fmt.Errorf("tenantID %w", err)
	}
	return nil
}

// isNotFound reports whether an OpenBao client error means the secret path
// does not exist. The OpenBao KV v2 client can signal this in two ways:
//
//  1. A *ResponseError with StatusCode 404 — returned when the metadata path
//     exists but the version has been deleted, or from the raw HTTP layer.
//  2. A text error starting with "secret not found:" — returned by the KVv2
//     high-level client when the path was never written to (the underlying
//     Logical().Read returns nil and the SDK wraps it in this message).
//
// Both forms are normalised to ErrNotFound so callers need not inspect the
// error message directly.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	var re *openbao.ResponseError
	if errors.As(err, &re) && re.StatusCode == 404 {
		return true
	}
	// KVv2.Get returns "secret not found: at <mount>/<path>" when the secret
	// path has never been written to (path returns nil from the logical client).
	return strings.HasPrefix(err.Error(), "secret not found:")
}
