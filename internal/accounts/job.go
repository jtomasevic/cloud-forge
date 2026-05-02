package accounts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gocql/gocql"
	"github.com/google/uuid"
)

// JobStatus is the lifecycle state of a provisioning job.
type JobStatus string

const (
	// JobStatusQueued means the job has been created but not yet picked up by
	// a worker goroutine.
	JobStatusQueued JobStatus = "QUEUED"

	// JobStatusProvisioning means a worker has claimed the job and is executing
	// the provisioning sequence.
	JobStatusProvisioning JobStatus = "PROVISIONING"

	// JobStatusReady means provisioning completed successfully. The result
	// field contains the JSON job result (vpc_info + api_key_id + key_hash).
	JobStatusReady JobStatus = "READY"

	// JobStatusFailed means provisioning failed. The error_message field
	// contains a human-readable description of the failure.
	JobStatusFailed JobStatus = "FAILED"
)

// JobOperation distinguishes the type of work a job performs.
type JobOperation string

const (
	// JobOperationProvisionVPC is the operation for the 10-step VPC provisioning
	// workflow described in docs/CF-VPC-Service-Proposal.md.
	JobOperationProvisionVPC JobOperation = "PROVISION_VPC"

	// JobOperationDeprovisionVPC is the operation for the reverse teardown flow.
	JobOperationDeprovisionVPC JobOperation = "DEPROVISION_VPC"
)

// ProvisioningJob is a row in cf.provisioning_jobs.
type ProvisioningJob struct {
	StartedAt      time.Time
	CompletedAt    time.Time
	IdempotencyKey string
	Operation      JobOperation
	Status         JobStatus
	ErrorMessage   string
	Result         string
	JobID          uuid.UUID
	TenantID       uuid.UUID
}

// ErrJobNotFound is returned when no job exists for the requested ID.
var ErrJobNotFound = errors.New("accounts: provisioning job not found")

// JobStore provides access to cf.provisioning_jobs and the idempotency
// deduplication table cf.provisioning_jobs_by_idem.
type JobStore struct {
	sess *gocql.Session
}

// NewJobStore returns a JobStore backed by the given session.
func NewJobStore(sess *gocql.Session) *JobStore {
	return &JobStore{sess: sess}
}

// Enqueue inserts a new job in QUEUED status. It first checks the idempotency
// table with an LWT insert. If the same (tenantID, idempotencyKey) has already
// been submitted, the existing job_id is returned without creating a duplicate
// job (preventing re-provisioning from UI double-clicks or retried requests).
//
// Returns the job_id that the caller should return to the client as a handle
// for the GET /jobs/{job_id} polling endpoint.
func (s *JobStore) Enqueue(ctx context.Context, tenantID uuid.UUID, idemKey string, op JobOperation) (uuid.UUID, error) {
	// Step 1: attempt to reserve the idempotency slot.
	jobID, err := uuid.NewRandom()
	if err != nil {
		return uuid.Nil, fmt.Errorf("accounts: generate job_id: %w", err)
	}

	dest := make(map[string]interface{})
	applied, err := s.sess.Query(`
		INSERT INTO cf.provisioning_jobs_by_idem
		  (tenant_id, idempotency_key, job_id)
		VALUES (?, ?, ?)
		IF NOT EXISTS`,
		gocql.UUID(tenantID), idemKey, gocql.UUID(jobID),
	).WithContext(ctx).MapScanCAS(dest)
	if err != nil {
		return uuid.Nil, fmt.Errorf("accounts: idempotency insert for tenant %s: %w", tenantID, err)
	}
	if !applied {
		// Another call with the same idempotency key already exists.
		// Return the existing job_id from the rejected row.
		if existing, ok := dest["job_id"]; ok {
			if existingUUID, ok := existing.(gocql.UUID); ok {
				return uuid.UUID(existingUUID), nil
			}
		}
		return uuid.Nil, fmt.Errorf("accounts: idempotency conflict for tenant %s key %q: could not extract existing job_id", tenantID, idemKey)
	}

	// Step 2: write the actual job row.
	now := time.Now().UTC()
	err = s.sess.Query(`
		INSERT INTO cf.provisioning_jobs
		  (job_id, tenant_id, idempotency_key, operation, status,
		   error_message, result, started_at, completed_at)
		VALUES (?, ?, ?, ?, ?, '', '', ?, ?)`,
		gocql.UUID(jobID), gocql.UUID(tenantID), idemKey,
		string(op), string(JobStatusQueued),
		now, time.Time{},
	).WithContext(ctx).Exec()
	if err != nil {
		return uuid.Nil, fmt.Errorf("accounts: insert job row for tenant %s: %w", tenantID, err)
	}
	return jobID, nil
}

// Claim transitions a job from QUEUED to PROVISIONING using an LWT
// (IF status = 'QUEUED'). Returns true if this caller won the claim, false if
// another worker already claimed it. False is not an error — it means the job
// is already being processed by another replica.
func (s *JobStore) Claim(ctx context.Context, tenantID, jobID uuid.UUID) (bool, error) {
	dest := make(map[string]interface{})
	applied, err := s.sess.Query(`
		UPDATE cf.provisioning_jobs
		  SET status = ?, started_at = ?
		  WHERE tenant_id = ? AND job_id = ?
		  IF status = ?`,
		string(JobStatusProvisioning), time.Now().UTC(),
		gocql.UUID(tenantID), gocql.UUID(jobID),
		string(JobStatusQueued),
	).WithContext(ctx).MapScanCAS(dest)
	if err != nil {
		return false, fmt.Errorf("accounts: claim job %s: %w", jobID, err)
	}
	return applied, nil
}

// Complete marks a job as READY and stores the JSON result blob.
// result must NOT contain the raw API key; only api_key_id, key_hash,
// and vpc_info are stored here.
func (s *JobStore) Complete(ctx context.Context, tenantID, jobID uuid.UUID, result string) error {
	return s.sess.Query(`
		UPDATE cf.provisioning_jobs
		  SET status = ?, result = ?, completed_at = ?
		  WHERE tenant_id = ? AND job_id = ?`,
		string(JobStatusReady), result, time.Now().UTC(),
		gocql.UUID(tenantID), gocql.UUID(jobID),
	).WithContext(ctx).Exec()
}

// Fail marks a job as FAILED and stores the error message for debugging.
func (s *JobStore) Fail(ctx context.Context, tenantID, jobID uuid.UUID, errMsg string) error {
	return s.sess.Query(`
		UPDATE cf.provisioning_jobs
		  SET status = ?, error_message = ?, completed_at = ?
		  WHERE tenant_id = ? AND job_id = ?`,
		string(JobStatusFailed), errMsg, time.Now().UTC(),
		gocql.UUID(tenantID), gocql.UUID(jobID),
	).WithContext(ctx).Exec()
}

// Get returns the job record for the given (tenantID, jobID) pair.
// Returns ErrJobNotFound if no matching row exists.
func (s *JobStore) Get(ctx context.Context, tenantID, jobID uuid.UUID) (*ProvisioningJob, error) {
	var j ProvisioningJob
	var jID, tID gocql.UUID
	var status, op string

	err := s.sess.Query(`
		SELECT job_id, tenant_id, idempotency_key, operation, status,
		       error_message, result, started_at, completed_at
		  FROM cf.provisioning_jobs
		  WHERE tenant_id = ? AND job_id = ?`,
		gocql.UUID(tenantID), gocql.UUID(jobID),
	).WithContext(ctx).Scan(
		&jID, &tID, &j.IdempotencyKey, &op, &status,
		&j.ErrorMessage, &j.Result, &j.StartedAt, &j.CompletedAt,
	)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, ErrJobNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("accounts: get job %s: %w", jobID, err)
	}
	j.JobID = uuid.UUID(jID)
	j.TenantID = uuid.UUID(tID)
	j.Status = JobStatus(status)
	j.Operation = JobOperation(op)
	return &j, nil
}
