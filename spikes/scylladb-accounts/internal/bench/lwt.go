package bench

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gocql/gocql"
)

// BenchLWTJobClaim measures LWT correctness and latency for the idempotent
// job-creation pattern used by CF-Provisioner:
//
//	INSERT INTO cf.provisioning_jobs_by_idem
//	  (tenant_id, idempotency_key, job_id) VALUES (?, ?, ?)
//	IF NOT EXISTS;
//
// cfg.LWTWriters goroutines all attempt to create a job with the same
// idempotency_key simultaneously.  Exactly one should receive applied=true.
// The function returns an LWTResult so the caller can assert Correct().
func BenchLWTJobClaim(sess *gocql.Session, cfg Config) LWTResult {
	tenantID := gocql.TimeUUID()
	idemKey := fmt.Sprintf("spike-idem-%d", time.Now().UnixNano())

	samples := make(Samples, 0, cfg.LWTWriters)
	var mu sync.Mutex
	var winners, losers, errs int32

	start := time.Now()
	var wg sync.WaitGroup
	for range cfg.LWTWriters {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// MapScanCAS handles the IF NOT EXISTS response generically:
			// on applied=true  the server returns only [applied]=true (1 column)
			// on applied=false the server returns [applied]=false + existing columns
			// gocql extracts the bool from dest["[applied]"] and removes the key.
			jobID := gocql.TimeUUID()

			t0 := time.Now()
			dest := make(map[string]interface{})
			applied, err := sess.Query(`
				INSERT INTO cf.provisioning_jobs_by_idem
				  (tenant_id, idempotency_key, job_id)
				VALUES (?, ?, ?)
				IF NOT EXISTS`,
				tenantID, idemKey, jobID,
			).MapScanCAS(dest)
			elapsed := time.Since(t0)

			if err != nil {
				if atomic.AddInt32(&errs, 1) == 1 {
					_, _ = fmt.Fprintf(os.Stderr, "[lwt_job_claim] error: %v\n", err)
				}
				return
			}

			mu.Lock()
			samples = append(samples, elapsed)
			mu.Unlock()

			if applied {
				atomic.AddInt32(&winners, 1)
			} else {
				atomic.AddInt32(&losers, 1)
			}
		}()
	}
	wg.Wait()
	total := time.Since(start)

	return LWTResult{
		Result:  BuildResult(BenchLWTClaim, samples, int(errs), total),
		Winners: int(winners),
		Losers:  int(losers),
	}
}

// BenchLWTStateTransition measures LWT correctness and latency for the
// provisioning job state-machine transition:
//
//	UPDATE cf.provisioning_jobs
//	  SET status = 'PROVISIONING', started_at = ?
//	  WHERE tenant_id = ? AND job_id = ?
//	  IF status = 'QUEUED';
//
// Only one of cfg.LWTWriters concurrent goroutines should succeed (the one
// that races first).  The job row is pre-inserted with status=QUEUED.
func BenchLWTStateTransition(sess *gocql.Session, cfg Config) (LWTResult, error) {
	tenantID := gocql.TimeUUID()
	jobID := gocql.TimeUUID()

	// Pre-insert a job row in QUEUED state that all goroutines will race to claim.
	err := sess.Query(`
		INSERT INTO cf.provisioning_jobs
		  (job_id, tenant_id, idempotency_key, operation, service_type,
		   instance_id, status, started_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		jobID, tenantID, "spike-transition", "PROVISION", "NATS",
		gocql.TimeUUID(), "QUEUED",
		time.Time{}, time.Time{},
	).Exec()
	if err != nil {
		return LWTResult{}, fmt.Errorf("insert seed job: %w", err)
	}

	samples := make(Samples, 0, cfg.LWTWriters)
	var mu sync.Mutex
	var winners, losers, errs int32

	start := time.Now()
	var wg sync.WaitGroup
	for range cfg.LWTWriters {
		wg.Add(1)
		go func() {
			defer wg.Done()

			t0 := time.Now()
			// For UPDATE IF … the server returns [applied] plus the existing row
			// on rejection.  MapScanCAS handles both cases generically.
			dest := make(map[string]interface{})
			applied, err := sess.Query(`
				UPDATE cf.provisioning_jobs
				  SET status = 'PROVISIONING', started_at = ?
				  WHERE tenant_id = ? AND job_id = ?
				  IF status = 'QUEUED'`,
				time.Now(), tenantID, jobID,
			).MapScanCAS(dest)
			elapsed := time.Since(t0)

			if err != nil {
				if atomic.AddInt32(&errs, 1) == 1 {
					_, _ = fmt.Fprintf(os.Stderr, "[lwt_state_transition] error: %v\n", err)
				}
				return
			}

			mu.Lock()
			samples = append(samples, elapsed)
			mu.Unlock()

			if applied {
				atomic.AddInt32(&winners, 1)
			} else {
				atomic.AddInt32(&losers, 1)
			}
		}()
	}
	wg.Wait()
	total := time.Since(start)

	return LWTResult{
		Result:  BuildResult(BenchLWTTransition, samples, int(errs), total),
		Winners: int(winners),
		Losers:  int(losers),
	}, nil
}
