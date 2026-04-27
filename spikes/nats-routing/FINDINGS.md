# FINDINGS — Spike 0.6: NATS JetStream Multi-Tenant Routing

> **Status:** TODO — run `go run ./cmd` against the 3-node cluster and fill
> in the measured values below.  Replace every `<FILL_IN>` with actual output
> from the spike program or from direct observation.

---

## Q1 — Dynamic account provisioning without cluster restart

**Question:** Can NATS accounts be provisioned dynamically at runtime without
restarting the NATS cluster?

**Answer:** `<PASS | FAIL>`

**Method used:** NATS config reload via SIGHUP (`docker kill --signal=HUP nats-1`).

**Observed behaviour:**
```
<Paste relevant section of spike program output here>
```

**Reload latency (approximate):** `<Xms>`

**Decision:** Config-reload is **sufficient for the spike**.
For production, the recommended approach is the **JWT account resolver**:

- An operator key signs tenant JWTs out-of-band (via CF-ResourceController).
- The NATS cluster is configured with a `resolver:` block pointing to a resolver
  service or directory.
- New tenant JWTs are pushed to the resolver; NATS picks them up automatically
  without any config file change or reload signal.
- This approach requires no filesystem access to the NATS pods and integrates
  cleanly with Kubernetes Secrets.

---

## Q2 — Cross-account isolation

**Question:** Is stream/subject isolation between NATS accounts complete?

**Answer:** `<PASS | FAIL>`

**Observed behaviour:**
```
<Paste isolation test output here>
```

**Conclusion:** `<Write 1–2 sentences on what you observed>`

---

## Q3 — Publish latency (1KB CloudEvent, JetStream sync)

**Question:** What is the per-message latency for 10,000 CloudEvent publishes?

**Environment:** `<Hardware description, e.g. MacBook Pro M3, 16GB RAM, Docker Desktop 4.x>`

| Metric | Measured | Threshold | Pass? |
|---|---|---|---|
| p50 | `<Xµs>` | — | — |
| p95 | `<Xµs>` | — | — |
| p99 | `<Xms>` | < 5ms | `<PASS\|FAIL>` |
| Min | `<Xµs>` | — | — |
| Max | `<Xms>` | — | — |
| Throughput | `<X msg/s>` | — | — |

**Notes:**
```
<Any outliers, GC pauses, or cold-start effects observed>
```

---

## Q4 — Content-based routing approach

**Question:** How is content-based routing implemented — within NATS subjects,
or via a consumer-side filter?

**Answer:** `<Subject-per-type | Dispatcher pattern | Hybrid>`

**Chosen approach for CF-EventRouter:**

> Publish all events to a single broad subject (`events.all`).  The consuming
> service reads the CloudEvents `type` field and dispatches to a per-type
> handler map.  New event types are added by registering a new handler — no
> NATS stream topology change is required.

**Trade-offs:**

| Approach | Pros | Cons |
|---|---|---|
| Dispatcher (recommended) | No subject cardinality growth; new types add zero NATS config | Slightly more CPU per message (JSON decode + map lookup) |
| Subject-per-type | NATS-level fan-out; consumers are decoupled | Subject explosion for many event types; stream config must be updated |
| JetStream filter expressions | NATS 2.10+ native; no consumer code | Requires NATS 2.10+; filter DSL is limited |

---

## Q5 — 50 accounts sequential provisioning

**Question:** Can 50 tenant accounts be provisioned sequentially in under 2 minutes?

**Answer:** `<PASS | FAIL>`

**Measured duration:** `<X.Xs>`

**Method:** Pre-defined accounts TENANT_01…TENANT_50 in `config/nats.conf`.
Sequential connection with `nats.Connect()` + `nats.UserInfo()`.

**Notes:**
```
<Any connection failures or retries observed>
```

---

## Dynamic Provisioning Model Decision

**CRD-based (Kubernetes operator) vs Config API:**

| Model | Description | Chosen? |
|---|---|---|
| CRD-based | `NATSAccount` CRD → operator watches → updates nats.conf + SIGHUP | No |
| JWT resolver | CF-ResourceController signs account JWTs → pushed to NATS resolver | **Yes** |
| Config API | Direct NATS management API | No native API available |

**Recommendation:** Use the **JWT account resolver** model in production.
See Q1 above for rationale.

---

## Routing Engine Design Input

Based on the spike findings, the recommended CF-EventRouter runtime
routing engine should:

1. **Subscribe** to a per-tenant JetStream stream (subject `tenants.<id>.events.>`)
2. **Decode** the CloudEvents envelope from each NATS message payload
3. **Dispatch** via a `map[string]HandlerFunc` keyed on `event.Type`
4. **Ack** the JetStream message after the handler returns successfully
5. **Nak with delay** on handler error (JetStream re-delivery)

This is a pure Go implementation — no broker-level routing rules, no
additional infrastructure.

---

## Gaps CF-EventRouter Must Close

| Gap | NATS native? | CF-EventRouter responsibility |
|---|---|---|
| Content-based routing (CloudEvents type dispatch) | No | Implement dispatcher in Go |
| Dead-letter queue for failed events | Partial (MaxDeliver) | Write to a `dlq.<tenant>` stream |
| Event schema validation | No | Validate CloudEvents envelope before dispatch |
| Tenant rate limiting | No | Implement per-tenant publish quota in CF-ResourceController |
| Cross-tenant event forwarding | Explicit import/export only | Document as unsupported in Phase 5 |

---

## Spike Outcome

  Q1  Dynamic provisioning without restart   PASS ✓
      TENANT_C is reachable without server restart — config reload (SIGHUP) works

  Q2  Cross-account isolation complete        PASS ✓
      NATS account isolation is complete: cross-account messages are silently dropped

  Q3  Publish latency (1KB CloudEvent, JetStream sync publish)
      p50 190.792µs   p95 245.25µs    p99 410.167µs 
      min 139.542µs   max 1.822125ms  throughput 5000 msg/s
      threshold: p99 < 5ms  →  PASS ✓

  Q4  Content-based routing implemented       PASS ✓
      content-based routing works: dispatcher reads 'type' field and calls per-type handlers

  Q5  50 accounts provisioned in < 2m         PASS ✓
      Duration: 84ms
      connected to 50 accounts in 84ms (failures: 0) — threshold: 2m0s

**Spike verdict:** PROCEED with NATS JetStream for CF-EventRouter.

