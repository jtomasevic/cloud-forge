package bench

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/gocql/gocql"
)

// BenchAPIKeyLookup measures the latency of the CF-Router hot path:
// a single-partition SELECT on cf.api_keys by key_hash.
//
// The benchmark fires cfg.Ops lookups across cfg.Concurrency goroutines.
// hashes must contain at least one entry (pre-seeded by SeedAPIKeys).
// The consistency level is passed in as a parameter so the caller can
// compare QUORUM vs ONE in the same run.
func BenchAPIKeyLookup(
	sess *gocql.Session,
	cfg Config,
	hashes []string,
	consistency gocql.Consistency,
	name BenchName,
) Result {
	type job struct{ hash string }

	jobs := make(chan job, cfg.Ops)
	// Fill the job queue: cycle through hashes so every hash gets hit evenly.
	for i := range cfg.Ops {
		jobs <- job{hash: hashes[i%len(hashes)]}
	}
	close(jobs)

	samples := make(Samples, 0, cfg.Ops)
	var mu sync.Mutex
	var errCount int32

	start := time.Now()
	var wg sync.WaitGroup
	for range cfg.Concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				// Measure from just before the network call to just after the
				// first byte arrives (Scan returns after the first row is read).
				t0 := time.Now()
				var (
					keyID      gocql.UUID
					tenantID   gocql.UUID
					status     string
					scopes     string
				)
				err := sess.Query(
					`SELECT key_id, tenant_id, status, scopes
					   FROM cf.api_keys WHERE key_hash = ?`, j.hash,
				).Consistency(consistency).Scan(&keyID, &tenantID, &status, &scopes)
				elapsed := time.Since(t0)

				if err != nil {
					atomic.AddInt32(&errCount, 1)
					continue
				}
				mu.Lock()
				samples = append(samples, elapsed)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	total := time.Since(start)

	return BuildResult(name, samples, int(errCount), total)
}
