// Package cluster provides prerequisite checking and a kubectl client
// for the Cilium enforcement spike.
//
// Components:
//   - install.go: CheckPrerequisites verifies that kubectl, k3d, helm, cilium CLI,
//     and vcluster are installed. Missing tools that can be auto-installed via
//     Homebrew are installed automatically; kubectl must be installed by the user.
//   - client.go: RealClient implements probe.KubectlClient by shelling out to the
//     kubectl binary. All operations accept a context for timeout/cancellation.
package cluster
