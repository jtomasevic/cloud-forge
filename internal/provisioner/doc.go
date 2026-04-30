// Package provisioner contains the building blocks for the CloudForge tenant
// provisioner service.
//
// When a new tenant namespace is created, the provisioner must immediately
// apply a CiliumNetworkPolicy that enforces the default-deny isolation model
// validated in the Cilium enforcement spike (spikes/cilium-enforcement).
//
// # Isolation model
//
// Every tenant namespace and the platform namespace (cf-system) receives the
// same baseline policy: allow ingress only from pods inside the same namespace,
// deny everything else. Because Cilium switches to default-deny the moment any
// CNP selects an endpoint, this single rule is sufficient to achieve full
// cross-namespace isolation at the eBPF layer.
//
// # Sub-packages (planned)
//
// Future sub-packages will cover vCluster lifecycle, kubeconfig storage, and
// NATS-routed provisioning events. CNP rendering lives here because it is the
// first piece required at namespace creation time, before any other resource.
package provisioner
