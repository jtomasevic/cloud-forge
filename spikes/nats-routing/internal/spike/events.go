package spike

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// DefaultBenchmarkMessages is the number of publish-ack roundtrips used by
// the latency benchmark when no override is provided.
const DefaultBenchmarkMessages = 10_000

// BuildCloudEvent constructs a minimal CloudEvents 1.0 envelope.
//
// Parameters:
//   - eventType   — follows the "com.cloudforge.<resource>.<verb>" convention
//   - source      — originating service name (e.g. "storage-svc")
//   - rawDataJSON — pre-marshalled JSON payload for the data field
//
// The ID is generated from the current Unix nanosecond timestamp, giving
// sufficient uniqueness for a spike.  Production code should use UUID v4.
func BuildCloudEvent(eventType, source, rawDataJSON string) CloudEvent {
	return CloudEvent{
		SpecVersion: "1.0",
		Type:        eventType,
		Source:      "//" + source,
		// UnixNano gives cheap, monotonically increasing IDs for a spike.
		ID:              fmt.Sprintf("%d", time.Now().UnixNano()),
		DataContentType: "application/json",
		Data:            json.RawMessage(rawDataJSON),
	}
}

// BuildBenchmarkPayload returns a CloudEvent-shaped payload that, when
// marshalled to JSON, is padded to approximately 1 KB.
//
// The size matches the spike spec requirement of "1KB CloudEvent payloads"
// for the latency benchmark (Q3).
func BuildBenchmarkPayload() CloudEvent {
	// A realistic storage-event payload.
	data := map[string]any{
		"name":       "benchmark-bucket",
		"tenant":     "bench-tenant",
		"project":    "bench-project",
		"region":     "eu-west-1",
		"created_at": time.Now().UTC().Format(time.RFC3339),
		// padding fills the remaining bytes so the marshalled payload is ~1KB.
		"_padding": strings.Repeat("x", 600),
	}
	raw, _ := json.Marshal(data)
	return CloudEvent{
		SpecVersion:     "1.0",
		Type:            "com.cloudforge.bucket.created",
		Source:          "//storage-svc",
		ID:              "benchmark",
		DataContentType: "application/json",
		Data:            json.RawMessage(raw),
	}
}

// PadTo1KB takes an already-marshalled JSON payload and pads it with
// trailing spaces until it reaches exactly 1024 bytes, then trims to 1024.
//
// The padding strategy is intentionally simple (trailing spaces in JSON are
// ignored by decoders) and is only suitable for benchmark purposes.
func PadTo1KB(data []byte) []byte {
	// Grow by appending spaces until we hit the target.
	for len(data) < 1024 {
		data = append(data, ' ')
	}
	// Truncate any surplus so the size is exactly 1024.
	return data[:1024]
}
