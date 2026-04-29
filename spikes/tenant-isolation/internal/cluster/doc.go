// Package cluster provides the infrastructure layer for the tenant isolation spike.
//
// It contains three concerns:
//
//   - [RealClient]   — implements [probe.KubectlClient] by shelling out to kubectl.
//   - [Install]      — checks and installs prerequisite CLI tools (vcluster, helm).
//   - [VCluster]     — lifecycle helpers: create, connect (kubeconfig), delete.
package cluster
