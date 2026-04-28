# Spike 0.8 — Knative Scale-to-Zero Cold Start

**Part of CloudForge Phase 0 foundation spikes.**

Measures the cold-start latency of Knative Serving functions after scale-to-zero
across three image size classes, and produces the minimum-replica guidance used in
CF-FunctionTrigger platform documentation.

---

## Background

CF-FunctionTrigger (Phase 6) deploys every consumer workload as a Knative
`Service` with `minScale=0` by default.  When a function has been idle for 30
seconds Knative terminates its pod.  The *next* request must wait for:

```
Autoscaler detects 0 pods
    → Kubernetes schedules the pod
    → Container runtime pulls the image layer (if not cached on the node)
    → Go process starts, HTTP server binds
    → Knative health probe passes
    → Request is forwarded and answered
```

Image size drives most of this latency on first cold start.  This spike
quantifies the cost for three representative function weight classes.

---

## Questions This Spike Answers

| # | Question | Threshold |
|---|---|---|
| Q1 | Cold-start p95 for minimal function (<10 MB image)? | < 3 s |
| Q2 | Cold-start p95 for medium function (~100 MB image)? | < 5 s |
| Q3 | Cold-start p95 for heavy function (~500 MB image)? | Documented — triggers min-replicas=1 |
| Q4 | Does a pure Go `net/http` client work against a Knative service URL? | Yes |
| Q5 | What `minScale` value eliminates cold start entirely? | 1 (always-on) |

---

## Prerequisites

| Tool | Version | Install |
|---|---|---|
| k3d | ≥ 5.7 | `brew install k3d` |
| kubectl | any | `brew install kubectl` |
| Docker | any | Docker Desktop |
| Go | 1.26+ | `brew install go` |
| ko | ≥ 0.18 *(optional)* | `brew install ko` |

**`ko` is optional** — the default build path uses plain `docker build` + `k3d image import`
which requires no extra tools beyond Docker.  Set `USE_KO=1` only if you want smaller
images and have `ko` installed via Homebrew.

> **Note on `go install github.com/ko-build/ko/cmd/ko@latest`:** This path was removed
> in `ko` v0.18.  The only supported install methods are `brew install ko` (macOS)
> or the [official installer script](https://ko.build/install/).

The `cloudforge-dev` k3d cluster from `make dev-up` (root Makefile) must
be running.  The cluster exposes port 9080→80 on the loadbalancer, which is
the port Knative Kourier uses.

---

## Quick Start

```bash
# 1. Install Go dependencies
make setup                       # go mod tidy

# 2. Install Knative Serving + net-kourier on the cluster
make deploy-knative

# 3. Build images (docker build + k3d import) and deploy Knative Services
#    Payloads are skipped automatically if already generated.
make deploy-functions            # default: uses docker (no ko needed)
# make deploy-functions USE_KO=1  # optional: use ko (requires brew install ko)

# 4. Wait 30 s for scale-to-zero, then run the benchmark
sleep 30
make measure                     # prints the full performance table

# 5. Tear down when done
make teardown
```

---

## Benchmark Output

After `make measure` the tool prints a table like this to stdout:

```
╔══════════════════════════════════════════════════════════════════════════════════════╗
║  CloudForge — Knative Scale-to-Zero Cold Start Benchmark                            ║
║  Cluster : cloudforge-dev (k3d)    Knative Serving v1.15.0   net-kourier            ║
║  Platform: darwin/arm64 go1.26     Started: 2026-04-27 03:00 UTC                    ║
╠══════════════╦══════════╦══════════╦═════════╦═════════╦═════════╦═════════╦════════╣
║ Variant      ║ Image    ║ p50      ║ p75     ║ p95     ║ p99     ║ min     ║ max    ║
╠══════════════╬══════════╬══════════╬═════════╬═════════╬═════════╬═════════╬════════╣
║ minimal      ║    8 MB  ║  1.21s   ║  1.43s  ║  2.08s  ║  2.71s  ║  0.89s  ║  2.71s ║
║ medium       ║   98 MB  ║  2.84s   ║  3.21s  ║  4.12s  ║  4.67s  ║  2.14s  ║  4.67s ║
║ heavy        ║  512 MB  ║  5.31s   ║  6.14s  ║  7.44s  ║  8.23s  ║  4.84s  ║  8.23s ║
╚══════════════╩══════════╩══════════╩═════════╩═════════╩═════════╩═════════╩════════╝

─── Threshold Analysis (p95) ──────────────────────────────────────────────────────────
  ✓ minimal   : p95 2.08s  — below 3.00s threshold. Scale-to-zero is safe.
  ⚠ medium    : p95 4.12s  — within 5.00s threshold but close. Recommend min-replicas=1 for AI functions.
  ✗ heavy     : p95 7.44s  — EXCEEDS 10.00s threshold. min-replicas=1 REQUIRED.

─── Recommendation ────────────────────────────────────────────────────────────────────
  CF-FunctionTrigger default  minScale=0  maxScale=10
  Override for medium         minScale=1  (strongly recommended for AI functions)
  Override for heavy          minScale=1  (enforced by admission webhook)
```

Exit codes: `0` = all variants pass, `1` = one or more variants fail their p95 threshold.

---

## Makefile Reference

### Setup

| Target | Description |
|---|---|
| `make setup` | Run `go mod tidy` to generate `go.sum` |

### Knative Lifecycle

| Target | Description |
|---|---|
| `make deploy-knative` | Install Knative Serving v1.15.0 + net-kourier + magic DNS patch |
| `make undeploy-knative` | Remove Knative Serving from the cluster |
| `make knative-status` | Show Knative pods and deployments |

### Functions

| Target | Description |
|---|---|
| `make gen-payloads` | Generate 50 MB + 200 MB synthetic payload files |
| `make deploy-functions` | Build images with `ko` and apply Knative Services (calls `gen-payloads` first) |
| `make undeploy-functions` | Delete all three Knative Services |
| `make functions-status` | Show Knative Service and pod status |

### Benchmark

| Target | Description |
|---|---|
| `make measure` | Run all 3 variants × 10 samples and print the performance table |
| `make measure-minimal` | Benchmark minimal variant only |
| `make measure-medium` | Benchmark medium variant only |
| `make measure-heavy` | Benchmark heavy variant only |
| `make measure SAMPLES=5` | Run with a custom sample count |

### Testing

| Target | Description |
|---|---|
| `make test` | Run all unit tests with `-race` |
| `make test-coverage` | Run unit tests with coverage report (target ≥ 90%) |

### Code Quality

| Target | Description |
|---|---|
| `make fmt` | Run `gofmt` on all Go files |
| `make vet` | Run `go vet` |
| `make lint` | Run `golangci-lint` |
| `make tidy` | Run `go mod tidy` |

### Lifecycle Shortcuts

| Target | Description |
|---|---|
| `make spike-run` | Full install: `deploy-knative` + `deploy-functions` |
| `make teardown` | Full cleanup: remove functions + Knative |
| `make clean` | Remove `coverage.out`, `coverage.html`, `bin/` |

---

## Code Structure

```
spikes/knative-coldstart/
├── cmd/
│   └── measure/
│       └── main.go                  # CLI entry point — parses flags, wires dependencies
├── functions/
│   ├── minimal/
│   │   └── main.go                  # Pure Go HTTP handler, distroless base
│   ├── medium/
│   │   ├── main.go                  # Handler + //go:embed payload.bin (50 MB)
│   │   └── payload.bin              # 1-byte placeholder; replaced by make gen-payloads
│   └── heavy/
│       ├── main.go                  # Handler + //go:embed payload.bin (200 MB)
│       └── payload.bin              # 1-byte placeholder; replaced by make gen-payloads
├── internal/
│   └── measure/
│       ├── doc.go                   # Package-level godoc
│       ├── types.go                 # Variant, Sample, Stats, BenchmarkResult, Config
│       ├── stats.go                 # ComputeStats, percentileAt, FormatDuration
│       ├── stats_test.go            # ≥90% coverage of stats.go and types.go
│       ├── probe.go                 # Prober interface + HTTPProber
│       ├── probe_test.go            # httptest-based TTFB measurement tests
│       ├── poller.go                # PodCounter interface + KubectlPodCounter + WaitForScaleToZero
│       ├── poller_test.go           # Mock-based scale-to-zero polling tests
│       ├── runner.go                # Runner.RunVariant + RunAll
│       ├── runner_test.go           # Mock-based runner tests
│       ├── table.go                 # PrintTable (renders terminal table)
│       └── table_test.go            # Output content and symbol verification
├── deploy/
│   ├── config-domain.yaml           # Knative magic DNS patch (127.0.0.1.sslip.io)
│   ├── service-minimal.yaml         # Knative Service for fn-minimal
│   ├── service-medium.yaml          # Knative Service for fn-medium
│   └── service-heavy.yaml           # Knative Service for fn-heavy
├── Makefile
├── go.mod
├── .gitignore
├── README.md
└── FINDINGS.md
```

---

## Internal Architecture

### Component Dependency Graph

```
cmd/measure/main.go
    │
    ├── measure.NewHTTPProber(timeout)          ← implements Prober
    ├── &measure.KubectlPodCounter{}            ← implements PodCounter
    ├── measure.DefaultConfig()                 ← benchmark parameters
    │
    └── measure.NewRunner(prober, counter, cfg, logger)
            │
            ├── runner.RunAll(ctx)
            │       │
            │       └── for each Variant in AllVariants:
            │               runner.RunVariant(ctx, variant)
            │                       │
            │                       ├── WaitForScaleToZero(ctx, counter, svc, ns, ...)
            │                       │       └── counter.ReadyPods(ctx, svc, ns) → polling loop
            │                       │
            │                       └── prober.Probe(ctx, url)
            │                               └── HTTPProber.Probe → TTFB measurement
            │
            └── measure.PrintTable(os.Stdout, result)
                    ├── printHeader
                    ├── printStatsTable
                    ├── printThresholdAnalysis
                    └── printRecommendation
```

### Interface Design for Testability

Both `Prober` and `PodCounter` are Go interfaces, enabling full unit test
coverage without a live cluster:

```go
// In tests, swap real implementations for lightweight inline mocks:

type mockProber struct { responses []probeResponse }
func (m *mockProber) Probe(ctx, url) (time.Duration, error) { ... }

type mockPodCounter struct { responses []podCountResponse }
func (m *mockPodCounter) ReadyPods(ctx, svc, ns) (int, error) { ... }
```

This pattern is used in `runner_test.go` and `poller_test.go`.

---

## How the Benchmark Measures Cold-Start Latency

The measurement uses **time-to-first-byte (TTFB)** — the elapsed time from the
moment `http.Client.Do()` is called to the moment the first byte of the response
body is available.

```go
start := time.Now()
resp, _ := client.Do(req)
buf := make([]byte, 1)
resp.Body.Read(buf)           // ← first byte received
ttfb := time.Since(start)
```

Reading exactly one byte (not the full body) is deliberate:

1. **Accuracy**: it captures the full cold-start chain including pod scheduling,
   image pull, and process startup, while excluding variable body-size transfer time.

2. **Scale-to-zero trigger**: a long-lived open connection keeps the function
   "active" in the Knative autoscaler's view.  Closing after one byte allows
   scale-to-zero to trigger for the next benchmark round.

---

## Findings

See [FINDINGS.md](FINDINGS.md) — populated after running `make measure`.
