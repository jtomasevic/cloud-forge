# Spike: Cilium Network Policy Enforcement

This spike validates that Cilium's eBPF-based network policy enforcement correctly
isolates tenant namespaces at the host-cluster level, independently of vCluster topology.

It is the second layer of the CloudForge tenant isolation model:

```
Layer 1 (vCluster topology):  separate pod CIDRs + isolated CoreDNS  ← validated in Spike 0.9
Layer 2 (Cilium eBPF):        CiliumNetworkPolicy deny-by-default     ← this spike
```

Together, these two layers make cross-tenant traffic structurally impossible at the
network level (Layer 1) AND policy-enforced at the kernel level (Layer 2).

---

## How It Works

### The enforcement model

Cilium replaces the default k3s CNI (flannel). Every network packet between pods
passes through an eBPF program attached to each pod's network interface. That program
looks up the packet's source and destination in a BPF map that mirrors the
`CiliumNetworkPolicy` objects. The decision (ALLOW or DROP) happens inside the Linux
kernel — before the packet ever reaches the target pod's TCP stack.

```
┌──────────────────────────────────────────────────────────────────────────────────┐
│  kernel (per node)                                                               │
│                                                                                  │
│  Pod A (tenant-b)  ──SYN──▶  eBPF hook  ──────────────────▶  Pod B (tenant-a)  │
│                                  │                                               │
│                           ┌──────┴──────┐                                       │
│                           │  BPF map    │   ← mirrors CiliumNetworkPolicy        │
│                           │  (policy    │     objects; updated by Cilium agent   │
│                           │   verdicts) │     on every CNP change                │
│                           └──────┬──────┘                                       │
│                                  │                                               │
│                     ALLOW ───────┴─────── DROP                                  │
│                       │                     │                                   │
│                    forwarded              silently                               │
│                    to Pod B               discarded                              │
│                                           (exit 28)                             │
└──────────────────────────────────────────────────────────────────────────────────┘
```

### Policy model: deny-by-default

Each tenant namespace gets a `CiliumNetworkPolicy` (CNP) at provisioning time.
The CNP uses an empty `endpointSelector` (matches all pods in the namespace) and
a single ingress rule: only allow traffic from pods **in the same namespace**.
Everything else is dropped.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  Logical policy view                                                        │
│                                                                             │
│  cilium-tenant-a                      cilium-tenant-b                      │
│  ┌──────────────────────────┐         ┌──────────────────────────┐         │
│  │  echo-pod (:8080)        │         │  probe-pod               │         │
│  │                          │◀── ✗ ───│  curl echo-pod:8080      │         │
│  │  CNP: deny cross-ns      │  DROP   │                          │         │
│  │  ingress                 │         └──────────────────────────┘         │
│  │                          │                                               │
│  │  allow-probe-pod         │                                               │
│  │  curl echo-pod:8080 ─────┼──────▶  ALLOW (same namespace)              │
│  └──────────────────────────┘                                               │
│                                                                             │
│  cf-system (platform)                                                       │
│  ┌──────────────────────────┐                                               │
│  │  platform-service        │◀── ✗ ───  any tenant pod           DROP      │
│  │  CNP: deny tenant ingress│                                               │
│  └──────────────────────────┘                                               │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Component diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  k3d cluster: cloudforge-dev                                                │
│                                                                             │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │  kube-system                                                          │  │
│  │                                                                       │  │
│  │  ┌─────────────────────┐      ┌─────────────────────┐               │  │
│  │  │  cilium-agent       │      │  hubble-relay        │               │  │
│  │  │  (DaemonSet)        │      │  (Deployment)        │               │  │
│  │  │                     │      │                      │               │  │
│  │  │  • enforces CNPs    │─────▶│  • aggregates flow   │               │  │
│  │  │    via eBPF maps    │      │    events from all   │               │  │
│  │  │  • records every    │      │    nodes             │               │  │
│  │  │    flow verdict     │      │  • gRPC :4245        │               │  │
│  │  │    in ring buffer   │      └──────────┬───────────┘               │  │
│  │  └─────────────────────┘                 │                           │  │
│  └───────────────────────────────────────── │ ──────────────────────────┘  │
│                                             │                               │
│  ┌─────────────────────┐  ┌──────────────── │ ──────────────────────────┐  │
│  │  cilium-tenant-a    │  │  cilium-tenant-b│                           │  │
│  │  echo-pod (:8080)   │  │  probe-pod      │                           │  │
│  │  allow-probe-pod    │  │  (attacker)     │                           │  │
│  │  CNP: deny cross-ns │  └─────────────────────────────────────────── ┘  │
│  └─────────────────────┘                                                   │
│                                                                             │
│  ┌─────────────────────┐  ┌──────────────────────────────────────────────┐ │
│  │  cf-system          │  │  vcluster-pilot                              │ │
│  │  platform-svc pod   │  │  vCluster StatefulSet (k3s control plane)    │ │
│  │  CNP: deny tenants  │  │  echo-pod, CNP: deny cross-ns                │ │
│  └─────────────────────┘  └──────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │ kubectl port-forward :4245
                                    ▼
                         ┌──────────────────────┐
                         │  Host machine (macOS) │
                         │  hubble observe CLI   │
                         │  (reads flow stream)  │
                         └──────────────────────┘
```

### Hubble observability data flow

Hubble is Cilium's built-in observability layer. It hooks into the same eBPF programs
that enforce policies and records every packet decision as a structured event.

```
Pod A (tenant-b)  ──SYN──▶  eBPF hook  ──DROP──▶  (discarded)
                                 │
                                 ▼
                    Hubble agent (per-node ring buffer)
                    records: {src_ns, src_pod, dst_ns,
                              dst_pod, port, verdict=DROP,
                              policy_identity, timestamp}
                                 │
                                 ▼
                    Hubble Relay (kube-system)
                    aggregates events from all nodes
                    exposes gRPC on :4245
                                 │
                    kubectl port-forward ──▶ localhost:4245
                                 │
                          ┌──────┴──────────────────┐
                          │  hubble observe --follow │  ← terminal stream
                          │  make hubble-observe-    │
                          │        dropped           │
                          └─────────────────────────-┘
                                 │
                          ┌──────┴──────┐
                          │  Hubble UI  │  ← browser service map (optional)
                          │  (browser)  │    make hubble-ui
                          └─────────────┘
```

### CNP provisioning flow

When the platform provisions a new tenant, the `internal/provisioner` package
renders the `CiliumNetworkPolicy` YAML from a Go template and applies it:

```
TenantIsolationPolicy("acme-corp")
        │
        ▼  text/template render
┌──────────────────────────────────────────┐
│ apiVersion: cilium.io/v2                 │
│ kind: CiliumNetworkPolicy                │
│ metadata:                                │
│   name: tenant-isolation                 │
│   namespace: acme-corp                   │
│ spec:                                    │
│   endpointSelector: {}                   │
│   ingress:                               │
│     - fromEndpoints:                     │
│         - matchLabels:                   │
│             io.kubernetes.pod.namespace: │
│               acme-corp                  │
└──────────────────────────────────────────┘
        │
        ▼  kubectl apply
  Cilium agent picks up the new CNP
        │
        ▼  BPF map update (kernel, ~milliseconds)
  Policy enforced for all pods in acme-corp
```

---

## Tests

| # | Test | What it validates |
|---|------|-------------------|
| 1 | `cross_namespace_deny` | CNP deny-by-default blocks TCP from tenant-B to tenant-A |
| 2 | `intra_namespace_allow` | CNP explicit allow permits traffic within the same namespace |
| 3 | `platform_isolation` | Tenant namespace cannot reach the cf-system platform namespace |
| 4 | `policy_trace` | `cilium-dbg policy trace` (primary) or `hubble observe` (fallback) confirms the DENY decision |
| 5 | `vcluster_coexistence` | CNP enforcement holds when a vCluster is running in the host namespace |

---

## Architecture

```
k3d cluster (cloudforge-dev)
│
├── cilium-tenant-a namespace
│   ├── echo pod (hashicorp/http-echo, :8080)     ← target for Tests 1, 2, 4
│   ├── allow-probe pod (nicolaka/netshoot)        ← same-namespace prober (Test 2)
│   └── CiliumNetworkPolicy: deny cross-namespace ingress
│
├── cilium-tenant-b namespace
│   └── probe pod (nicolaka/netshoot)              ← cross-namespace attacker (Tests 1, 3, 4)
│
├── cf-system namespace
│   ├── service pod (hashicorp/http-echo, :8080)  ← platform service target (Test 3)
│   └── CiliumNetworkPolicy: deny tenant ingress
│
└── vcluster-pilot namespace
    ├── vCluster StatefulSet (k3s control plane)   ← presence test (Test 5)
    ├── echo pod (hashicorp/http-echo, :8080)      ← target for Test 5
    └── CiliumNetworkPolicy: deny cross-namespace ingress
```

---

## Prerequisites

| Tool | Version | Install |
|------|---------|---------|
| kubectl | any | `brew install kubectl` |
| k3d | ≥ 5.x | `brew install k3d` |
| helm | any | `brew install helm` |
| cilium CLI | matches CILIUM_VERSION | `brew install cilium-cli` |
| hubble CLI | **must match CILIUM_VERSION** | see note below |
| vcluster CLI | any | `brew install loft-sh/tap/vcluster` |

> **hubble CLI version pinning:** `brew install hubble` installs the latest release,
> which may not match the Hubble Relay version in the cluster. A version mismatch
> causes `Error "invalid fieldmask"` when using `--verdict` filters. Install the
> matching version from GitHub releases:
>
> ```bash
> HUBBLE_VERSION=v1.17.3   # must match CILIUM_VERSION in Makefile
> curl -L --remote-name-all \
>   "https://github.com/cilium/hubble/releases/download/${HUBBLE_VERSION}/hubble-darwin-arm64.tar.gz" \
>   "https://github.com/cilium/hubble/releases/download/${HUBBLE_VERSION}/hubble-darwin-arm64.tar.gz.sha256sum"
> shasum -a 256 --check hubble-darwin-arm64.tar.gz.sha256sum
> sudo tar xzvf hubble-darwin-arm64.tar.gz -C /usr/local/bin
> ```

---

## Quick Start

```bash
# Full spike (deletes current k3d cluster, recreates with Cilium, runs 5 tests)
make run

# Re-run probe only (Cilium cluster already running)
make run-probe

# Restore standard dev cluster after the spike
make clean
```

> **Warning:** `make run` deletes the existing k3d cluster (`cloudforge-dev`).
> All running workloads (ScyllaDB, NATS, vClusters) will be lost.
> Run `make clean` when done to restore the standard cluster.

---

## Commands

### Probe

| Command | Description |
|---------|-------------|
| `make run` | Full end-to-end: recreate cluster + install Cilium + run 5 tests |
| `make clean` | Delete Cilium cluster + restore standard dev cluster |
| `make run-probe` | Run probe against existing Cilium cluster (no cluster recreation) |
| `make build` | Build the probe binary to `bin/probe` |
| `make test` | Run unit tests (no cluster required) |
| `make test-coverage` | Unit tests + coverage report |
| `make check-tools` | Show installed tool versions |

### Hubble observability

These targets require `make hubble-port-forward` to be running in a separate terminal.

| Command | Description |
|---------|-------------|
| `make hubble-port-forward` | Expose Hubble Relay on `localhost:4245` — keep this terminal open |
| `make hubble-observe` | Stream all live network flows from all nodes |
| `make hubble-observe-dropped` | Stream only DROPPED flows — isolation violation monitor |
| `make hubble-observe-tenant NS=<ns>` | Show last 200 flows for a specific namespace |
| `make hubble-ui` | Deploy Hubble UI pod and open browser service map |
| `make cilium-status` | Show Cilium agent status, active CNPs, and Hubble Relay health |

**Recommended isolation verification workflow:**

```bash
# Terminal 1 — keep open (proxies Hubble Relay to localhost)
make hubble-port-forward

# Terminal 2 — watch for policy violations in real time
make hubble-observe-dropped

# Terminal 3 — trigger cross-namespace traffic or run the probe
make run-probe
```

Any DROPPED flow appears in Terminal 2 with the full 5-tuple (src namespace,
src pod, dst namespace, dst pod, port) and the policy identity that caused the drop.

---

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `CLUSTER` | `cloudforge-dev` | k3d cluster name |
| `CILIUM_VERSION` | `1.17.3` | Cilium version to install |

Override on the command line:

```bash
make run CILIUM_VERSION=1.16.5
```

---

## Code Structure

```
spikes/cilium-enforcement/
├── Makefile
├── README.md
├── FINDINGS.md
├── go.mod
├── cmd/probe/main.go           # CLI entry point
└── internal/
    ├── cluster/
    │   ├── install.go          # prerequisite tool checks
    │   └── client.go           # RealClient: kubectl wrapper
    └── probe/
        ├── types.go            # Config, TestResult, KubectlClient interface
        ├── fake_client.go      # FakeClient for unit tests
        ├── helpers.go          # passResult, failResult, skipResult, truncate
        ├── deny.go             # Test 1: cross_namespace_deny
        ├── allow.go            # Test 2: intra_namespace_allow
        ├── platform.go         # Test 3: platform_isolation
        ├── trace.go            # Test 4: policy_trace (cilium-dbg + Hubble fallback)
        ├── coexist.go          # Test 5: vcluster_coexistence
        ├── runner.go           # RunAll orchestrator
        └── table.go            # PrintResults, PrintMetrics

# Provisioner integration (consumed by tenant provisioner):
internal/provisioner/
├── doc.go                      # package documentation
├── cnp.go                      # TenantIsolationPolicy / PlatformIsolationPolicy
└── cnp_test.go                 # unit tests (no cluster required)

# Dev cluster integration:
deploy/k3d/cluster.yaml         # --flannel-backend=none, --disable-network-policy
deploy/kustomize/base/
└── network-policies.yaml       # platform-isolation CNP for cf-system
```

---

## Test Coverage

Unit tests use a `FakeClient` that mocks all kubectl operations. The real cluster
is never needed for unit tests. Coverage target: ≥ 90% for `internal/probe`.

```bash
make test-coverage
```

---

## What Was Learned

1. **`cilium policy trace` was removed in v1.17.** The binary was split:
   `cilium` (end-user CLI) and `cilium-dbg` (debug/introspection). The probe
   was updated to use `cilium-dbg policy trace` with a Hubble observe fallback.

2. **Hubble CLI version must match Cilium version.** `brew install hubble`
   installs the latest, which can be 2+ minor versions ahead of the cluster's
   Hubble Relay. This causes `invalid fieldmask` errors on `--verdict` filters.
   Always install hubble from GitHub releases, pinned to match `CILIUM_VERSION`.

3. **eBPF enforcement is instantaneous.** CNP changes propagate from the API
   server to the BPF map in the kernel within milliseconds. There is no grace
   period — a pod that previously had access loses it the moment the CNP is
   applied.

4. **vCluster pods are subject to host-level CNPs.** Pods scheduled inside a
   vCluster namespace on the host cluster are real pods. The host Cilium agent
   enforces host-level CNPs on them. vCluster isolation does not bypass Cilium.

5. **Hubble flow records are the authoritative source of truth for policy
   verdicts** — stronger than `policy trace` (which simulates) because they
   reflect actual BPF dataplane decisions on live traffic.

---

## Results

See [FINDINGS.md](FINDINGS.md) for full results and architectural conclusions.
