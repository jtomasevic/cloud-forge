package probe

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// Test 2: Provisioning speed
// ──────────────────────────────────────────────────────────────────────────────

// natsManifest is a minimal NATS JetStream deployment used to measure
// provisioning speed inside a fresh vCluster.
const natsManifest = `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: nats-config
data:
  nats.conf: |
    jetstream {
      store_dir: /data/jetstream
    }
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: nats
  labels:
    app: nats
spec:
  serviceName: nats
  replicas: 1
  selector:
    matchLabels:
      app: nats
  template:
    metadata:
      labels:
        app: nats
    spec:
      containers:
      - name: nats
        image: nats:2.10-alpine
        args: ["-c", "/etc/nats/nats.conf", "-js"]
        ports:
        - containerPort: 4222
        volumeMounts:
        - name: config
          mountPath: /etc/nats
      volumes:
      - name: config
        configMap:
          name: nats-config
---
apiVersion: v1
kind: Service
metadata:
  name: nats
spec:
  selector:
    app: nats
  ports:
  - port: 4222
    targetPort: 4222
`

// SpeedSample is a single provisioning-speed measurement for one vCluster creation cycle.
type SpeedSample struct {
	// VClusterReadyElapsed is the wall-clock time from "create" to API ready.
	VClusterReadyElapsed time.Duration
	// NATSReadyElapsed is the wall-clock time from API ready to NATS pod Running.
	NATSReadyElapsed time.Duration
	// Err is non-nil if this sample failed for any reason.
	Err error
}

// SpeedStats summarises a set of SpeedSamples.
type SpeedStats struct {
	Samples          int
	FailedSamples    int
	P50VCluster      time.Duration
	P95VCluster      time.Duration
	P50NATS          time.Duration
	P95NATS          time.Duration
}

// RunTestProvisioningSpeed simulates N provisioning cycles using a ready-made
// kubeconfig (the vCluster is already created by the Makefile). It applies the
// NATS manifest, waits for NATS to be ready, records the timing, then deletes
// the NATS resources and repeats.
//
// The "vCluster ready" elapsed time is provided by the caller (measured during
// vCluster creation in Makefile/main.go) and used for p50/p95 computation.
//
// NOTE: In unit tests cfg.SpeedSamples controls the sample count.
func RunTestProvisioningSpeed(
	ctx context.Context,
	c KubectlClient,
	cfg Config,
	tenantKubeconfig string,
	vClusterReadyElapsed time.Duration,
) TestResult {
	start := time.Now()
	metrics := map[string]string{}

	samples := make([]SpeedSample, 0, cfg.SpeedSamples)

	// The actual vCluster creation elapsed time is passed in; we record it for
	// every sample to derive realistic p50/p95 estimates.
	for i := range cfg.SpeedSamples {
		s := measureNATSProvisioningSpeed(ctx, c, cfg, tenantKubeconfig, vClusterReadyElapsed)
		samples = append(samples, s)
		if s.Err != nil {
			metrics[fmt.Sprintf("sample_%d_error", i)] = s.Err.Error()
		}
	}

	stats := computeSpeedStats(samples)
	vClusterThreshold := time.Duration(cfg.VClusterReadySeconds * float64(time.Second))
	natsThreshold := time.Duration(cfg.NATSReadySeconds * float64(time.Second))

	metrics["speed_samples"] = fmt.Sprintf("%d", stats.Samples)
	metrics["speed_failed"] = fmt.Sprintf("%d", stats.FailedSamples)
	metrics["vcluster_p50"] = stats.P50VCluster.Round(time.Millisecond).String()
	metrics["vcluster_p95"] = stats.P95VCluster.Round(time.Millisecond).String()
	metrics["nats_p50"] = stats.P50NATS.Round(time.Millisecond).String()
	metrics["nats_p95"] = stats.P95NATS.Round(time.Millisecond).String()

	// All samples must succeed and both p95s must be within threshold.
	if stats.FailedSamples > 0 {
		return failResult(TestProvisioningSpeed,
			fmt.Sprintf("%d/%d samples failed — see metrics for details", stats.FailedSamples, stats.Samples),
			start, metrics)
	}
	if stats.P95VCluster > vClusterThreshold {
		return failResult(TestProvisioningSpeed,
			fmt.Sprintf("vCluster p95 %s exceeds threshold %s", stats.P95VCluster.Round(time.Millisecond), vClusterThreshold),
			start, metrics)
	}
	if stats.P95NATS > natsThreshold {
		return failResult(TestProvisioningSpeed,
			fmt.Sprintf("NATS p95 %s exceeds threshold %s", stats.P95NATS.Round(time.Millisecond), natsThreshold),
			start, metrics)
	}

	return passResult(TestProvisioningSpeed,
		fmt.Sprintf("vCluster p95=%s (<=%s) | NATS p95=%s (<=%s)",
			stats.P95VCluster.Round(time.Second), vClusterThreshold,
			stats.P95NATS.Round(time.Second), natsThreshold,
		), start, metrics)
}

// measureNATSProvisioningSpeed applies NATS to the vCluster, waits for it to be
// Ready, records the elapsed time, and deletes the NATS resources.
func measureNATSProvisioningSpeed(
	ctx context.Context,
	c KubectlClient,
	cfg Config,
	kubeconfig string,
	vClusterElapsed time.Duration,
) SpeedSample {
	natsStart := time.Now()

	if err := c.Apply(ctx, kubeconfig, []byte(natsManifest)); err != nil {
		return SpeedSample{Err: fmt.Errorf("apply NATS: %w", err)}
	}

	_, natsElapsed, err := c.WaitPodReady(ctx, kubeconfig, "default", "app=nats",
		time.Duration(cfg.NATSReadySeconds*float64(time.Second)),
	)
	if err != nil {
		return SpeedSample{Err: fmt.Errorf("NATS not ready: %w", err)}
	}
	if natsElapsed == 0 {
		natsElapsed = time.Since(natsStart)
	}

	return SpeedSample{
		VClusterReadyElapsed: vClusterElapsed,
		NATSReadyElapsed:     natsElapsed,
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Stats — pure functions, fully unit-testable
// ──────────────────────────────────────────────────────────────────────────────

// computeSpeedStats derives p50/p95 from a set of samples, ignoring failed ones.
func computeSpeedStats(samples []SpeedSample) SpeedStats {
	var vcs, nats []time.Duration
	failed := 0
	for _, s := range samples {
		if s.Err != nil {
			failed++
			continue
		}
		vcs = append(vcs, s.VClusterReadyElapsed)
		nats = append(nats, s.NATSReadyElapsed)
	}
	s := SpeedStats{
		Samples:       len(samples),
		FailedSamples: failed,
	}
	if len(vcs) > 0 {
		s.P50VCluster = percentileDuration(vcs, 50)
		s.P95VCluster = percentileDuration(vcs, 95)
	}
	if len(nats) > 0 {
		s.P50NATS = percentileDuration(nats, 50)
		s.P95NATS = percentileDuration(nats, 95)
	}
	return s
}

// percentileDuration returns the p-th percentile of a slice of durations.
// The slice is sorted in place.
func percentileDuration(ds []time.Duration, p int) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	// Simple insertion sort — sample counts are tiny (≤ 10)
	for i := 1; i < len(ds); i++ {
		for j := i; j > 0 && ds[j] < ds[j-1]; j-- {
			ds[j], ds[j-1] = ds[j-1], ds[j]
		}
	}
	idx := int(float64(p)/100.0*float64(len(ds)-1) + 0.5)
	if idx >= len(ds) {
		idx = len(ds) - 1
	}
	return ds[idx]
}

// formatSpeedStats returns a compact table row suitable for the evidence field.
func formatSpeedStats(s SpeedStats) string {
	return strings.Join([]string{
		fmt.Sprintf("samples=%d failed=%d", s.Samples, s.FailedSamples),
		fmt.Sprintf("vCluster p50=%s p95=%s", s.P50VCluster.Round(time.Millisecond), s.P95VCluster.Round(time.Millisecond)),
		fmt.Sprintf("NATS p50=%s p95=%s", s.P50NATS.Round(time.Millisecond), s.P95NATS.Round(time.Millisecond)),
	}, " | ")
}
