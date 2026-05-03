package measure

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// buildPassingResult returns a BenchmarkResult where every variant passes its
// p95 threshold.  Used as the happy-path fixture in table tests.
func buildPassingResult() BenchmarkResult {
	return BenchmarkResult{
		KnativeVersion: "v1.15.0",
		Platform:       "darwin/arm64 go1.26",
		StartedAt:      time.Date(2026, 4, 27, 3, 0, 0, 0, time.UTC),
		Results: map[Variant]Stats{
			VariantMinimal: {
				Variant: VariantMinimal, ImageSize: "8 MB",
				P50: 1200 * time.Millisecond, P75: 1400 * time.Millisecond,
				P95: 2000 * time.Millisecond, P99: 2700 * time.Millisecond,
				Min: 900 * time.Millisecond, Max: 2700 * time.Millisecond,
				SampleCount: 10,
			},
			VariantMedium: {
				Variant: VariantMedium, ImageSize: "98 MB",
				P50: 2800 * time.Millisecond, P75: 3200 * time.Millisecond,
				P95: 4100 * time.Millisecond, P99: 4600 * time.Millisecond,
				Min: 2100 * time.Millisecond, Max: 4600 * time.Millisecond,
				SampleCount: 10,
			},
			VariantHeavy: {
				Variant: VariantHeavy, ImageSize: "512 MB",
				P50: 5300 * time.Millisecond, P75: 6100 * time.Millisecond,
				P95: 7400 * time.Millisecond, P99: 8200 * time.Millisecond,
				Min: 4800 * time.Millisecond, Max: 8200 * time.Millisecond,
				SampleCount: 10,
			},
		},
	}
}

// ─── PrintTable ───────────────────────────────────────────────────────────────

// TestPrintTable_ContainsAllVariants verifies that the rendered table output
// includes a row for each variant.
func TestPrintTable_ContainsAllVariants(t *testing.T) {
	var buf strings.Builder
	PrintTable(&buf, buildPassingResult())
	out := buf.String()

	for _, v := range AllVariants {
		assert.Contains(t, out, string(v),
			"output must contain variant name %q", v)
	}
}

// TestPrintTable_ContainsHeaderFields verifies that metadata from the result
// struct is present in the rendered header section.
func TestPrintTable_ContainsHeaderFields(t *testing.T) {
	var buf strings.Builder
	PrintTable(&buf, buildPassingResult())
	out := buf.String()

	assert.Contains(t, out, "v1.15.0", "Knative version must appear in header")
	assert.Contains(t, out, "net-kourier", "network backend must appear in header")
	assert.Contains(t, out, "2026", "timestamp year must appear in header")
}

// TestPrintTable_ThresholdSymbols verifies that the threshold analysis section
// emits the correct pass/warn/fail symbols for the fixture data.
//
// Fixture:
//   - minimal p95=2.00s  threshold=3s  → 67% of threshold  → ✓
//   - medium  p95=4.10s  threshold=5s  → 82% of threshold  → ⚠  (> 80%)
//   - heavy   p95=7.40s  threshold=10s → 74% of threshold  → ✓
func TestPrintTable_ThresholdSymbols(t *testing.T) {
	var buf strings.Builder
	PrintTable(&buf, buildPassingResult())
	out := buf.String()

	assert.Contains(t, out, "✓", "must contain at least one passing symbol")
	// Medium is at 82% of threshold (4.10 / 5.00) — should trigger the ⚠ warning.
	assert.Contains(t, out, "⚠", "medium variant at 82% of threshold must emit a warning symbol")
}

// TestPrintTable_FailingVariantEmitsFailSymbol verifies that a variant whose
// p95 exceeds its threshold produces the ✗ failure symbol.
func TestPrintTable_FailingVariantEmitsFailSymbol(t *testing.T) {
	result := buildPassingResult()
	// Override heavy to breach its 10s threshold.
	heavy := result.Results[VariantHeavy]
	heavy.P95 = 12 * time.Second
	result.Results[VariantHeavy] = heavy

	var buf strings.Builder
	PrintTable(&buf, result)
	out := buf.String()

	assert.Contains(t, out, "✗", "a variant exceeding its threshold must emit ✗")
	assert.Contains(t, out, "EXCEEDS", "failure message must contain EXCEEDS")
}

// TestPrintTable_MinReplicaRecommendation verifies that the recommendation
// section emits the min-replicas=1 override for failing variants.
func TestPrintTable_MinReplicaRecommendation(t *testing.T) {
	result := buildPassingResult()
	heavy := result.Results[VariantHeavy]
	heavy.P95 = 12 * time.Second
	result.Results[VariantHeavy] = heavy

	var buf strings.Builder
	PrintTable(&buf, result)
	out := buf.String()

	assert.Contains(t, out, "minScale=1", "failing variant must appear in recommendation")
	assert.Contains(t, out, "heavy", "recommendation must name the failing variant")
}

// TestPrintTable_AllPassedMessage verifies the "All variants pass" message
// appears in the recommendation section when every variant meets its threshold.
func TestPrintTable_AllPassedMessage(t *testing.T) {
	// Build a result where all three variants have low p95 values.
	result := BenchmarkResult{
		KnativeVersion: "v1.15.0",
		Platform:       "linux/amd64",
		StartedAt:      time.Now(),
		Results: map[Variant]Stats{
			VariantMinimal: {Variant: VariantMinimal, P95: 1 * time.Second, SampleCount: 5},
			VariantMedium:  {Variant: VariantMedium, P95: 2 * time.Second, SampleCount: 5},
			VariantHeavy:   {Variant: VariantHeavy, P95: 3 * time.Second, SampleCount: 5},
		},
	}

	var buf strings.Builder
	PrintTable(&buf, result)
	out := buf.String()

	assert.Contains(t, out, "All variants pass", "success message must appear when all pass")
}

// TestPrintTable_MissingVariant verifies that variants absent from the result
// map produce a dash-filled row rather than panicking.
func TestPrintTable_MissingVariant(t *testing.T) {
	result := BenchmarkResult{
		StartedAt: time.Now(),
		Results: map[Variant]Stats{
			// Only minimal — medium and heavy are missing.
			VariantMinimal: {Variant: VariantMinimal, P95: 1 * time.Second, SampleCount: 3},
		},
	}

	require.NotPanics(t, func() {
		var buf strings.Builder
		PrintTable(&buf, result)
	}, "PrintTable must not panic when variants are absent")
}

// TestPrintTable_EmptyKnativeVersion verifies graceful handling when the
// Knative version is not populated (pre-cluster-check scenario).
func TestPrintTable_EmptyKnativeVersion(t *testing.T) {
	result := buildPassingResult()
	result.KnativeVersion = "" // simulate missing cluster metadata

	require.NotPanics(t, func() {
		var buf strings.Builder
		PrintTable(&buf, result)
	})

	var buf strings.Builder
	PrintTable(&buf, result)
	assert.Contains(t, buf.String(), "unknown",
		"empty KnativeVersion must be displayed as 'unknown'")
}
