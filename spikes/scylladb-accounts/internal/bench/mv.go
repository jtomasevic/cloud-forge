package bench

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/gocql/gocql"
)

// BenchMVSlugLookup measures the latency of tenant slug resolution through
// the cf.tenants_by_slug materialized view.
//
// CF-Router performs this lookup on every request that carries a JWT with a
// "slug" sub-claim.  The target is p99 < 5 ms — slightly looser than the
// API key lookup because the JWT path is less frequent than the API key path.
//
// tenants must contain at least one entry (pre-seeded by SeedTenants).
func BenchMVSlugLookup(
	sess *gocql.Session,
	cfg Config,
	tenants []TenantRow,
) Result {
	type job struct{ slug string }

	jobs := make(chan job, cfg.Ops)
	for i := range cfg.Ops {
		jobs <- job{slug: tenants[i%len(tenants)].Slug}
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
				t0 := time.Now()
				var tenantID gocql.UUID
				var status string
				err := sess.Query(
					`SELECT tenant_id, status FROM cf.tenants_by_slug WHERE slug = ?`,
					j.slug,
				).Consistency(gocql.Quorum).Scan(&tenantID, &status)
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

	return BuildResult(BenchMVSlug, samples, int(errCount), total)
}

// BenchMVEmailLookup measures the latency of user email resolution through
// the cf.users_by_email materialized view.
//
// The login flow calls this lookup once per authentication attempt.  It is
// less latency-sensitive than the routing hot path but should still complete
// in < 5 ms p99 to keep the login UX responsive.
//
// users must contain at least one entry (pre-seeded by SeedUsers).
func BenchMVEmailLookup(
	sess *gocql.Session,
	cfg Config,
	users []UserRow,
) Result {
	type job struct{ email string }

	jobs := make(chan job, cfg.Ops)
	for i := range cfg.Ops {
		jobs <- job{email: users[i%len(users)].Email}
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
				t0 := time.Now()
				var tenantID, userID gocql.UUID
				var status string
				err := sess.Query(
					`SELECT tenant_id, user_id, status
					   FROM cf.users_by_email WHERE email = ?`,
					j.email,
				).Consistency(gocql.Quorum).Scan(&tenantID, &userID, &status)
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

	return BuildResult(BenchMVEmail, samples, int(errCount), total)
}
