package spike

import (
	"encoding/json"
	"time"
)

// ---------------------------------------------------------------------------
// CloudEvent envelope
// ---------------------------------------------------------------------------

// CloudEvent mirrors the CloudEvents 1.0 JSON envelope fields that the
// content-based routing logic inspects.
//
// We use a lightweight local struct rather than the full sdk-go/v2 Event so
// the binary stays small and we have complete control over JSON field names.
// Phase 5 consumers may switch to the full SDK if they need spec compliance
// (e.g. HTTP protocol binding, AVRO encoding).
type CloudEvent struct {
	// SpecVersion is always "1.0".
	SpecVersion string `json:"specversion"`
	// Type is the dot-separated event type, e.g. "com.cloudforge.bucket.created".
	// The routing dispatcher branches on this field.
	Type string `json:"type"`
	// Source is the originating service URI.
	Source string `json:"source"`
	// ID is a unique event identifier.
	ID string `json:"id"`
	// DataContentType is typically "application/json".
	DataContentType string `json:"datacontenttype"`
	// Data is the arbitrary JSON payload carried by the event.
	Data json.RawMessage `json:"data"`
}

// ---------------------------------------------------------------------------
// Latency statistics
// ---------------------------------------------------------------------------

// LatencyStats holds the aggregated results of the JetStream publish latency
// benchmark.  All durations are publish-to-ack roundtrip times.
type LatencyStats struct {
	// P50 / P95 / P99 are percentile roundtrip durations.
	P50 time.Duration
	P95 time.Duration
	P99 time.Duration
	// Min and Max are the smallest and largest individual latency observed.
	Min time.Duration
	Max time.Duration
	// Throughput is messages per second over the full benchmark run.
	Throughput float64
}

// ---------------------------------------------------------------------------
// Spike results
// ---------------------------------------------------------------------------

// SpikeResult collects the Boolean pass/fail and narrative answers for every
// spike question so they can be printed together at the end.
type SpikeResult struct {
	// Q1: dynamic provisioning without cluster restart.
	Q1Pass   bool
	Q1Detail string

	// Q2: cross-account isolation.
	Q2Pass   bool
	Q2Detail string

	// Q3: latency.
	Q3Stats  LatencyStats
	Q3Detail string

	// Q4: content-based routing approach.
	Q4Pass   bool
	Q4Detail string

	// Q5: 50 accounts provisioned in under 2 minutes.
	Q5Pass     bool
	Q5Duration time.Duration
	Q5Detail   string
}

// ---------------------------------------------------------------------------
// Routing
// ---------------------------------------------------------------------------

// RouteHandler is a function that processes a specific CloudEvent type.
// Handlers receive the decoded event and a logger; they should not return
// errors because dispatch already committed to delivery by calling them.
type RouteHandler func(event CloudEvent, logger interface{ Info(string, ...any) })

// ---------------------------------------------------------------------------
// Provisioning
// ---------------------------------------------------------------------------

// ProvisionedAccount describes credentials for one NATS account.
type ProvisionedAccount struct {
	// AccountName is the NATS account identifier (e.g. "TENANT_C").
	AccountName string
	// User / Password are the NATS username and password for this account.
	User     string
	Password string
}
