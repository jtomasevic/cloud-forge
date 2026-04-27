# Spike 0.6 — NATS JetStream Multi-Tenant Routing

## Purpose

This spike validates whether NATS JetStream, using the **accounts** isolation
model, is a suitable foundation for CF-EventRouter (Phase 5).

It answers five concrete questions before Phase 5 commits to NATS:

| # | Question | Threshold |
|---|---|---|
| Q1 | Can accounts be provisioned dynamically without a cluster restart? | Yes |
| Q2 | Is cross-account isolation complete? | Yes — zero leakage |
| Q3 | p99 publish latency for 1KB CloudEvent on a local cluster? | < 5 ms |
| Q4 | How is content-based routing implemented? | Document approach |
| Q5 | Can 50 accounts be provisioned sequentially in < 2 minutes? | Yes |

Results are written to [`FINDINGS.md`](FINDINGS.md) after running the program.

---

## Prerequisites

| Tool | Install |
|---|---|
| Docker Desktop ≥ 4.x | <https://www.docker.com/products/docker-desktop> |
| Go 1.26+ | `brew install go` |
| NATS CLI (optional) | `brew install nats-io/nats-tools/nats` |

---

## Quick Start (Makefile)

All day-to-day operations are available as `make` targets.
Run `make help` from the `spikes/nats-routing/` directory to see the full list.

```bash
# 1. Start the 3-node NATS cluster and wait for leader election
make cluster-up

# 2. Run the full spike (all 5 questions)
make run

# 3. Tear everything down when you're done
make cluster-down
```

> **Without Make:** see the [Manual Quick Start](#manual-quick-start) section below.

---

## Makefile Reference

### Cluster targets

#### `make cluster-up`

Runs `docker compose -f config/nats-cluster.yaml up -d` to start the three
NATS nodes in the background, waits 5 seconds for JetStream leader election to
complete, then automatically calls `cluster-status` so you can confirm all
nodes are healthy before running the spike.

```bash
make cluster-up
# ── Container status ──────────────────────────────────────────────
# NAME     STATUS          PORTS
# nats-1   Up 5 seconds    0.0.0.0:4222->4222/tcp, ...
# nats-2   Up 5 seconds    ...
# nats-3   Up 5 seconds    ...
#
# ── NATS monitoring ───────────────────────────────────────────────
#   node :8222  →  OK
#   node :8223  →  OK
#   node :8224  →  OK
```

#### `make cluster-down`

Stops all three containers and **removes JetStream data volumes** (`-v`).
Use this when you want a completely clean slate — all stream data, consumer
offsets, and message storage are erased.

```bash
make cluster-down
```

#### `make cluster-restart`

Convenience shortcut for `cluster-down` followed by `cluster-up`.
Useful when you want to reset the cluster to a known empty state between spike
runs.

```bash
make cluster-restart
```

#### `make cluster-status`

Queries the Docker Compose project status and polls the `/healthz` HTTP
endpoint on each monitoring port (`:8222`, `:8223`, `:8224`).  Shows `OK` when
a node is reachable and `UNREACHABLE` otherwise.  Safe to run at any time —
will not start or stop anything.

```bash
make cluster-status
```

#### `make cluster-logs`

Tails the combined stdout log of all three NATS nodes.  Each line is prefixed
with the container name (`nats-1 |`, `nats-2 |`, …) so you can observe leader
elections, config reloads, and client connection events in real time.
Press `Ctrl-C` to stop.

```bash
make cluster-logs
```

#### `make cluster-logs-node NODE=nats-1`

Same as `cluster-logs` but limited to a single named container.  Useful when
you only care about one node (e.g. after sending `SIGHUP` to reload its config).

```bash
make cluster-logs-node NODE=nats-1
```

---

### Spike targets

#### `make build`

Compiles `./cmd` to a self-contained binary called `./spike-bin`.  Useful
when you want to time the spike without the JIT overhead of `go run`, or when
you need to inspect the binary size.

```bash
make build
./spike-bin                              # run the compiled binary
NATS_URL=nats://localhost:4223 ./spike-bin  # point at node 2
```

#### `make run`

The primary way to execute the spike.  Runs `go run ./cmd` with the `NATS_URL`
environment variable forwarded.  The program connects to the cluster, executes
all five spike sections in order (Q2 → Q4 → Q3 → Q1/Q5), and prints a
formatted results table.

```bash
make run                                 # uses NATS_URL=nats://localhost:4222
make run NATS_URL=nats://localhost:4223  # connect to node 2 for failover test
```

#### `make run-bench-only`

Same as `make run`, but also forwards `BENCH_MSGS` so you can run a shorter
benchmark without modifying source code.  Handy for a quick sanity check before
running the full 10,000-message benchmark.

```bash
make run-bench-only BENCH_MSGS=100   # 100-message warm-up
make run-bench-only                  # full 10,000-message run
```

---

### Test targets

#### `make test`

Runs the complete unit test suite.  All tests use an **in-process embedded NATS
server** (via `nats-server/v2`), so **Docker is not required**.  The suite
covers all five spike components: isolation, content-based routing, latency
benchmark, provisioning, and the orchestration layer.

```bash
make test
# Running unit tests…
# ok   github.com/…/internal/spike   2.4s
# All tests passed.
```

#### `make test-race`

Identical to `make test` but passes `-race` to the Go test runner.  The race
detector instruments all channel and memory operations, adding roughly 5–10×
overhead.  Run this before merging any concurrency changes (the routing
dispatch loop and isolation test use goroutines).

```bash
make test-race
```

#### `make test-short`

Runs only tests that do **not** call `t.Skip()` under `-short` mode.  Currently
all spike tests complete quickly (< 3s total), so `test-short` is primarily
useful in CI pipelines that want a very fast pre-commit gate without the full
provisioning test (which connects to 50 accounts).

```bash
make test-short
```

#### `make test-coverage`

Runs the full test suite with `-coverprofile` and produces two outputs:

1. **Terminal report** — per-function coverage percentages printed to stdout.
2. **`coverage.html`** — an annotated HTML file showing covered (green) and
   uncovered (red) lines for every source file.

After the report, the target checks whether total coverage meets the **90%
threshold** and prints a `✓` or `⚠️` accordingly.

```bash
make test-coverage
# ── Per-function coverage ──────────────────────────────────────────
# …/spike/events.go:BuildCloudEvent        100.0%
# …/spike/dispatcher.go:Dispatch           100.0%
# …/spike/routing.go:RunContentBasedRouting 100.0%
# …
# total: (statements)   90.2%
#
# ── Generating HTML report → coverage.html ────────────────────────
# Open coverage.html in a browser to browse line-by-line coverage.
# ✓  Coverage 90.2% meets threshold 90%
```

Open `coverage.html` in any browser to navigate the annotated source tree.

---

### Code quality targets

#### `make fmt`

Runs `gofmt -w -s` on all `.go` files in the module.  The `-s` flag applies
simplification rules (e.g. removes redundant composite-literal types).  Safe
to run at any time; it only modifies files that are already out of style.

```bash
make fmt
```

#### `make vet`

Runs `go vet ./...` — the Go built-in static analyser.  Catches common mistakes
like mismatched `Printf` format strings, unreachable code, and suspicious
composite literals.  Runs in under 1 second and is always worth running before
committing.

```bash
make vet
```

#### `make lint`

Runs `golangci-lint run ./...`.  If `golangci-lint` is not installed, the
target prints clear install instructions rather than failing silently.

```bash
make lint
# or install first:
brew install golangci-lint
```

#### `make tidy`

Runs `go mod tidy` to keep `go.mod` and `go.sum` in sync with the actual
import graph.  Run this whenever you add, remove, or upgrade a dependency, or
before opening a PR.

```bash
make tidy
```

---

### Provisioning targets

#### `make add-tenant TENANT=acme`

Wraps the `scripts/add-tenant.sh` helper.  The script appends a new account
block (user/password derived from the tenant name) to `config/nats.conf`,
copies the file into every running NATS container via `docker cp`, sends
`SIGHUP` to each container to trigger a live config reload, then verifies the
new account is reachable — **all without restarting any NATS node**.

```bash
make add-tenant TENANT=acme
# Appending account ACME to config/nats.conf…
# Copying config to nats-1, nats-2, nats-3…
# Sending SIGHUP to reload config…
# Verifying ACME connectivity… OK
```

#### `make nats-info`

Fetches `/varz` from the first NATS node and pretty-prints the JSON using
`python3 -m json.tool`.  Shows server version, uptime, and connection counts.
Requires the cluster to be running.

```bash
make nats-info
```

#### `make nats-streams`

Runs `nats stream ls` against `NATS_URL` to list all JetStream streams visible
to the default credentials.  Requires the [NATS CLI](https://github.com/nats-io/natscli).
Prints an install hint if the CLI is missing.

```bash
make nats-streams
```

---

### Clean targets

#### `make clean`

Removes the compiled binary (`spike-bin`), the coverage profile (`coverage.out`),
and the HTML report (`coverage.html`).  Does **not** touch the cluster.

```bash
make clean
```

#### `make clean-all`

Runs `make clean` and then `make cluster-down`.  Use this to return the
repository to a pristine state after a spike session.

```bash
make clean-all
```

---

## Internal Architecture

This section explains what every component does, how they depend on each other,
and exactly how a call propagates from the entry point all the way to the NATS
cluster.

### Package layout and component roles

```
internal/spike/
│
│  ── Pure functions (no NATS, no I/O) ──────────────────────────────
├── types.go          Shared data types: CloudEvent, LatencyStats,
│                     SpikeResult, RouteHandler, ProvisionedAccount
│
├── events.go         CloudEvent construction helpers:
│                     BuildCloudEvent, BuildBenchmarkPayload, PadTo1KB
│
├── stats.go          Latency math: ComputePercentiles (nearest-rank p50/95/99)
│
├── dispatcher.go     Content-based routing logic: Dispatch, NewDefaultRoutes,
│                     HandleBucketCreated, HandleBucketDeleted
│
├── results.go        Terminal output: PrintResults, PassLabel
│
│  ── NATS-connected components ─────────────────────────────────────
├── connect.go        Connection helpers with context-aware retry:
│                     ConnectWithRetryCtx, ConnectWithRetryN, ConnectWithRetry
│
├── isolation.go      Q2 — cross-account isolation test: RunIsolationTest
│
├── routing.go        Q4 — JetStream content-based routing:
│                     RunContentBasedRouting, RunContentBasedRoutingWithTimeout,
│                     ConsumeAndDispatch
│
├── benchmark.go      Q3 — JetStream sync-publish latency:
│                     RunLatencyBenchmark
│
├── provisioning.go   Q1 + Q5 — dynamic account provisioning:
│                     BuildAccountList, RunProvisioningTest,
│                     GenerateUpdatedConfig, DemonstrateConfigReload
│
│  ── Orchestration ─────────────────────────────────────────────────
└── run.go            Top-level wiring: Run, RunWithTimeout, ConfigPath
```

### Component dependency graph

```
cmd/main.go
└── run.go
    ├── connect.go ──────────────────── nats.go (external)
    │
    ├── isolation.go ─────────────────── nats.go (external)
    │
    ├── routing.go
    │   ├── events.go ──────── (pure)
    │   ├── dispatcher.go ──── (pure)
    │   │   └── types.go ───── (pure)
    │   └── nats.go/jetstream  (external)
    │
    ├── benchmark.go
    │   ├── events.go ──────── (pure)
    │   ├── stats.go ────────── (pure)
    │   └── nats.go/jetstream  (external)
    │
    ├── provisioning.go
    │   ├── connect.go
    │   └── nats.go (external), os/exec (Docker)
    │
    └── results.go ──────────── (pure)
        └── types.go
```

Arrow direction means "imports / calls".  The pure components at the bottom of
the tree are fully unit-testable without any running infrastructure.

### Entry point: `cmd/main.go`

`main.go` is an intentionally thin wrapper — it owns exactly two
responsibilities:

1. **Read configuration** — resolve `NATS_URL` from the environment (or use
   the default `nats://localhost:4222`), and locate `config/nats.conf` via
   `spike.ConfigPath()` (which uses `runtime.Caller` to find the source tree
   regardless of build flags).

2. **Delegate** — hand the resolved URL, config path, a 10-minute deadline, and
   a `slog.Logger` to `spike.RunWithTimeout`.

```
┌───────────────────────────────────────────────────────┐
│  cmd/main.go                                          │
│                                                       │
│  1. logger  := slog.New(TextHandler, os.Stdout)       │
│  2. url     := GetEnvOrDefault("NATS_URL", default)   │
│  3. cfgPath := ConfigPath()  ← runtime.Caller         │
│  4. RunWithTimeout(10m, url, cfgPath, logger)  ──────────────►
└───────────────────────────────────────────────────────┘
```

Everything below this point lives in `internal/spike/`.

---

### Call flow: full execution trace

The diagram below shows the complete execution path from `main()` to the NATS
cluster.  Each indented level is a function call; `→ NATS` marks calls that
cross the network boundary to the server.

```
main()
└── RunWithTimeout(10m, url, cfgPath, logger)          [run.go]
    │   creates context.WithTimeout(Background(), 10m)
    └── Run(ctx, url, cfgPath, logger)                  [run.go]
        │
        ├── ConnectWithRetryCtx(ctx, url, "user-a", "password-a", 10, 2s)
        │       → NATS: nats.Connect  (up to 10 attempts × 2s backoff)
        │       ← *nats.Conn  (ncA — authenticated as TENANT_A)
        │
        ├── ConnectWithRetryCtx(ctx, url, "user-b", "password-b", 10, 2s)
        │       → NATS: nats.Connect
        │       ← *nats.Conn  (ncB — authenticated as TENANT_B)
        │
        ├── RunIsolationTest(ctx, ncA, ncB, "events.secret", logger)  ── Q2
        │       See "Q2 isolation flow" below.
        │       ← (pass bool, detail string)
        │
        ├── RunContentBasedRouting(ctx, ncA, nil, logger)  ─────────── Q4
        │       See "Q4 routing flow" below.
        │       ← (pass bool, detail string)
        │
        ├── RunLatencyBenchmark(ctx, ncA, 0, logger)  ──────────────── Q3
        │       See "Q3 benchmark flow" below.
        │       ← (LatencyStats, detail string)
        │
        ├── RunProvisioningTest(ctx, url, "user-c", "pass-c", cfgPath, logger)
        │       See "Q1/Q5 provisioning flow" below.                ── Q1 + Q5
        │       ← (q1Pass, q1Detail, q5Pass, q5Duration, q5Detail)
        │
        └── PrintResults(result, os.Stdout, logger)                 [results.go]
                writes the formatted results table
                ← bool (true = all critical questions passed)
```

---

### Q2 — Isolation flow (`isolation.go`)

Tests that a message published by TENANT_B is **never delivered** to a
subscription held by TENANT_A, even when both use the same subject string.

```
RunIsolationTest(ctx, ncA, ncB, "events.secret", logger)
│
├── ncA.Subscribe("events.secret", handler → received chan)
│       → NATS: register subscription on TENANT_A's namespace
│
├── ncA.Flush()   ← ensures subscription is registered server-side
│
├── ncB.Publish("events.secret", spy-payload)
│       → NATS: publish on TENANT_B's namespace
│           server silently drops — different account, no routing
│
├── select { timeout 300ms }
│   ├── received ← message  →  FAIL (isolation broken)
│   └── timer fired          →  OK  (no cross-account delivery)
│
└── sanity check: ncA.Publish on "events.secret.sanity"
        → NATS: ncA subscribes + publishes to itself
        ← message received within 300ms  →  confirms subscription works
```

---

### Q4 — Content-based routing flow (`routing.go`, `dispatcher.go`, `events.go`)

Demonstrates that a single JetStream subject (`events.all`) can carry events
of different types and route them to per-type Go handlers.

```
RunContentBasedRouting(ctx, ncA, routes=nil, logger)
    └── RunContentBasedRoutingWithTimeout(ctx, ncA, routes, 5s, logger)
        │
        ├── routes = NewDefaultRoutes()                          [dispatcher.go]
        │       {
        │         "com.cloudforge.bucket.created" → HandleBucketCreated
        │         "com.cloudforge.bucket.deleted" → HandleBucketDeleted
        │       }
        │
        ├── jetstream.New(ncA)
        │       → NATS: JetStream context on TENANT_A
        │
        ├── js.CreateOrUpdateStream("ROUTING_TEST", subjects=["events.>"])
        │       → NATS: create in-memory WorkQueue stream
        │
        ├── Build 2 CloudEvents                                 [events.go]
        │   ├── BuildCloudEvent("com.cloudforge.bucket.created", "storage-svc", …)
        │   └── BuildCloudEvent("com.cloudforge.bucket.deleted", "storage-svc", …)
        │
        ├── js.Publish(ctx, "events.all", json(event))  × 2
        │       → NATS: persist events in ROUTING_TEST stream
        │
        ├── stream.CreateOrUpdateConsumer(FilterSubject="events.>")
        │       → NATS: ephemeral pull consumer
        │
        ├── consumer.Messages()  ← iterator
        │
        └── ConsumeAndDispatch(ctx, msgs, routes, n=2, timeout=5s, logger)
                │   (goroutine reads n messages then signals WaitGroup)
                │
                ├── msgs.Next()  → NATS msg #1 (bucket.created)
                │   Dispatch(data, routes, logger)              [dispatcher.go]
                │   ├── json.Unmarshal → CloudEvent{Type: "…bucket.created"}
                │   ├── routes["…bucket.created"] → HandleBucketCreated
                │   └── msg.Ack()  → NATS: acknowledge delivery
                │
                └── msgs.Next()  → NATS msg #2 (bucket.deleted)
                    Dispatch(data, routes, logger)
                    ├── json.Unmarshal → CloudEvent{Type: "…bucket.deleted"}
                    ├── routes["…bucket.deleted"] → HandleBucketDeleted
                    └── msg.Ack()  → NATS: acknowledge delivery
```

---

### Q3 — Latency benchmark flow (`benchmark.go`, `events.go`, `stats.go`)

Publishes 10,000 CloudEvent payloads (~1KB each) synchronously to JetStream
and measures the publish-to-ack roundtrip for every message.

```
RunLatencyBenchmark(ctx, ncA, msgCount=0, logger)
│
├── msgCount = DefaultBenchmarkMessages (10 000)
│
├── jetstream.New(ncA)
│       → NATS: JetStream context on TENANT_A
│
├── js.CreateOrUpdateStream("BENCH", subjects=["bench.events"])
│       → NATS: in-memory stream for benchmark isolation
│
├── BuildBenchmarkPayload()                                 [events.go]
│       → CloudEvent (~600B JSON data field)
├── json.Marshal(payload)
├── PadTo1KB(data)  → exactly 1024 bytes               [events.go]
│
├── loop i = 0 … 9999:
│   ├── t0 = time.Now()
│   ├── js.Publish(ctx, "bench.events", data)
│   │       → NATS: synchronous publish — blocks until server ACK
│   │       (this is a durability-latency measurement, not fire-and-forget)
│   └── latencies = append(latencies, time.Since(t0))
│
├── ComputePercentiles(latencies, elapsed)              [stats.go]
│   ├── sort.Slice(latencies)  ← ascending
│   ├── pct(50) → P50   (nearest-rank formula: idx = ceil(p/100 * n) − 1)
│   ├── pct(95) → P95
│   ├── pct(99) → P99
│   ├── latencies[0]   → Min
│   ├── latencies[n-1] → Max
│   └── float64(n) / elapsed.Seconds() → Throughput
│
└── ← LatencyStats{P50, P95, P99, Min, Max, Throughput}, detail string
```

---

### Q1 + Q5 — Provisioning flow (`provisioning.go`, `connect.go`)

Q1 proves that an account added to `nats.conf` and reloaded via SIGHUP is
immediately usable without restarting any node.  Q5 measures how long it takes
to connect sequentially to 50 pre-provisioned accounts.

```
RunProvisioningTest(ctx, url, "user-c", "pass-c", confPath, logger)
│
├── ── Q1 ──────────────────────────────────────────────────────
│   ConnectWithRetryN(url, "user-c", "pass-c", maxRetries=3, delay=0)
│       → NATS: connect as TENANT_C (pre-provisioned via SIGHUP pattern)
│       ← *nats.Conn  →  q1Pass=true  ("no restart required")
│       ← error       →  q1Pass=false ("Q1 inconclusive")
│
├── ── Q5 ──────────────────────────────────────────────────────
│   BuildAccountList(50)                         [provisioning.go — pure]
│       → []ProvisionedAccount{
│           {TENANT_01, "tenant-01", "pass-01"},
│           …
│           {TENANT_50, "tenant-50", "pass-50"},
│         }
│
│   start = time.Now()
│   loop account ∈ accounts:
│       nats.Connect(url, UserInfo(acc.User, acc.Password))
│           → NATS: open TCP + TLS handshake + auth
│       nc.Close()
│   q5Duration = time.Since(start)
│   q5Pass     = failures==0 && q5Duration < 2m
│
└── ── Config-reload demo (best-effort) ─────────────────────────
    DemonstrateConfigReload(ctx, url, confPath, logger)
    │
    ├── os.ReadFile(confPath)  ← read current nats.conf
    │
    ├── GenerateUpdatedConfig(current, "TENANT_DYNAMIC", …)
    │       [pure: string.Replace inserts account block before marker]
    │
    ├── os.WriteFile(tmpPath, updated)  ← write to /tmp
    │
    ├── exec: docker cp tmpPath nats-1:/config/nats.conf
    │           copies updated config into the running container
    │
    ├── exec: docker kill --signal=HUP nats-1
    │           triggers live NATS config reload — no restart
    │
    ├── time.Sleep(200ms)  ← wait for reload propagation
    │
    └── nats.Connect(url, UserInfo("user-dynamic", "pass-dynamic"))
            → NATS: verify TENANT_DYNAMIC is immediately reachable
```

---

### How the `connect.go` retry chain works

All NATS connections go through a single shared implementation that handles the
JetStream leader election window and respects context cancellation:

```
ConnectWithRetry(url, user, pass, logger)           high-level, fixed 10×2s
    └── ConnectWithRetryN(url, user, pass, 10, 2s)
            └── connectWithRetryCtx(ctx=Background(), …)

ConnectWithRetryCtx(ctx, url, user, pass, n, delay) context-aware variant
    └── connectWithRetryCtx(ctx, …)                 shared implementation

connectWithRetryCtx
    loop attempt = 0 … maxRetries-1:
    ├── nats.Connect(url, UserInfo, Timeout=5s, MaxReconnects=5)
    │   ├── OK  → return *nats.Conn
    │   └── err → log warn
    └── select {
          <-ctx.Done()    → return context error immediately
          <-time.After(d) → continue to next attempt
        }
    all attempts exhausted → return error
```

---

## Architecture: NATS Cluster

```
┌─────────────────────────────────────────────────────────────┐
│                   3-Node NATS JetStream Cluster             │
│                                                             │
│  ┌─────────┐   cluster routes   ┌─────────┐   ┌─────────┐  │
│  │  nats-1 │ ◄────────────────► │  nats-2 │ ─ │  nats-3 │  │
│  │ :4222   │                    │ :4223   │   │ :4224   │  │
│  └────┬────┘                    └─────────┘   └─────────┘  │
│       │                                                     │
│  Accounts (isolated subject namespaces):                    │
│    TENANT_A  — user-a / password-a                         │
│    TENANT_B  — user-b / password-b  (isolated from A)      │
│    TENANT_C  — user-c / password-c  (Q1 SIGHUP demo)       │
│    TENANT_01 … TENANT_50            (Q5 bulk provisioning) │
└─────────────────────────────────────────────────────────────┘
```

### NATS Accounts model

Each account defines a completely separate subject namespace:

- `TENANT_A` subscribed to `events.secret` will **never** receive a message
  published to `events.secret` by `TENANT_B` — even with identical subject
  strings.
- JetStream streams are scoped to the account: `TENANT_B` cannot enumerate or
  consume streams created by `TENANT_A`.
- Cross-account communication is only possible via explicit import/export
  configuration — there is no accidental leakage.

### Content-based routing approach (Q4)

All events are published to a single broad subject (`events.all`).
The consuming service decodes the CloudEvents envelope and dispatches to
per-type handlers:

```
NATS subject: events.all
 └── CloudEvent { "type": "com.cloudforge.bucket.created", ... }
       └── dispatcher → handleBucketCreated()
 └── CloudEvent { "type": "com.cloudforge.bucket.deleted", ... }
       └── dispatcher → handleBucketDeleted()
```

**Alternative: subject-per-type**

Events can also be published to type-specific subjects:

```
events.bucket.created → handleBucketCreated consumer
events.bucket.deleted → handleBucketDeleted consumer
```

This moves routing into NATS (cheap) but increases subject cardinality and
requires consumers to know all types at subscription time.
**The recommended approach for CF-EventRouter is the dispatcher pattern**
(single broad subject + Go-level dispatch) because new event types can be
added without changing the NATS stream topology.

---

## Code Structure

```
spikes/nats-routing/
├── cmd/
│   └── main.go                ← thin entry point; calls spike.RunWithTimeout()
├── config/
│   ├── nats.conf              ← NATS accounts + JetStream config
│   └── nats-cluster.yaml      ← Docker Compose: 3-node cluster
├── internal/spike/
│   ├── types.go               ← CloudEvent, LatencyStats, SpikeResult, RouteHandler
│   ├── events.go              ← BuildCloudEvent, BuildBenchmarkPayload, PadTo1KB
│   ├── stats.go               ← ComputePercentiles (pure, no NATS)
│   ├── dispatcher.go          ← Dispatch, NewDefaultRoutes, HandleBucket*
│   ├── routing.go             ← RunContentBasedRouting + ConsumeAndDispatch
│   ├── isolation.go           ← RunIsolationTest
│   ├── benchmark.go           ← RunLatencyBenchmark
│   ├── provisioning.go        ← BuildAccountList, RunProvisioningTest,
│   │                               GenerateUpdatedConfig, DemonstrateConfigReload
│   ├── connect.go             ← ConnectWithRetry* (context-aware retry)
│   ├── results.go             ← PrintResults, PassLabel
│   └── run.go                 ← Run(), ConfigPath(), RunWithTimeout()
├── scripts/
│   └── add-tenant.sh          ← dynamic provisioning helper (wraps docker cp + SIGHUP)
├── Makefile                   ← all dev commands (see `make help`)
├── go.mod / go.sum
├── README.md                  ← this file
└── FINDINGS.md                ← fill in after running the program
```

### Test coverage

The `internal/spike` package ships with **90%+ statement coverage**.
Tests use an in-process embedded NATS server (`nats-server/v2`) — no Docker
required.  Run `make test-coverage` to regenerate the report.

---

## Manual Quick Start

If you prefer not to use Make:

### 1 — Start the 3-node cluster

```bash
# From the spike root:
docker compose -f config/nats-cluster.yaml up -d

# Verify all three nodes are healthy:
docker compose -f config/nats-cluster.yaml ps
```

Wait ~5 seconds for the JetStream leader election to complete.
You can confirm readiness by checking the monitoring endpoint:

```bash
curl -s http://localhost:8222/jsz | python3 -m json.tool
```

The response should include `"config": { ... }` with `"enabled": true`.

### 2 — Run the spike program

```bash
go run ./cmd
```

The program exercises all five spike questions in sequence and prints a
structured results table to stdout.

Override the NATS URL if needed:

```bash
NATS_URL=nats://localhost:4223 go run ./cmd   # connect to node 2
```

### 3 — Tear down

```bash
docker compose -f config/nats-cluster.yaml down       # keep volumes
docker compose -f config/nats-cluster.yaml down -v    # wipe volumes too
```

---

## Dynamic Provisioning Demo

To manually add a new tenant account at runtime (Q1 / Q5 scenario):

```bash
# Usage: add-tenant.sh <ACCOUNT_ID> <USERNAME> <PASSWORD>
./scripts/add-tenant.sh NEW_TENANT new-user new-secret
```

The script:
1. Appends the account block to `config/nats.conf`.
2. Copies the updated config into all running NATS containers.
3. Sends `SIGHUP` to each node to trigger a live reload (no restart).
4. Verifies connectivity with the new credentials.

---

## Monitoring

While the program runs, the NATS monitoring API is available at:

| Endpoint | Description |
|---|---|
| `http://localhost:8222/varz` | Server vars (version, uptime, connections) |
| `http://localhost:8222/jsz` | JetStream stats (streams, consumers, bytes) |
| `http://localhost:8222/accountz` | All account details |
| `http://localhost:8222/connz` | Active client connections |
| `http://localhost:8222/routez` | Cluster route health |

---

## Troubleshooting

**"dial tcp: connection refused"** — the cluster is not running or not yet ready.
Run `make cluster-up` and wait for the status check to show all three nodes as `OK`.

**"nats: Authorization Violation"** — the account or credentials in `nats.conf`
do not match what the program uses. Check that `config/nats.conf` is mounted
correctly in the container: `docker exec nats-1 cat /config/nats.conf`.

**JetStream stream creation fails** — the cluster may not have a JetStream
leader yet. Check `http://localhost:8222/jsz` — `meta_leader` must be non-empty.

**Config reload not working** — verify that the container has write access to
`/config/nats.conf`. The bind mount in `nats-cluster.yaml` uses `:ro`; the
`add-tenant.sh` script uses `docker cp` which bypasses the `:ro` flag.
