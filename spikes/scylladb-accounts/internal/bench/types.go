package bench

import "time"

// ─── Configuration ────────────────────────────────────────────────────────────

// Config holds all tunable parameters for the benchmark suite.
// Zero-value fields are replaced with defaults by [DefaultConfig].
type Config struct {
	// ScyllaDB connection parameters.
	Hosts    []string // CQL contact points (default: ["127.0.0.1"])
	Port     int      // CQL port (default: 9042)
	Keyspace string   // Target keyspace (default: "cf")
	Username string   // CQL username (default: "cassandra")
	Password string   // CQL password (default: "cassandra")

	// Benchmark workload size.
	SeedRows    int // Number of rows to insert before benchmarking (default: 1000)
	Ops         int // Number of read operations per benchmark (default: 2000)
	Concurrency int // Number of concurrent goroutines (default: 50)

	// LWT-specific settings.
	LWTWriters int // Concurrent writers trying to claim the same job (default: 20)

	// Request and connection timeouts.
	ConnectTimeout time.Duration // ScyllaDB dial timeout (default: 10s)
	QueryTimeout   time.Duration // Per-query timeout (default: 5s)
}

// DefaultConfig returns a Config suitable for local k3d development.
func DefaultConfig() Config {
	return Config{
		Hosts:          []string{"127.0.0.1"},
		Port:           9042,
		Keyspace:       "cf",
		Username:       "cassandra",
		Password:       "cassandra",
		SeedRows:       1000,
		Ops:            2000,
		Concurrency:    50,
		LWTWriters:     20,
		ConnectTimeout: 10 * time.Second,
		QueryTimeout:   5 * time.Second,
	}
}

// ─── Result types ─────────────────────────────────────────────────────────────

// BenchName identifies a benchmark run in the results table.
type BenchName string

const (
	BenchAPIKeyQuorum  BenchName = "api_key_lookup (QUORUM)"
	BenchAPIKeyOne     BenchName = "api_key_lookup (ONE)"
	BenchLWTClaim      BenchName = "lwt_job_claim"
	BenchLWTTransition BenchName = "lwt_state_transition"
	BenchMVSlug        BenchName = "mv_tenant_by_slug (QUORUM)"
	BenchMVEmail       BenchName = "mv_user_by_email (QUORUM)"
)

// Result holds the latency distribution and error count for one benchmark.
type Result struct {
	Name BenchName

	// Number of completed operations (excluding errors).
	Ops int

	// Latency percentiles measured from first byte of request to first byte
	// of response.
	P50 time.Duration
	P95 time.Duration
	P99 time.Duration
	Min time.Duration
	Max time.Duration

	// Total wall-clock duration for all operations (used to compute throughput).
	TotalDuration time.Duration

	// Number of operations that returned an error (not including LWT
	// applied=false outcomes, which are tracked separately in LWTResult).
	Errors int
}

// Throughput returns operations per second based on TotalDuration.
func (r Result) Throughput() float64 {
	if r.TotalDuration == 0 {
		return 0
	}
	return float64(r.Ops) / r.TotalDuration.Seconds()
}

// LWTResult extends Result with LWT-specific correctness counters.
type LWTResult struct {
	Result

	// Winners is the number of goroutines that received applied=true.
	// For idempotent creation (IF NOT EXISTS) this should always be exactly 1.
	// For state transition (IF status = 'QUEUED') this should also be exactly 1.
	Winners int

	// Losers is the number of goroutines that received applied=false.
	// Winners + Losers + Errors should equal the total attempt count.
	Losers int
}

// Correct returns true when exactly one goroutine won the LWT (correctness
// guarantee: no duplicate provisioning jobs, no double state-transitions).
func (l LWTResult) Correct() bool {
	return l.Winners == 1
}

// ─── Sample collection ────────────────────────────────────────────────────────

// Samples is a sortable slice of durations collected during a benchmark.
// It is used to compute percentile values.
type Samples []time.Duration

func (s Samples) Len() int           { return len(s) }
func (s Samples) Less(i, j int) bool { return s[i] < s[j] }
func (s Samples) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
