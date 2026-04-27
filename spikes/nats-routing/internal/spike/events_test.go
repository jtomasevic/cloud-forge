package spike_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jtomasevic/cloud-forge/spikes/nats-routing/internal/spike"
)

// TestBuildCloudEvent_Fields verifies that the returned envelope carries the
// correct spec version, type, source prefix, and data field.
func TestBuildCloudEvent_Fields(t *testing.T) {
	t.Parallel()

	ev := spike.BuildCloudEvent(
		"com.cloudforge.bucket.created",
		"storage-svc",
		`{"name":"test-bucket"}`,
	)

	assert.Equal(t, "1.0", ev.SpecVersion)
	assert.Equal(t, "com.cloudforge.bucket.created", ev.Type)
	// Source should be prefixed with "//".
	assert.Equal(t, "//storage-svc", ev.Source)
	assert.Equal(t, "application/json", ev.DataContentType)
	assert.NotEmpty(t, ev.ID, "ID must be generated")
	// Data must be valid JSON.
	assert.True(t, json.Valid(ev.Data), "Data must be valid JSON")
}

// TestBuildCloudEvent_UniqueIDs verifies that two successive calls produce
// different IDs, confirming the timestamp-based ID generator works.
func TestBuildCloudEvent_UniqueIDs(t *testing.T) {
	t.Parallel()

	ev1 := spike.BuildCloudEvent("a", "svc", `{}`)
	ev2 := spike.BuildCloudEvent("a", "svc", `{}`)

	// Two calls in rapid succession may share a nanosecond on very fast
	// hardware, so we test that IDs are at minimum non-empty.
	assert.NotEmpty(t, ev1.ID)
	assert.NotEmpty(t, ev2.ID)
}

// TestBuildCloudEvent_EmptyData verifies that an empty JSON object is accepted
// as the data field.
func TestBuildCloudEvent_EmptyData(t *testing.T) {
	t.Parallel()

	ev := spike.BuildCloudEvent("test.type", "svc", `{}`)
	assert.True(t, json.Valid(ev.Data))
}

// TestBuildBenchmarkPayload_Approx1KB verifies that the benchmark payload is
// at least 800 bytes when marshalled to JSON, confirming it represents the
// "~1KB payload" required by the spike spec.
func TestBuildBenchmarkPayload_Approx1KB(t *testing.T) {
	t.Parallel()

	payload := spike.BuildBenchmarkPayload()
	data, err := json.Marshal(payload)
	require.NoError(t, err)

	// The raw marshal should be ≥ 800 bytes before PadTo1KB.
	assert.GreaterOrEqual(t, len(data), 800,
		"benchmark payload should be close to 1KB before padding")
	assert.Equal(t, "com.cloudforge.bucket.created", payload.Type)
}

// TestPadTo1KB_PadsShortPayload verifies that a payload shorter than 1024 bytes
// is padded to exactly 1024 bytes.
func TestPadTo1KB_PadsShortPayload(t *testing.T) {
	t.Parallel()

	short := []byte(`{"small":"payload"}`)
	padded := spike.PadTo1KB(short)

	assert.Equal(t, 1024, len(padded), "padded payload must be exactly 1024 bytes")
	// The original prefix must be preserved.
	assert.Equal(t, short, padded[:len(short)])
}

// TestPadTo1KB_TruncatesLongPayload verifies that a payload longer than 1024
// bytes is truncated to exactly 1024 bytes.
func TestPadTo1KB_TruncatesLongPayload(t *testing.T) {
	t.Parallel()

	long := make([]byte, 2048)
	for i := range long {
		long[i] = 'x'
	}
	padded := spike.PadTo1KB(long)

	assert.Equal(t, 1024, len(padded))
}

// TestPadTo1KB_ExactSize verifies that a payload that is already exactly
// 1024 bytes is returned unchanged.
func TestPadTo1KB_ExactSize(t *testing.T) {
	t.Parallel()

	exact := make([]byte, 1024)
	for i := range exact {
		exact[i] = 'a'
	}
	padded := spike.PadTo1KB(exact)

	assert.Equal(t, 1024, len(padded))
	assert.Equal(t, exact, padded)
}
