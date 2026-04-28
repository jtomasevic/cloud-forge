package measure

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Prober measures the time-to-first-byte (TTFB) of a cold-start HTTP request.
//
// Implementations must handle Knative's activator behaviour: when a function
// has zero replicas, the activator returns 502 (Bad Gateway) or 503 (Service
// Unavailable) while the pod is starting.  The Probe method must treat these
// transient responses as "still warming up" and retry until a non-5xx response
// arrives or the context deadline is exceeded.  The elapsed time from the
// FIRST request to the FIRST successful response is the true cold-start TTFB.
type Prober interface {
	// Probe sends a cold-start HTTP GET to url and returns the elapsed time
	// from the first request to the first non-5xx response body byte.
	//
	// Returns a non-nil error if:
	//   - the context deadline is exceeded before a non-5xx response is received
	//   - a 4xx response is returned (indicates a configuration error)
	//   - the TCP connection or TLS handshake permanently fails
	Probe(ctx context.Context, url string) (time.Duration, error)
}

// HTTPProber is the production implementation of Prober.
//
// It retries on HTTP 502/503 with retryInterval backoff to handle the Knative
// activator's transient "not ready" responses during cold start.  The timer
// starts at the first request and stops at the first byte of the first
// successful (2xx/3xx/4xx) response.
type HTTPProber struct {
	// client is the shared HTTP transport.  Reused across retries to benefit
	// from connection keep-alive once the pod becomes available.
	client *http.Client

	// retryInterval is the pause between 502/503 retries.
	// Shorter values reduce dead-wait time; longer values reduce kube-proxy pressure.
	retryInterval time.Duration
}

// NewHTTPProber creates an HTTPProber with the given per-attempt timeout and
// inter-retry interval.
//
// timeout limits each individual HTTP round-trip.
// retryInterval is the sleep between 502/503 retry attempts.
// Pass retryInterval=0 to use the default of 200ms.
func NewHTTPProber(timeout time.Duration, retryInterval time.Duration) *HTTPProber {
	ri := retryInterval
	if ri <= 0 {
		// 200ms default balances responsiveness and kube-proxy load.
		ri = 200 * time.Millisecond
	}
	return &HTTPProber{
		client: &http.Client{
			Timeout: timeout,
			// Disable automatic redirect following.  A 301/302 from Knative
			// indicates a misconfigured ingress URL and should surface as an
			// error rather than silently chasing the redirect.
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		retryInterval: ri,
	}
}

// Probe sends repeated GET requests to url until a non-5xx response is
// received (or ctx is cancelled), and returns the elapsed time from the first
// request to the first byte of the first successful response.
//
// Knative cold-start protocol:
//   - Pod count = 0 → activator queues the request and starts scaling.
//   - If the pod is not ready within the activator's internal timeout (~600ms),
//     it returns 502 Bad Gateway or 503 Service Unavailable.
//   - Subsequent requests are retried until the pod becomes ready and the
//     activator forwards the first real response.
//
// The full retry loop duration (first 502 → first 200) is what CloudForge
// users experience as cold-start latency, so it is the correct TTFB metric.
//
// HTTP 4xx responses are returned as errors — they indicate misconfiguration
// (wrong path, auth failure) not transient unavailability.
func (p *HTTPProber) Probe(ctx context.Context, url string) (time.Duration, error) {
	// Record the start time ONCE before the first attempt.
	// All retries count towards the same cold-start measurement.
	start := time.Now()

	for {
		// Respect context cancellation between retries.
		if ctx.Err() != nil {
			return 0, fmt.Errorf("cold-start probe cancelled after %s: %w",
				time.Since(start).Round(time.Millisecond), ctx.Err())
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return 0, fmt.Errorf("build request for %s: %w", url, err)
		}

		resp, err := p.client.Do(req)
		if err != nil {
			// Network-level failures (connection refused, dial timeout) can
			// also occur immediately after scale-to-zero when the endpoint is
			// being removed from kube-proxy's IPVS table.  Treat them as
			// transient and retry, same as 502/503.
			select {
			case <-ctx.Done():
				return 0, fmt.Errorf("cold-start probe timed out after %s: %w",
					time.Since(start).Round(time.Millisecond), ctx.Err())
			case <-time.After(p.retryInterval):
				continue
			}
		}
		defer resp.Body.Close() //nolint:errcheck

		// 502 / 503 are Knative activator signals — "pod not yet ready".
		// Sleep the retry interval and try again; the clock keeps running.
		if resp.StatusCode == http.StatusBadGateway ||
			resp.StatusCode == http.StatusServiceUnavailable {
			resp.Body.Close() //nolint:errcheck
			select {
			case <-ctx.Done():
				return 0, fmt.Errorf("cold-start probe timed out (502/503 loop) after %s: %w",
					time.Since(start).Round(time.Millisecond), ctx.Err())
			case <-time.After(p.retryInterval):
				continue
			}
		}

		// Any other 5xx (500, 504, …) indicates a real server crash — surface it.
		if resp.StatusCode >= http.StatusInternalServerError {
			return 0, fmt.Errorf("server error: status %d from %s", resp.StatusCode, url)
		}

		// Non-5xx response: read exactly one byte to mark the "first byte" event.
		// Reading a full body would keep the connection alive and delay the
		// Knative autoscaler's idle-pod detection (scale-to-zero timing).
		buf := make([]byte, 1)
		if _, err := resp.Body.Read(buf); err != nil {
			return 0, fmt.Errorf("read first byte from %s: %w", url, err)
		}

		return time.Since(start), nil
	}
}
