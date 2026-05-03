package service

// Tests for runProvisionWorkflow and runDeprovisionWorkflow.
//
// These workflows run as goroutines in production but are called synchronously
// here (same package, whitebox access) so no synchronisation is needed.
// All external I/O (kubectl, OpenBao, vCluster) is replaced via the
// package-level seam variables defined in service.go.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	openbao "github.com/openbao/openbao/api/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jtomasevic/cloud-forge/internal/accounts"
	"github.com/jtomasevic/cloud-forge/internal/provisioner"
)

// ── seam helpers ──────────────────────────────────────────────────────────────

// resetWorkflowSeams restores all injectable function variables to their
// production defaults.  Call via defer at the start of each test that
// overrides any seam.
func resetWorkflowSeams() {
	createNamespaceFn = createNamespace
	applyIsolationPoliciesFn = applyIsolationPolicies
	createVClusterFn = provisioner.CreateVCluster
	storeKubeconfigFn = provisioner.Store
	generateAPIKeyFn = provisioner.GenerateAPIKey
	revokeKubeconfigFn = provisioner.Revoke
	deleteVClusterFn = provisioner.DeleteVCluster
	kubectlApplyBytesFn = kubectlApplyBytes
	tenantIsolationPolicyFn = provisioner.TenantIsolationPolicy
	provisionerAccessPolicyFn = provisioner.ProvisionerAccessPolicy
}

// stubVClusterResult returns a dummy VClusterResult sufficient for the workflow.
func stubVClusterResult() *provisioner.VClusterResult {
	return &provisioner.VClusterResult{KubeconfigYAML: "apiVersion: v1"}
}

// stubGeneratedKey returns a dummy GeneratedAPIKey.
func stubGeneratedKey() *provisioner.GeneratedAPIKey {
	return &provisioner.GeneratedAPIKey{
		RawKey:  "cf_live_testkey123",
		KeyHash: "abc123",
		Record: &accounts.APIKey{
			KeyID:    uuid.New(),
			TenantID: uuid.New(),
		},
	}
}

// allExternalSeamsSucceed injects no-op success functions for every external
// call, giving the workflow a clear path to completion.
func allExternalSeamsSucceed() {
	createNamespaceFn = func(_ context.Context, _ string) error { return nil }
	applyIsolationPoliciesFn = func(_ context.Context, _ string) error { return nil }
	createVClusterFn = func(_ context.Context, _ provisioner.VClusterConfig) (*provisioner.VClusterResult, error) {
		return stubVClusterResult(), nil
	}
	storeKubeconfigFn = func(_ context.Context, _ *openbao.Client, _, _ string) error { return nil }
	generateAPIKeyFn = func(_ context.Context, _ provisioner.APIKeyStorer, _ uuid.UUID, _, _ string) (*provisioner.GeneratedAPIKey, error) {
		return stubGeneratedKey(), nil
	}
	revokeKubeconfigFn = func(_ context.Context, _ *openbao.Client, _ string) error { return nil }
	deleteVClusterFn = func(_ context.Context, _, _ string) error { return nil }
}

// ── runProvisionWorkflow ──────────────────────────────────────────────────────

// TestRunProvisionWorkflow_ClaimError returns without marking the job failed
// when the claim itself errors (the LWT error path).
func TestRunProvisionWorkflow_ClaimError(t *testing.T) {
	defer resetWorkflowSeams()
	failCalled := false
	jobs := &fakeJobQueuer{
		claimFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) {
			return false, errors.New("lock: scylladb error")
		},
		fail: func(_ context.Context, _, _ uuid.UUID, _ string) error {
			failCalled = true
			return nil
		},
	}
	svc := newWithIfaces(&fakeTenantManager{}, jobs, &fakeAPIKeyManager{}).(*CFProvisionerService)

	svc.runProvisionWorkflow(uuid.New(), ProvisionParams{TenantSlug: "t1"})

	assert.False(t, failCalled, "job.Fail must not be called when Claim itself errors")
}

// TestRunProvisionWorkflow_AlreadyClaimed returns silently when another replica
// holds the job (claim returns false, nil).
func TestRunProvisionWorkflow_AlreadyClaimed(t *testing.T) {
	defer resetWorkflowSeams()
	getBySlugCalled := false
	tenants := &fakeTenantManager{
		getBySlugFn: func(_ context.Context, _ string) (*accounts.Tenant, error) {
			getBySlugCalled = true
			return nil, nil
		},
	}
	jobs := &fakeJobQueuer{
		claimFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return false, nil },
	}
	svc := newWithIfaces(tenants, jobs, &fakeAPIKeyManager{}).(*CFProvisionerService)

	svc.runProvisionWorkflow(uuid.New(), ProvisionParams{TenantSlug: "t1"})

	assert.False(t, getBySlugCalled, "tenant.GetBySlug must not be called when job is already claimed")
}

// TestRunProvisionWorkflow_TenantNotFound fails the job when Step 2 cannot
// find the tenant record (it should have been created during registration).
func TestRunProvisionWorkflow_TenantNotFound(t *testing.T) {
	defer resetWorkflowSeams()
	failMsg := ""
	jobs := &fakeJobQueuer{
		claimFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
		fail: func(_ context.Context, _, _ uuid.UUID, msg string) error {
			failMsg = msg
			return nil
		},
	}
	tenants := &fakeTenantManager{
		getBySlugFn: func(_ context.Context, _ string) (*accounts.Tenant, error) {
			return nil, accounts.ErrTenantNotFound
		},
	}
	svc := newWithIfaces(tenants, jobs, &fakeAPIKeyManager{}).(*CFProvisionerService)

	svc.runProvisionWorkflow(uuid.New(), ProvisionParams{TenantSlug: "missing"})

	assert.Contains(t, failMsg, "tenant not found")
}

// TestRunProvisionWorkflow_TenantGetError fails the job when Step 2 returns
// an unexpected DB error on the GetBySlug call.
func TestRunProvisionWorkflow_TenantGetError(t *testing.T) {
	defer resetWorkflowSeams()
	failCalled := false
	jobs := &fakeJobQueuer{
		claimFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
		fail:    func(_ context.Context, _, _ uuid.UUID, _ string) error { failCalled = true; return nil },
	}
	tenants := &fakeTenantManager{
		getBySlugFn: func(_ context.Context, _ string) (*accounts.Tenant, error) {
			return nil, errors.New("scylladb: write timeout")
		},
	}
	svc := newWithIfaces(tenants, jobs, &fakeAPIKeyManager{}).(*CFProvisionerService)

	svc.runProvisionWorkflow(uuid.New(), ProvisionParams{TenantSlug: "t1"})

	assert.True(t, failCalled)
}

// TestRunProvisionWorkflow_NamespaceFails fails the job when Step 4 errors.
func TestRunProvisionWorkflow_NamespaceFails(t *testing.T) {
	defer resetWorkflowSeams()
	failCalled := false
	createNamespaceFn = func(_ context.Context, _ string) error {
		return errors.New("kubectl: not found")
	}
	jobs := &fakeJobQueuer{
		claimFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
		fail:    func(_ context.Context, _, _ uuid.UUID, _ string) error { failCalled = true; return nil },
	}
	tenants := &fakeTenantManager{}
	svc := newWithIfaces(tenants, jobs, &fakeAPIKeyManager{}).(*CFProvisionerService)

	svc.runProvisionWorkflow(uuid.New(), ProvisionParams{TenantSlug: "t1"})

	assert.True(t, failCalled)
}

// TestRunProvisionWorkflow_IsolationPoliciesFail fails the job when Step 5 errors.
func TestRunProvisionWorkflow_IsolationPoliciesFail(t *testing.T) {
	defer resetWorkflowSeams()
	failCalled := false
	createNamespaceFn = func(_ context.Context, _ string) error { return nil }
	applyIsolationPoliciesFn = func(_ context.Context, _ string) error {
		return errors.New("cilium: policy apply failed")
	}
	jobs := &fakeJobQueuer{
		claimFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
		fail:    func(_ context.Context, _, _ uuid.UUID, _ string) error { failCalled = true; return nil },
	}
	svc := newWithIfaces(&fakeTenantManager{}, jobs, &fakeAPIKeyManager{}).(*CFProvisionerService)

	svc.runProvisionWorkflow(uuid.New(), ProvisionParams{TenantSlug: "t1"})

	assert.True(t, failCalled)
}

// TestRunProvisionWorkflow_VClusterFails fails the job when Step 6 errors.
func TestRunProvisionWorkflow_VClusterFails(t *testing.T) {
	defer resetWorkflowSeams()
	failCalled := false
	createNamespaceFn = func(_ context.Context, _ string) error { return nil }
	applyIsolationPoliciesFn = func(_ context.Context, _ string) error { return nil }
	createVClusterFn = func(_ context.Context, _ provisioner.VClusterConfig) (*provisioner.VClusterResult, error) {
		return nil, errors.New("vcluster: helm install failed")
	}
	jobs := &fakeJobQueuer{
		claimFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
		fail:    func(_ context.Context, _, _ uuid.UUID, _ string) error { failCalled = true; return nil },
	}
	svc := newWithIfaces(&fakeTenantManager{}, jobs, &fakeAPIKeyManager{}).(*CFProvisionerService)

	svc.runProvisionWorkflow(uuid.New(), ProvisionParams{TenantSlug: "t1"})

	assert.True(t, failCalled)
}

// TestRunProvisionWorkflow_KubeconfigStoreFails fails the job when Step 7 errors.
func TestRunProvisionWorkflow_KubeconfigStoreFails(t *testing.T) {
	defer resetWorkflowSeams()
	failCalled := false
	createNamespaceFn = func(_ context.Context, _ string) error { return nil }
	applyIsolationPoliciesFn = func(_ context.Context, _ string) error { return nil }
	createVClusterFn = func(_ context.Context, _ provisioner.VClusterConfig) (*provisioner.VClusterResult, error) {
		return stubVClusterResult(), nil
	}
	storeKubeconfigFn = func(_ context.Context, _ *openbao.Client, _, _ string) error {
		return errors.New("openbao: permission denied")
	}
	jobs := &fakeJobQueuer{
		claimFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
		fail:    func(_ context.Context, _, _ uuid.UUID, _ string) error { failCalled = true; return nil },
	}
	svc := newWithIfaces(&fakeTenantManager{}, jobs, &fakeAPIKeyManager{}).(*CFProvisionerService)

	svc.runProvisionWorkflow(uuid.New(), ProvisionParams{TenantSlug: "t1"})

	assert.True(t, failCalled)
}

// TestRunProvisionWorkflow_GenerateAPIKeyFails fails the job when Step 8 errors.
func TestRunProvisionWorkflow_GenerateAPIKeyFails(t *testing.T) {
	defer resetWorkflowSeams()
	failCalled := false
	createNamespaceFn = func(_ context.Context, _ string) error { return nil }
	applyIsolationPoliciesFn = func(_ context.Context, _ string) error { return nil }
	createVClusterFn = func(_ context.Context, _ provisioner.VClusterConfig) (*provisioner.VClusterResult, error) {
		return stubVClusterResult(), nil
	}
	storeKubeconfigFn = func(_ context.Context, _ *openbao.Client, _, _ string) error { return nil }
	generateAPIKeyFn = func(_ context.Context, _ provisioner.APIKeyStorer, _ uuid.UUID, _, _ string) (*provisioner.GeneratedAPIKey, error) {
		return nil, errors.New("provisioner: key generation failed")
	}
	jobs := &fakeJobQueuer{
		claimFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
		fail:    func(_ context.Context, _, _ uuid.UUID, _ string) error { failCalled = true; return nil },
	}
	svc := newWithIfaces(&fakeTenantManager{}, jobs, &fakeAPIKeyManager{}).(*CFProvisionerService)

	svc.runProvisionWorkflow(uuid.New(), ProvisionParams{TenantSlug: "t1"})

	assert.True(t, failCalled)
}

// TestRunProvisionWorkflow_SetCIDRsFails fails the job when Step 9 errors.
func TestRunProvisionWorkflow_SetCIDRsFails(t *testing.T) {
	defer resetWorkflowSeams()
	allExternalSeamsSucceed()
	failCalled := false
	tenants := &fakeTenantManager{
		setCIDRsFn: func(_ context.Context, _ uuid.UUID, _, _ string) error {
			return errors.New("scylladb: write error")
		},
	}
	jobs := &fakeJobQueuer{
		claimFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
		fail:    func(_ context.Context, _, _ uuid.UUID, _ string) error { failCalled = true; return nil },
	}
	svc := newWithIfaces(tenants, jobs, &fakeAPIKeyManager{}).(*CFProvisionerService)

	svc.runProvisionWorkflow(uuid.New(), ProvisionParams{TenantSlug: "t1"})

	assert.True(t, failCalled)
}

// TestRunProvisionWorkflow_UpdateStatusFails fails the job when Step 10 errors.
func TestRunProvisionWorkflow_UpdateStatusFails(t *testing.T) {
	defer resetWorkflowSeams()
	allExternalSeamsSucceed()
	failCalled := false
	tenants := &fakeTenantManager{
		updateStatus: func(_ context.Context, _ uuid.UUID, _ accounts.TenantStatus) error {
			return errors.New("scylladb: write error")
		},
	}
	jobs := &fakeJobQueuer{
		claimFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
		fail:    func(_ context.Context, _, _ uuid.UUID, _ string) error { failCalled = true; return nil },
	}
	svc := newWithIfaces(tenants, jobs, &fakeAPIKeyManager{}).(*CFProvisionerService)

	svc.runProvisionWorkflow(uuid.New(), ProvisionParams{TenantSlug: "t1"})

	assert.True(t, failCalled)
}

// TestRunProvisionWorkflow_CompleteJobFails logs the error but does not panic
// when Step 11 (job.Complete) fails.
func TestRunProvisionWorkflow_CompleteJobFails(t *testing.T) {
	defer resetWorkflowSeams()
	allExternalSeamsSucceed()
	jobs := &fakeJobQueuer{
		claimFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
		complete: func(_ context.Context, _, _ uuid.UUID, _ string) error {
			return errors.New("scylladb: write error")
		},
	}
	svc := newWithIfaces(&fakeTenantManager{}, jobs, &fakeAPIKeyManager{}).(*CFProvisionerService)

	// Must not panic even if Complete fails.
	require.NotPanics(t, func() {
		svc.runProvisionWorkflow(uuid.New(), ProvisionParams{TenantSlug: "t1"})
	})
}

// TestRunProvisionWorkflow_HappyPath exercises the entire 10-step workflow with
// all external calls stubbed to succeed.
func TestRunProvisionWorkflow_HappyPath(t *testing.T) {
	defer resetWorkflowSeams()
	allExternalSeamsSucceed()

	completeCalled := false
	jobs := &fakeJobQueuer{
		claimFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
		complete: func(_ context.Context, _, _ uuid.UUID, _ string) error {
			completeCalled = true
			return nil
		},
	}
	svc := newWithIfaces(&fakeTenantManager{}, jobs, &fakeAPIKeyManager{}).(*CFProvisionerService)

	svc.runProvisionWorkflow(uuid.New(), ProvisionParams{
		TenantSlug: "acme",
	})

	assert.True(t, completeCalled, "job must be completed on success")
}

// ── runDeprovisionWorkflow ────────────────────────────────────────────────────

// TestRunDeprovisionWorkflow_ClaimError returns early when Claim itself errors.
func TestRunDeprovisionWorkflow_ClaimError(t *testing.T) {
	defer resetWorkflowSeams()
	revokeCalled := false
	revokeKubeconfigFn = func(_ context.Context, _ *openbao.Client, _ string) error {
		revokeCalled = true
		return nil
	}
	jobs := &fakeJobQueuer{
		claimFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) {
			return false, errors.New("lock error")
		},
	}
	svc := newWithIfaces(&fakeTenantManager{}, jobs, &fakeAPIKeyManager{}).(*CFProvisionerService)
	tenant := &accounts.Tenant{TenantID: uuid.New(), Slug: "acme"}

	svc.runDeprovisionWorkflow(uuid.New(), tenant)

	assert.False(t, revokeCalled)
}

// TestRunDeprovisionWorkflow_AlreadyClaimed returns when another replica holds
// the job.
func TestRunDeprovisionWorkflow_AlreadyClaimed(t *testing.T) {
	defer resetWorkflowSeams()
	revokeCalled := false
	revokeKubeconfigFn = func(_ context.Context, _ *openbao.Client, _ string) error {
		revokeCalled = true
		return nil
	}
	jobs := &fakeJobQueuer{
		claimFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return false, nil },
	}
	svc := newWithIfaces(&fakeTenantManager{}, jobs, &fakeAPIKeyManager{}).(*CFProvisionerService)
	tenant := &accounts.Tenant{TenantID: uuid.New(), Slug: "acme"}

	svc.runDeprovisionWorkflow(uuid.New(), tenant)

	assert.False(t, revokeCalled)
}

// TestRunDeprovisionWorkflow_RevokeFails fails the job when Step 1 errors.
func TestRunDeprovisionWorkflow_RevokeFails(t *testing.T) {
	defer resetWorkflowSeams()
	failCalled := false
	revokeKubeconfigFn = func(_ context.Context, _ *openbao.Client, _ string) error {
		return errors.New("openbao: permission denied")
	}
	jobs := &fakeJobQueuer{
		claimFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
		fail:    func(_ context.Context, _, _ uuid.UUID, _ string) error { failCalled = true; return nil },
	}
	svc := newWithIfaces(&fakeTenantManager{}, jobs, &fakeAPIKeyManager{}).(*CFProvisionerService)
	tenant := &accounts.Tenant{TenantID: uuid.New(), Slug: "acme"}

	svc.runDeprovisionWorkflow(uuid.New(), tenant)

	assert.True(t, failCalled)
}

// TestRunDeprovisionWorkflow_DeleteVClusterFails fails the job when Step 2 errors.
func TestRunDeprovisionWorkflow_DeleteVClusterFails(t *testing.T) {
	defer resetWorkflowSeams()
	failCalled := false
	revokeKubeconfigFn = func(_ context.Context, _ *openbao.Client, _ string) error { return nil }
	deleteVClusterFn = func(_ context.Context, _, _ string) error {
		return errors.New("vcluster: delete failed")
	}
	jobs := &fakeJobQueuer{
		claimFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
		fail:    func(_ context.Context, _, _ uuid.UUID, _ string) error { failCalled = true; return nil },
	}
	svc := newWithIfaces(&fakeTenantManager{}, jobs, &fakeAPIKeyManager{}).(*CFProvisionerService)
	tenant := &accounts.Tenant{TenantID: uuid.New(), Slug: "acme"}

	svc.runDeprovisionWorkflow(uuid.New(), tenant)

	assert.True(t, failCalled)
}

// TestRunDeprovisionWorkflow_UpdateStatusFails fails the job when Step 3 errors.
func TestRunDeprovisionWorkflow_UpdateStatusFails(t *testing.T) {
	defer resetWorkflowSeams()
	failCalled := false
	revokeKubeconfigFn = func(_ context.Context, _ *openbao.Client, _ string) error { return nil }
	deleteVClusterFn = func(_ context.Context, _, _ string) error { return nil }
	tenants := &fakeTenantManager{
		updateStatus: func(_ context.Context, _ uuid.UUID, _ accounts.TenantStatus) error {
			return errors.New("scylladb: write error")
		},
	}
	jobs := &fakeJobQueuer{
		claimFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
		fail:    func(_ context.Context, _, _ uuid.UUID, _ string) error { failCalled = true; return nil },
	}
	svc := newWithIfaces(tenants, jobs, &fakeAPIKeyManager{}).(*CFProvisionerService)
	tenant := &accounts.Tenant{TenantID: uuid.New(), Slug: "acme"}

	svc.runDeprovisionWorkflow(uuid.New(), tenant)

	assert.True(t, failCalled)
}

// TestRunDeprovisionWorkflow_CompleteJobFails does not panic when Complete fails.
func TestRunDeprovisionWorkflow_CompleteJobFails(t *testing.T) {
	defer resetWorkflowSeams()
	revokeKubeconfigFn = func(_ context.Context, _ *openbao.Client, _ string) error { return nil }
	deleteVClusterFn = func(_ context.Context, _, _ string) error { return nil }
	jobs := &fakeJobQueuer{
		claimFn:  func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
		complete: func(_ context.Context, _, _ uuid.UUID, _ string) error { return errors.New("db error") },
	}
	svc := newWithIfaces(&fakeTenantManager{}, jobs, &fakeAPIKeyManager{}).(*CFProvisionerService)
	tenant := &accounts.Tenant{TenantID: uuid.New(), Slug: "acme"}

	require.NotPanics(t, func() {
		svc.runDeprovisionWorkflow(uuid.New(), tenant)
	})
}

// TestRunDeprovisionWorkflow_HappyPath exercises the full 5-step teardown
// with all external calls stubbed to succeed.
func TestRunDeprovisionWorkflow_HappyPath(t *testing.T) {
	defer resetWorkflowSeams()
	revokeKubeconfigFn = func(_ context.Context, _ *openbao.Client, _ string) error { return nil }
	deleteVClusterFn = func(_ context.Context, _, _ string) error { return nil }

	completeCalled := false
	jobs := &fakeJobQueuer{
		claimFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
		complete: func(_ context.Context, _, _ uuid.UUID, _ string) error {
			completeCalled = true
			return nil
		},
	}
	svc := newWithIfaces(&fakeTenantManager{}, jobs, &fakeAPIKeyManager{}).(*CFProvisionerService)
	tenant := &accounts.Tenant{TenantID: uuid.New(), Slug: "acme"}

	svc.runDeprovisionWorkflow(uuid.New(), tenant)

	assert.True(t, completeCalled)
}

// ── applyIsolationPolicies ────────────────────────────────────────────────────

// TestApplyIsolationPolicies_Success verifies that both policies are applied
// when the kubectl apply seam succeeds.
func TestApplyIsolationPolicies_Success(t *testing.T) {
	defer resetWorkflowSeams()
	applyCalls := 0
	kubectlApplyBytesFn = func(_ context.Context, _ []byte) error {
		applyCalls++
		return nil
	}

	err := applyIsolationPolicies(context.Background(), "tenant-acme")

	require.NoError(t, err)
	assert.Equal(t, 2, applyCalls, "both isolation and access policies must be applied")
}

// TestApplyIsolationPolicies_TenantIsolationApplyFails propagates the error
// when applying the first policy fails.
func TestApplyIsolationPolicies_TenantIsolationApplyFails(t *testing.T) {
	defer resetWorkflowSeams()
	applyCalls := 0
	kubectlApplyBytesFn = func(_ context.Context, _ []byte) error {
		applyCalls++
		return errors.New("kubectl: apply failed")
	}

	err := applyIsolationPolicies(context.Background(), "tenant-acme")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "apply tenant-isolation")
	assert.Equal(t, 1, applyCalls, "must stop after the first failure")
}

// TestApplyIsolationPolicies_ProvisionerAccessApplyFails propagates the error
// when applying the second policy fails.
func TestApplyIsolationPolicies_ProvisionerAccessApplyFails(t *testing.T) {
	defer resetWorkflowSeams()
	callCount := 0
	kubectlApplyBytesFn = func(_ context.Context, _ []byte) error {
		callCount++
		if callCount == 2 {
			return errors.New("kubectl: access policy apply failed")
		}
		return nil
	}

	err := applyIsolationPolicies(context.Background(), "tenant-acme")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "apply provisioner-access")
}

// ── ProvisionParams is now just TenantSlug ───────────────────────────────────

// TestProvisionParams_HasTenantSlug confirms the ProvisionParams type contains
// the TenantSlug field and is usable as expected.
func TestProvisionParams_HasTenantSlug(_ *testing.T) {
	p := ProvisionParams{TenantSlug: "acme"}
	_ = p.TenantSlug
}

// ── timing guard for goroutine tests ─────────────────────────────────────────

// TestProvision_WorkflowRunsAsynchronously confirms that Provision() returns
// before the workflow goroutine finishes (it runs in the background).
func TestProvision_WorkflowRunsAsynchronously(t *testing.T) {
	defer resetWorkflowSeams()

	// started closes when the goroutine enters createNamespaceFn; workflowDone
	// closes when the goroutine reaches its terminal step (Complete or Fail).
	// Waiting for workflowDone before returning prevents a data race: without it
	// the goroutine would still be reading seam variables concurrently with the
	// next test's resetWorkflowSeams() write.
	started := make(chan struct{})
	workflowDone := make(chan struct{})

	// Block the background goroutine for 100 ms so the Provision() call can
	// return and we can assert elapsed < 50 ms.
	createNamespaceFn = func(_ context.Context, _ string) error {
		close(started)
		time.Sleep(100 * time.Millisecond)
		return nil
	}
	applyIsolationPoliciesFn = func(_ context.Context, _ string) error { return nil }
	createVClusterFn = func(_ context.Context, _ provisioner.VClusterConfig) (*provisioner.VClusterResult, error) {
		return stubVClusterResult(), nil
	}
	storeKubeconfigFn = func(_ context.Context, _ *openbao.Client, _, _ string) error { return nil }
	generateAPIKeyFn = func(_ context.Context, _ provisioner.APIKeyStorer, _ uuid.UUID, _, _ string) (*provisioner.GeneratedAPIKey, error) {
		return stubGeneratedKey(), nil
	}

	wantJob := uuid.New()
	closeOnce := sync.Once{}
	jobs := &fakeJobQueuer{
		enqueueFn: func(_ context.Context, _ uuid.UUID, _ string, _ accounts.JobOperation) (uuid.UUID, error) {
			return wantJob, nil
		},
		claimFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
		complete: func(_ context.Context, _, _ uuid.UUID, _ string) error {
			closeOnce.Do(func() { close(workflowDone) })
			return nil
		},
		fail: func(_ context.Context, _, _ uuid.UUID, _ string) error {
			closeOnce.Do(func() { close(workflowDone) })
			return nil
		},
	}
	svc := newWithIfaces(&fakeTenantManager{}, jobs, &fakeAPIKeyManager{})

	before := time.Now()
	jobID, err := svc.Provision(context.Background(), ProvisionParams{TenantSlug: "t1"})
	elapsed := time.Since(before)

	require.NoError(t, err)
	assert.Equal(t, wantJob, jobID)
	assert.Less(t, elapsed, 50*time.Millisecond, "Provision must return before the workflow completes")

	// First confirm the goroutine actually started.
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("goroutine did not start in time")
	}

	// Then wait for the workflow to finish so the seam variables are not read
	// concurrently with the next test's resetWorkflowSeams() write.
	select {
	case <-workflowDone:
	case <-time.After(5 * time.Second):
		t.Log("workflow goroutine did not complete in time (acceptable in CI)")
	}
}

// ── applyIsolationPolicies — policy-render error branches ────────────────────

// TestApplyIsolationPolicies_TenantIsolationPolicyFails covers the error branch
// at the first policy render call (line 286 in service.go).
func TestApplyIsolationPolicies_TenantIsolationPolicyFails(t *testing.T) {
	defer resetWorkflowSeams()
	renderErr := errors.New("template: isolation render failed")
	tenantIsolationPolicyFn = func(_ string) ([]byte, error) { return nil, renderErr }

	err := applyIsolationPolicies(context.Background(), "tenant-acme")

	require.ErrorIs(t, err, renderErr)
}

// TestApplyIsolationPolicies_ProvisionerAccessPolicyFails covers the error branch
// at the second policy render call (line 294 in service.go): the first render and
// its kubectl apply both succeed; only the access-policy render is injected to fail.
func TestApplyIsolationPolicies_ProvisionerAccessPolicyFails(t *testing.T) {
	defer resetWorkflowSeams()
	kubectlApplyBytesFn = func(_ context.Context, _ []byte) error { return nil }
	renderErr := errors.New("template: access render failed")
	provisionerAccessPolicyFn = func(_ string) ([]byte, error) { return nil, renderErr }

	err := applyIsolationPolicies(context.Background(), "tenant-acme")

	require.ErrorIs(t, err, renderErr)
}

// ── createNamespace — kubectl error branches ──────────────────────────────────

// TestCreateNamespace_KubectlDryRunFails covers the kubectl dry-run error branch
// inside createNamespace. An empty namespace string causes kubectl to exit with a
// non-zero status ("name must be specified"), exercising the return-on-error path.
func TestCreateNamespace_KubectlDryRunFails(t *testing.T) {
	defer resetWorkflowSeams()

	// An empty namespace is rejected by kubectl client-side (no cluster needed).
	err := createNamespace(context.Background(), "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "kubectl dry-run namespace")
}

// TestCreateNamespace_DryRunSucceeds_ApplySeamCalled covers the success path of
// the kubectl dry-run step and verifies that the produced manifest is forwarded to
// kubectlApplyBytesFn. The apply seam is stubbed so no live cluster is required.
func TestCreateNamespace_DryRunSucceeds_ApplySeamCalled(t *testing.T) {
	defer resetWorkflowSeams()
	kubectlApplyBytesFn = func(_ context.Context, _ []byte) error { return nil }

	// A valid lowercase name passes kubectl client-side dry-run without a cluster.
	err := createNamespace(context.Background(), "tenant-test")

	require.NoError(t, err)
}

// ── kubectlApplyBytes — kubectl error branch ──────────────────────────────────

// TestKubectlApplyBytes_KubectlFails covers the error branch inside
// kubectlApplyBytes. kubectl apply requires a live API server; running it without
// one causes it to exit with a non-zero status, exercising the wrapped-error path.
func TestKubectlApplyBytes_KubectlFails(t *testing.T) {
	defer resetWorkflowSeams()
	manifest := []byte("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: tenant-test\n")

	// kubectl apply -f - will fail because there is no reachable API server in
	// the unit-test environment. That covers the cmd.Run() error branch.
	err := kubectlApplyBytes(context.Background(), manifest)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "kubectl apply")
}
