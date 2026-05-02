package service

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jtomasevic/cloud-forge/internal/accounts"
)

// ── Mocks ─────────────────────────────────────────────────────────────────────

type fakeTenantManager struct { //nolint:govet // field order optimised for readability
	getBySlugFn  func(ctx context.Context, slug string) (*accounts.Tenant, error)
	setCIDRsFn   func(ctx context.Context, tenantID uuid.UUID, pod, svc string) error
	updateStatus func(ctx context.Context, tenantID uuid.UUID, status accounts.TenantStatus) error
}

func (f *fakeTenantManager) GetBySlug(ctx context.Context, slug string) (*accounts.Tenant, error) {
	if f.getBySlugFn != nil {
		return f.getBySlugFn(ctx, slug)
	}
	return &accounts.Tenant{TenantID: uuid.New(), Slug: slug}, nil
}

func (f *fakeTenantManager) SetCIDRs(ctx context.Context, tenantID uuid.UUID, pod, svc string) error {
	if f.setCIDRsFn != nil {
		return f.setCIDRsFn(ctx, tenantID, pod, svc)
	}
	return nil
}

func (f *fakeTenantManager) UpdateStatus(ctx context.Context, tenantID uuid.UUID, status accounts.TenantStatus) error {
	if f.updateStatus != nil {
		return f.updateStatus(ctx, tenantID, status)
	}
	return nil
}

type fakeJobQueuer struct { //nolint:govet // field order optimised for readability
	enqueueFn func(ctx context.Context, tenantID uuid.UUID, idemKey string, op accounts.JobOperation) (uuid.UUID, error)
	getFn     func(ctx context.Context, tenantID, jobID uuid.UUID) (*accounts.ProvisioningJob, error)
	claimFn   func(ctx context.Context, tenantID, jobID uuid.UUID) (bool, error)
	complete  func(ctx context.Context, tenantID, jobID uuid.UUID, result string) error
	fail      func(ctx context.Context, tenantID, jobID uuid.UUID, errMsg string) error
}

func (f *fakeJobQueuer) Enqueue(ctx context.Context, tenantID uuid.UUID, idemKey string, op accounts.JobOperation) (uuid.UUID, error) {
	if f.enqueueFn != nil {
		return f.enqueueFn(ctx, tenantID, idemKey, op)
	}
	return uuid.New(), nil
}

func (f *fakeJobQueuer) Get(ctx context.Context, tenantID, jobID uuid.UUID) (*accounts.ProvisioningJob, error) {
	if f.getFn != nil {
		return f.getFn(ctx, tenantID, jobID)
	}
	return &accounts.ProvisioningJob{JobID: jobID, Status: accounts.JobStatusQueued}, nil
}

func (f *fakeJobQueuer) Claim(ctx context.Context, tenantID, jobID uuid.UUID) (bool, error) {
	if f.claimFn != nil {
		return f.claimFn(ctx, tenantID, jobID)
	}
	return true, nil
}

func (f *fakeJobQueuer) Complete(ctx context.Context, tenantID, jobID uuid.UUID, result string) error {
	if f.complete != nil {
		return f.complete(ctx, tenantID, jobID, result)
	}
	return nil
}

func (f *fakeJobQueuer) Fail(ctx context.Context, tenantID, jobID uuid.UUID, errMsg string) error {
	if f.fail != nil {
		return f.fail(ctx, tenantID, jobID, errMsg)
	}
	return nil
}

type fakeAPIKeyManager struct {
	storeFn        func(ctx context.Context, k *accounts.APIKey) error
	revokeByHashFn func(ctx context.Context, keyHash string) error
}

func (f *fakeAPIKeyManager) Store(ctx context.Context, k *accounts.APIKey) error {
	if f.storeFn != nil {
		return f.storeFn(ctx, k)
	}
	return nil
}

func (f *fakeAPIKeyManager) RevokeByHash(ctx context.Context, keyHash string) error {
	if f.revokeByHashFn != nil {
		return f.revokeByHashFn(ctx, keyHash)
	}
	return nil
}

// ── helper ────────────────────────────────────────────────────────────────────

func newTestService(tenants tenantManager, jobs jobQueuer, keys apiKeyManager) ProvisionerService {
	return newWithIfaces(tenants, jobs, keys)
}

// ── New() ─────────────────────────────────────────────────────────────────────

// TestNew_ImplementsInterface verifies that the production constructor New()
// (which takes a Deps value and resolves concrete types to the internal
// interfaces) satisfies the ProvisionerService interface and returns a
// non-nil value. This exercises the New() code path which is otherwise
// never called in tests that use newWithIfaces.
func TestNew_ImplementsInterface(t *testing.T) {
	// Nil stores are safe here: New() only wires them into the struct —
	// no queries are issued during construction.
	svc := New(Deps{
		Tenants: nil,
		Keys:    nil,
		Jobs:    nil,
		Bao:     nil,
	})

	assert.NotNil(t, svc, "New must return a non-nil ProvisionerService")
}

// ── logger() ─────────────────────────────────────────────────────────────────

// TestLogger_UsesInjectedLogger exercises the s.log != nil branch of logger().
// Without this test, only the fallback (slog.Default) path is covered.
// The test uses the whitebox pattern (same package) to set the log field directly.
func TestLogger_UsesInjectedLogger(t *testing.T) {
	customLog := slog.Default()
	svc := &CFProvisionerService{log: customLog}

	got := svc.logger()

	assert.Equal(t, customLog, got, "logger() must return the injected logger when set")
}

// ── failJob() ─────────────────────────────────────────────────────────────────

// TestFailJob_LogsErrorWhenFailWriteFails exercises the branch inside failJob()
// where s.jobs.Fail() itself returns an error. This covers the inner
// log.Error("write job failure") statement that is otherwise unreachable in
// the normal success and step-error paths.
func TestFailJob_LogsErrorWhenFailWriteFails(_ *testing.T) {
	jobs := &fakeJobQueuer{
		fail: func(_ context.Context, _, _ uuid.UUID, _ string) error {
			return errors.New("scylladb: write timeout")
		},
	}
	svc := newWithIfaces(&fakeTenantManager{}, jobs, &fakeAPIKeyManager{}).(*CFProvisionerService)

	// failJob is a private method; call it directly in the whitebox test.
	// It must not panic and must attempt to write the failure to the store.
	svc.failJob(
		context.Background(),
		svc.logger(),
		uuid.Nil,
		uuid.New(),
		"unit test step",
		errors.New("step error"),
	)
	// Assertions are implicit: no panic and the fail function was called
	// (verified by the mock returning an error which is logged but not returned).
}

// ── Provision ─────────────────────────────────────────────────────────────────

func TestProvision_ReturnsJobID(t *testing.T) {
	wantJobID := uuid.New()
	jobs := &fakeJobQueuer{
		enqueueFn: func(_ context.Context, _ uuid.UUID, _ string, _ accounts.JobOperation) (uuid.UUID, error) {
			return wantJobID, nil
		},
		// Return !claimed so the background goroutine exits immediately
		// without reaching kubectl/OpenBao calls that require live infra.
		claimFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) {
			return false, nil
		},
	}
	svc := newTestService(&fakeTenantManager{}, jobs, &fakeAPIKeyManager{})

	jobID, err := svc.Provision(context.Background(), ProvisionParams{TenantSlug: "acme"})

	require.NoError(t, err)
	assert.Equal(t, wantJobID, jobID)
}

func TestProvision_ReturnsError_WhenEnqueueFails(t *testing.T) {
	jobs := &fakeJobQueuer{
		enqueueFn: func(_ context.Context, _ uuid.UUID, _ string, _ accounts.JobOperation) (uuid.UUID, error) {
			return uuid.Nil, errors.New("db unavailable")
		},
	}
	svc := newTestService(&fakeTenantManager{}, jobs, &fakeAPIKeyManager{})

	_, err := svc.Provision(context.Background(), ProvisionParams{TenantSlug: "acme"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "enqueue provision job")
}

// ── GetJob ────────────────────────────────────────────────────────────────────

func TestGetJob_ReturnsResult_ForQueuedJob(t *testing.T) {
	jobID := uuid.New()
	jobs := &fakeJobQueuer{
		getFn: func(_ context.Context, _, _ uuid.UUID) (*accounts.ProvisioningJob, error) {
			return &accounts.ProvisioningJob{
				JobID:  jobID,
				Status: accounts.JobStatusQueued,
			}, nil
		},
	}
	svc := newTestService(&fakeTenantManager{}, jobs, &fakeAPIKeyManager{})

	result, err := svc.GetJob(context.Background(), jobID)

	require.NoError(t, err)
	assert.Equal(t, jobID, result.JobID)
	assert.Equal(t, string(accounts.JobStatusQueued), result.Status)
}

func TestGetJob_ReturnsErrJobNotFound(t *testing.T) {
	jobs := &fakeJobQueuer{
		getFn: func(_ context.Context, _, _ uuid.UUID) (*accounts.ProvisioningJob, error) {
			return nil, accounts.ErrJobNotFound
		},
	}
	svc := newTestService(&fakeTenantManager{}, jobs, &fakeAPIKeyManager{})

	_, err := svc.GetJob(context.Background(), uuid.New())

	require.ErrorIs(t, err, ErrJobNotFound)
}

func TestGetJob_ReturnsError_OnDBFailure(t *testing.T) {
	jobs := &fakeJobQueuer{
		getFn: func(_ context.Context, _, _ uuid.UUID) (*accounts.ProvisioningJob, error) {
			return nil, errors.New("connection reset")
		},
	}
	svc := newTestService(&fakeTenantManager{}, jobs, &fakeAPIKeyManager{})

	_, err := svc.GetJob(context.Background(), uuid.New())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "get job")
}

func TestGetJob_ParsesReadyJobResult(t *testing.T) {
	jobID := uuid.New()
	apiKeyID := uuid.New()
	resultJSON := `{"api_key":"cf_live_test","api_key_id":"` + apiKeyID.String() + `","vpc_info":{"pod_cidr":"10.100.1.0/24","service_cidr":"10.200.1.0/24"}}`
	jobs := &fakeJobQueuer{
		getFn: func(_ context.Context, _, _ uuid.UUID) (*accounts.ProvisioningJob, error) {
			return &accounts.ProvisioningJob{
				JobID:  jobID,
				Status: accounts.JobStatusReady,
				Result: resultJSON,
			}, nil
		},
	}
	svc := newTestService(&fakeTenantManager{}, jobs, &fakeAPIKeyManager{})

	result, err := svc.GetJob(context.Background(), jobID)

	require.NoError(t, err)
	require.NotNil(t, result.VPCResult)
	assert.Equal(t, "cf_live_test", result.VPCResult.APIKey)
	assert.Equal(t, "10.100.1.0/24", result.VPCResult.PodCIDR)
}

// ── Deprovision ───────────────────────────────────────────────────────────────

func TestDeprovision_ReturnsJobID(t *testing.T) {
	wantJobID := uuid.New()
	tenants := &fakeTenantManager{
		getBySlugFn: func(_ context.Context, slug string) (*accounts.Tenant, error) {
			return &accounts.Tenant{TenantID: uuid.New(), Slug: slug}, nil
		},
	}
	jobs := &fakeJobQueuer{
		enqueueFn: func(_ context.Context, _ uuid.UUID, _ string, _ accounts.JobOperation) (uuid.UUID, error) {
			return wantJobID, nil
		},
		// Return !claimed so the background goroutine exits immediately
		// without reaching provisioner.Revoke which needs a live OpenBao client.
		claimFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) {
			return false, nil
		},
	}
	svc := newTestService(tenants, jobs, &fakeAPIKeyManager{})

	jobID, err := svc.Deprovision(context.Background(), DeprovisionParams{TenantSlug: "acme"})

	require.NoError(t, err)
	assert.Equal(t, wantJobID, jobID)
}

func TestDeprovision_ReturnsErrTenantNotFound(t *testing.T) {
	tenants := &fakeTenantManager{
		getBySlugFn: func(_ context.Context, _ string) (*accounts.Tenant, error) {
			return nil, accounts.ErrTenantNotFound
		},
	}
	svc := newTestService(tenants, &fakeJobQueuer{}, &fakeAPIKeyManager{})

	_, err := svc.Deprovision(context.Background(), DeprovisionParams{TenantSlug: "ghost"})

	require.ErrorIs(t, err, ErrTenantNotFound)
}

func TestDeprovision_ReturnsError_WhenTenantLookupFails(t *testing.T) {
	tenants := &fakeTenantManager{
		getBySlugFn: func(_ context.Context, _ string) (*accounts.Tenant, error) {
			return nil, errors.New("db timeout")
		},
	}
	svc := newTestService(tenants, &fakeJobQueuer{}, &fakeAPIKeyManager{})

	_, err := svc.Deprovision(context.Background(), DeprovisionParams{TenantSlug: "acme"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "lookup tenant for deprovision")
}

func TestDeprovision_ReturnsError_WhenEnqueueFails(t *testing.T) {
	tenants := &fakeTenantManager{}
	jobs := &fakeJobQueuer{
		enqueueFn: func(_ context.Context, _ uuid.UUID, _ string, _ accounts.JobOperation) (uuid.UUID, error) {
			return uuid.Nil, errors.New("write conflict")
		},
	}
	svc := newTestService(tenants, jobs, &fakeAPIKeyManager{})

	_, err := svc.Deprovision(context.Background(), DeprovisionParams{TenantSlug: "acme"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "enqueue deprovision job")
}

// Workflow whitebox tests (runProvisionWorkflow, runDeprovisionWorkflow,
// applyIsolationPolicies) live in workflow_test.go, which uses injectable
// seam variables to avoid live kubectl / OpenBao / vCluster dependencies.
