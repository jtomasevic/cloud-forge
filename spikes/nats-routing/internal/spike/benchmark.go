package spike

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// BenchmarkStreamName is the JetStream stream name used by [RunLatencyBenchmark].
const BenchmarkStreamName = "BENCH"

// BenchmarkSubject is the subject published to during the latency test.
const BenchmarkSubject = "bench.events"

// RunLatencyBenchmark publishes msgCount CloudEvent payloads (~1KB each) to a
// JetStream stream and measures the synchronous publish-to-ack roundtrip for
// each message.
//
// JetStream's synchronous Publish() blocks until the server acknowledges
// persistence, making this an accurate durability-latency measurement.
//
// When msgCount is 0 the function uses [DefaultBenchmarkMessages].
func RunLatencyBenchmark(
	ctx context.Context,
	nc *nats.Conn,
	msgCount int,
	logger *slog.Logger,
) (stats LatencyStats, detail string) {
	if msgCount <= 0 {
		msgCount = DefaultBenchmarkMessages
	}

	js, err := jetstream.New(nc)
	if err != nil {
		return stats, fmt.Sprintf("jetstream.New: %v", err)
	}

	// A dedicated stream keeps the benchmark isolated from the routing test.
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     BenchmarkStreamName,
		Subjects: []string{BenchmarkSubject},
		Storage:  jetstream.MemoryStorage,
		Replicas: 1,
	})
	if err != nil {
		return stats, fmt.Sprintf("CreateOrUpdateStream: %v", err)
	}
	defer js.DeleteStream(ctx, BenchmarkStreamName) //nolint:errcheck

	// Build a representative ~1KB payload once, then reuse it for every publish.
	payload := BuildBenchmarkPayload()
	// json.Marshal on CloudEvent (strings + json.RawMessage) cannot fail.
	data, _ := json.Marshal(payload)
	data = PadTo1KB(data)

	logger.Info("starting latency benchmark",
		"messages", msgCount,
		"payload_bytes", len(data),
	)

	latencies := make([]time.Duration, 0, msgCount)
	start := time.Now()

	for i := range msgCount {
		t0 := time.Now()
		if _, err := js.Publish(ctx, BenchmarkSubject, data); err != nil {
			logger.Warn("publish failed, skipping", "i", i, "error", err)
			continue
		}
		latencies = append(latencies, time.Since(t0))
	}

	elapsed := time.Since(start)

	if len(latencies) == 0 {
		return stats, "all publishes failed — no latency data collected"
	}

	stats = ComputePercentiles(latencies, elapsed)
	detail = fmt.Sprintf(
		"published %d messages in %s — p50=%s p95=%s p99=%s min=%s max=%s throughput=%.0f msg/s",
		len(latencies), elapsed.Round(time.Millisecond),
		stats.P50, stats.P95, stats.P99,
		stats.Min, stats.Max,
		stats.Throughput,
	)
	logger.Info("benchmark complete",
		"p50", stats.P50,
		"p95", stats.P95,
		"p99", stats.P99,
		"throughput_msg_s", fmt.Sprintf("%.0f", stats.Throughput),
	)
	return stats, detail
}
