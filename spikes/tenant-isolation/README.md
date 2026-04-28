# Spike: Tenant Network Isolation (vCluster)

**Category:** Architecture, Networking, Infrastructure  
**Reference:** [docs/3-Introduce-CF-VPC.md §11](../../docs/3-Introduce-CF-VPC.md) — Recommended Tenant Isolation Spike  
**Status:** In progress — run `make run` to execute

---

## Purpose

Validate that vCluster-based per-tenant virtual clusters meet CloudForge's requirements for network isolation, operational complexity, provisioning speed, and resource overhead — before vCluster is adopted as the production isolation primitive.

The expected output is a go/no-go decision in `FINDINGS.md` and a host-cluster sizing formula.

---

## What is Tested

| # | Test | Question | Pass Threshold |
|---|------|----------|----------------|
| 1 | **Network isolation** | Is cross-tenant TCP/DNS traffic topologically blocked? | 100% of vectors blocked |
| 2 | **Provisioning speed** | How long does vCluster creation + NATS take? | vCluster p95 < 90 s; NATS p95 < 3 min |
| 3 | **Provisioner communication** | Can the platform apply manifests to a vCluster? Is scope enforced? | 100% apply success; cross-scope blocked |
| 4 | **Resource overhead** | How much RAM/CPU does an idle vCluster consume? | < 300 MiB RAM, < 100 m CPU |
| 5 | **Cilium enforcement** | Does Cilium eBPF deny cross-namespace TCP? | 100% denied (SKIP if Cilium absent) |
| 6 | **Failure recovery** | How fast does the vCluster API server recover after a crash? | < 60 s to Ready |

---

## Architecture

```
cmd/probe/main.go          — minimal entry point: flags + wire + PrintResults
internal/
  cluster/
    client.go              — KubectlClient interface + RealClient (kubectl shim)
    install.go             — prerequisite detection + installation hints
    vcluster.go            — CreateVCluster / DeleteVCluster / KubeconfigPath
  probe/
    types.go               — TestResult, Config, KubectlClient interface, parsers
    fake_client.go         — FakeClient test double for all unit tests
    isolation.go           — Test 1: network isolation (nc + nslookup)
    speed.go               — Test 2: provisioning speed (NATS timing)
    provisioner.go         — Test 3: provisioner communication scope
    overhead.go            — Test 4: resource overhead + SizingFormula
    cilium.go              — Test 5: Cilium eBPF enforcement
    recovery.go            — Test 6: API server failure recovery
    runner.go              — RunAll: sequential test orchestrator
    table.go               — formatted terminal results table
```

All six tests accept a `KubectlClient` interface. Unit tests inject `FakeClient`; production runs use `RealClient` (kubectl subprocess).

---

## Prerequisites

The `make run` command installs missing tools automatically on macOS (Homebrew).

| Tool | Install | Required |
|------|---------|----------|
| kubectl | `brew install kubectl` | Yes |
| k3d | `brew install k3d` | Yes |
| helm | `brew install helm` | Yes |
| vcluster CLI | `brew install loft-sh/tap/vcluster` | Yes |

**The k3d cluster `cloudforge-dev` must already be running.** Start it from the repo root:

```bash
make dev-up    # from /cloud-forge root
```

---

## Quick Start

```bash
# Full spike run (installs tools if needed, creates vClusters, runs 6 tests)
make run

# Clean up vClusters when done (keeps the k3d cluster running)
make clean
```

---

## All Commands

| Command | What it does |
|---------|-------------|
| `make run` | Full end-to-end: tools → cluster check → vClusters → 6 tests → results |
| `make clean` | Delete tenant vClusters + generated kubeconfigs |
| `make test` | Run unit tests (no cluster required) |
| `make test-coverage` | Unit tests + coverage % per package |
| `make build` | Build `bin/probe` |
| `make create-vclusters` | Create tenant-a and tenant-b vClusters only |
| `make run-probe` | Run the probe binary against existing vClusters |
| `make check-tools` | Show installed tool versions |
| `make install-metrics-server` | Deploy metrics-server (needed for Test 4) |

---

## Environment Details

- **Host cluster:** k3d `cloudforge-dev` (single node, used by Spike 0.7 and 0.8)
- **vCluster A namespace:** `vcluster-tenant-a`
- **vCluster B namespace:** `vcluster-tenant-b`
- **Generated kubeconfigs:** `kubeconfigs/tenant-a.kubeconfig`, `kubeconfigs/tenant-b.kubeconfig`
- **Note:** kubeconfigs are in `.gitignore` because they contain credentials

---

## Configuration

All defaults match the pass/fail thresholds from `docs/3-Introduce-CF-VPC.md §11.5`.

```bash
# Override via environment or flags:
make run SPEED_SAMPLES=5 TENANT_A=vpc-a TENANT_B=vpc-b

# Or pass flags directly to the binary:
bin/probe -speed-samples 5 -verbose -skip-create
```

---

## Test Coverage

The `internal/probe` package targets ≥ 90% unit test coverage.
All six tests are exercised with `FakeClient` without requiring a live cluster.

```bash
make test-coverage
```

---

## Results

See `FINDINGS.md` after running the spike.
