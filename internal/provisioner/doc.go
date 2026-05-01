// Package provisioner contains the building blocks for the CloudForge tenant
// provisioner service (CF-Provisioner).
//
// # Responsibility
//
// When a new tenant is onboarded, the provisioner must:
//  1. Create a vCluster on the host Kubernetes cluster.
//  2. Apply a CiliumNetworkPolicy that enforces default-deny isolation.
//  3. Store the vCluster kubeconfig in OpenBao so every provisioner replica
//     and every subsequent provisioning job can reach that tenant's API server.
//
// This package provides the building blocks for steps 2 and 3. Step 1 is
// handled by the vCluster CLI and is out of scope here.
//
// # Isolation model
//
// Every tenant namespace and the platform namespace (cf-system) receives the
// same baseline Cilium policy: allow ingress only from pods inside the same
// namespace, deny everything else. Because Cilium switches to default-deny the
// moment any CiliumNetworkPolicy selects an endpoint, this single rule is
// sufficient to achieve full cross-namespace isolation at the eBPF layer.
//
// Validated in: spikes/cilium-enforcement/FINDINGS.md
//
// # Kubeconfig storage model
//
// The provisioner communicates with tenant vClusters exclusively via the
// vCluster Kubernetes API server, using a kubeconfig stored in OpenBao. There
// is no direct pod-to-pod connection between the platform network and any
// tenant environment.
//
// OpenBao KV v2 path structure:
//
//	secret/cf/tenants/{tenant-id}/kubeconfig
//
// One OpenBao policy per tenant scopes access to exactly that path:
//
//	path "secret/data/cf/tenants/{tenant-id}/*" { capabilities = ["create", "read", "update", "delete"] }
//	path "secret/metadata/cf/tenants/{tenant-id}/*" { capabilities = ["read", "delete", "list"] }
//
// Validated in: spikes/tenant-isolation/FINDINGS.md (follow-up action 2)
//
// # Sub-packages (planned)
//
// Future sub-packages will cover vCluster lifecycle management, NATS-routed
// provisioning events, and quota enforcement. CNP rendering and kubeconfig
// storage live here because they are the first operations required at tenant
// creation time, before any other resource.
package provisioner
