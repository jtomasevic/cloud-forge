package provisioner

import (
	"bytes"
	"errors"
	"fmt"
	"text/template"
)

// isolationCNPTemplate is the canonical CiliumNetworkPolicy template used for
// both tenant namespaces and the platform namespace.
//
// Policy semantics (validated in spikes/cilium-enforcement):
//   - endpointSelector: {} — applies to every pod in the namespace.
//   - ingress[0].fromEndpoints — allows traffic only from pods whose
//     io.kubernetes.pod.namespace label matches the protected namespace itself.
//   - Once any CNP selects an endpoint, Cilium's eBPF dataplane enforces
//     default-deny for all unmatched flows; no explicit deny rule is needed.
//
// The policy name is injected so that kubectl output distinguishes tenant
// isolation policies from platform isolation policies.
const isolationCNPTemplate = `apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: {{ .PolicyName }}
  namespace: {{ .Namespace }}
spec:
  endpointSelector: {}
  ingress:
    - fromEndpoints:
        - matchLabels:
            io.kubernetes.pod.namespace: {{ .Namespace }}
`

var parsedCNPTemplate = template.Must(template.New("cnp").Parse(isolationCNPTemplate))

// cnpData holds the values injected into the CNP template.
type cnpData struct {
	PolicyName string
	Namespace  string
}

// TenantIsolationPolicy renders a CiliumNetworkPolicy YAML that enforces
// default-deny isolation for a tenant namespace.
//
// The policy allows ingress only from pods that share the same namespace
// (intra-tenant east-west traffic). All cross-namespace ingress is denied at
// the eBPF layer by Cilium's identity model.
//
// The returned bytes are ready to be passed to kubectl apply or the Kubernetes
// client's Apply method.
func TenantIsolationPolicy(namespace string) ([]byte, error) {
	return renderCNP(namespace, "tenant-isolation")
}

// PlatformIsolationPolicy renders a CiliumNetworkPolicy YAML that enforces
// default-deny isolation for the platform namespace (cf-system).
//
// The policy has the same structure as TenantIsolationPolicy: it allows ingress
// only from pods within the same namespace. Since no tenant workloads run in
// cf-system, this effectively denies all tenant-initiated connections to the
// CloudForge control plane.
func PlatformIsolationPolicy(namespace string) ([]byte, error) {
	return renderCNP(namespace, "platform-isolation")
}

// renderCNP renders the isolation template for the given namespace and policy name.
func renderCNP(namespace, policyName string) ([]byte, error) {
	if err := validateNamespace(namespace); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := parsedCNPTemplate.Execute(&buf, cnpData{
		PolicyName: policyName,
		Namespace:  namespace,
	}); err != nil {
		return nil, fmt.Errorf("render CNP template: %w", err)
	}
	return buf.Bytes(), nil
}

// validateNamespace returns an error if namespace is not a valid Kubernetes
// namespace name. Kubernetes namespace names must be lowercase alphanumeric or
// hyphens (RFC 1123 DNS label), and must not be empty.
func validateNamespace(ns string) error {
	if ns == "" {
		return errors.New("namespace must not be empty")
	}
	for i, r := range ns {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' && i > 0 && i < len(ns)-1:
		default:
			return fmt.Errorf("namespace %q contains invalid character %q at position %d", ns, r, i)
		}
	}
	return nil
}
