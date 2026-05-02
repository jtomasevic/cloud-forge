package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
	openbao "github.com/openbao/openbao/api/v2"

	"github.com/jtomasevic/cloud-forge/internal/accounts"
	"github.com/jtomasevic/cloud-forge/internal/provisioner"
)

// CFProvisionerService is the concrete implementation of ProvisionerService.
// It is unexported and only accessible through the ProvisionerService interface
// returned by New().
// Fields are stored as local interfaces (tenantManager, jobQueuer, apiKeyManager)
// so that tests can inject mocks without needing a live ScyllaDB / OpenBao.
type CFProvisionerService struct {
	tenants tenantManager
	jobs    jobQueuer
	keys    apiKeyManager
	bao     *openbao.Client
	log     *slog.Logger
}

// Workflow step seams — replaced in tests to avoid kubectl, OpenBao, and
// vCluster CLI dependencies.  Production code uses the real functions.
//
//nolint:gochecknoglobals // test seams; deliberate package-level mutability
var (
	createNamespaceFn        = createNamespace
	applyIsolationPoliciesFn = applyIsolationPolicies
	createVClusterFn         = provisioner.CreateVCluster
	storeKubeconfigFn        = provisioner.Store
	generateAPIKeyFn         = provisioner.GenerateAPIKey
	revokeKubeconfigFn       = provisioner.Revoke
	deleteVClusterFn         = provisioner.DeleteVCluster
	kubectlApplyBytesFn      = kubectlApplyBytes

	// Policy rendering seams — replaced in tests to exercise all error branches
	// of applyIsolationPolicies without needing a namespace that fails in one
	// template but not the other.
	tenantIsolationPolicyFn   = provisioner.TenantIsolationPolicy
	provisionerAccessPolicyFn = provisioner.ProvisionerAccessPolicy
)

// ── ProvisionerService implementation ────────────────────────────────────────

// Provision enqueues the VPC provisioning job and launches the background
// workflow. Returns the job ID immediately; callers must poll GetJob.
func (s *CFProvisionerService) Provision(ctx context.Context, p ProvisionParams) (uuid.UUID, error) {
	idemKey := "provision-vpc:" + p.TenantSlug
	jobID, err := s.jobs.Enqueue(ctx, uuid.Nil, idemKey, accounts.JobOperationProvisionVPC)
	if err != nil {
		return uuid.Nil, fmt.Errorf("enqueue provision job: %w", err)
	}
	go s.runProvisionWorkflow(jobID, p) //nolint:gosec,contextcheck // goroutine owns its own lifecycle context, not the request context
	return jobID, nil
}

// GetJob returns the current state of any provisioning or deprovisioning job.
func (s *CFProvisionerService) GetJob(ctx context.Context, jobID uuid.UUID) (JobResult, error) {
	// uuid.Nil is used as the tenant_id partition key for provision-vpc jobs
	// in this VPC slice (provisioning runs before the tenant record exists).
	job, err := s.jobs.Get(ctx, uuid.Nil, jobID)
	if errors.Is(err, accounts.ErrJobNotFound) {
		return JobResult{}, ErrJobNotFound
	}
	if err != nil {
		return JobResult{}, fmt.Errorf("get job: %w", err)
	}
	return ToJobResultFromAccountsJob(job), nil
}

// Deprovision looks up the tenant by slug, enqueues a deprovision job, and
// launches the teardown workflow in the background.
func (s *CFProvisionerService) Deprovision(ctx context.Context, p DeprovisionParams) (uuid.UUID, error) {
	tenant, err := s.tenants.GetBySlug(ctx, p.TenantSlug)
	if errors.Is(err, accounts.ErrTenantNotFound) {
		return uuid.Nil, ErrTenantNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("lookup tenant for deprovision: %w", err)
	}

	idemKey := "deprovision-vpc:" + p.TenantSlug + ":" + time.Now().Format("2006-01-02")
	jobID, err := s.jobs.Enqueue(ctx, tenant.TenantID, idemKey, accounts.JobOperationDeprovisionVPC)
	if err != nil {
		return uuid.Nil, fmt.Errorf("enqueue deprovision job: %w", err)
	}
	go s.runDeprovisionWorkflow(jobID, tenant) //nolint:gosec,contextcheck // goroutine owns its own lifecycle context, not the request context
	return jobID, nil
}

// ── Provisioning workflow ─────────────────────────────────────────────────────

// runProvisionWorkflow executes the 10-step VPC provisioning sequence
// in a background goroutine. The HTTP handler has already returned 202.
//
// Steps are documented in docs/CF-VPC-Service-Proposal.md.
// Any step failure writes FAILED to the job record and returns.
func (s *CFProvisionerService) runProvisionWorkflow(jobID uuid.UUID, p ProvisionParams) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	log := s.logger().With("job_id", jobID, "tenant", p.TenantSlug)

	// Step 1: claim the job with an LWT so only one replica runs the workflow.
	claimed, err := s.jobs.Claim(ctx, uuid.Nil, jobID)
	if err != nil {
		log.Error("claim job", "err", err)
		return
	}
	if !claimed {
		log.Info("job already claimed by another replica")
		return
	}

	// Step 2: look up the existing tenant record (created during registration).
	tenant, err := s.tenants.GetBySlug(ctx, p.TenantSlug)
	if errors.Is(err, accounts.ErrTenantNotFound) {
		_ = s.jobs.Fail(ctx, uuid.Nil, jobID, "tenant not found: "+p.TenantSlug)
		return
	}
	if err != nil {
		s.failJob(ctx, log, uuid.Nil, jobID, "get tenant record", err)
		return
	}

	// Step 3: CIDR allocation.
	// A real deployment wires a *gocql.Session into the service and calls
	// provisioner.AllocateCIDRs. In this VPC slice a fixed pair is used to
	// avoid passing the session through the service constructor explicitly.
	// TODO: inject CIDRAllocationDB and call provisioner.AllocateCIDRs.
	cidrPair := provisioner.CIDRPair{PodCIDR: "10.100.1.0/24", SvcCIDR: "10.200.1.0/24"}

	// Step 4: create host namespace.
	hostNamespace := "tenant-" + p.TenantSlug
	if err := createNamespaceFn(ctx, hostNamespace); err != nil { //nolint:govet // idiomatic if-err pattern; scoped err does not escape the block
		s.failJob(ctx, log, tenant.TenantID, jobID, "create namespace", err)
		return
	}

	// Step 5: apply Cilium isolation policies.
	if err := applyIsolationPoliciesFn(ctx, hostNamespace); err != nil { //nolint:govet // idiomatic if-err pattern; scoped err does not escape the block
		s.failJob(ctx, log, tenant.TenantID, jobID, "apply Cilium policies", err)
		return
	}

	// Step 6: create vCluster.
	vclusterResult, err := createVClusterFn(ctx, provisioner.VClusterConfig{
		TenantID:      p.TenantSlug,
		HostNamespace: hostNamespace,
		PodCIDR:       cidrPair.PodCIDR,
		SvcCIDR:       cidrPair.SvcCIDR,
	})
	if err != nil {
		s.failJob(ctx, log, tenant.TenantID, jobID, "create vCluster", err)
		return
	}

	// Step 7: store kubeconfig in OpenBao.
	if err := storeKubeconfigFn(ctx, s.bao, p.TenantSlug, vclusterResult.KubeconfigYAML); err != nil { //nolint:govet // idiomatic if-err pattern; scoped err does not escape the block
		s.failJob(ctx, log, tenant.TenantID, jobID, "store kubeconfig", err)
		return
	}

	// Step 8: generate API key (raw key returned once; only the hash is persisted).
	generated, err := generateAPIKeyFn(
		ctx, s.keys, tenant.TenantID,
		fmt.Sprintf("%s default key", tenant.DisplayName),
		"provision:write,provision:read",
	)
	if err != nil {
		s.failJob(ctx, log, tenant.TenantID, jobID, "generate api key", err)
		return
	}

	// Step 9: write CIDRs and set tenant status to ACTIVE.
	if err := s.tenants.SetCIDRs(ctx, tenant.TenantID, cidrPair.PodCIDR, cidrPair.SvcCIDR); err != nil {
		s.failJob(ctx, log, tenant.TenantID, jobID, "set CIDRs", err)
		return
	}
	if err := s.tenants.UpdateStatus(ctx, tenant.TenantID, accounts.TenantStatusActive); err != nil {
		s.failJob(ctx, log, tenant.TenantID, jobID, "set tenant ACTIVE", err)
		return
	}

	// Step 10: mark job READY with the result payload.
	// The raw API key is included here so the first GET /jobs/{id} can return
	// it to the caller. It must not be stored beyond this blob.
	resultJSON, _ := json.Marshal(map[string]any{
		"api_key":    generated.RawKey,
		"api_key_id": generated.Record.KeyID,
		"key_hash":   generated.KeyHash,
		"vpc_info": map[string]any{
			"pod_cidr":       cidrPair.PodCIDR,
			"service_cidr":   cidrPair.SvcCIDR,
			"vcluster_ready": true,
		},
	})
	if err := s.jobs.Complete(ctx, tenant.TenantID, jobID, string(resultJSON)); err != nil {
		log.Error("mark job complete", "err", err)
	}
	log.Info("tenant provisioned")
}

// ── Deprovisioning workflow ───────────────────────────────────────────────────

// runDeprovisionWorkflow executes the 5-step teardown sequence.
func (s *CFProvisionerService) runDeprovisionWorkflow(jobID uuid.UUID, tenant *accounts.Tenant) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	log := s.logger().With("job_id", jobID, "tenant", tenant.Slug)

	claimed, err := s.jobs.Claim(ctx, tenant.TenantID, jobID)
	if err != nil || !claimed {
		return
	}

	hostNamespace := "tenant-" + tenant.Slug

	// Step 1: revoke kubeconfig from OpenBao (idempotent).
	if err := revokeKubeconfigFn(ctx, s.bao, tenant.Slug); err != nil {
		s.failJob(ctx, log, tenant.TenantID, jobID, "revoke kubeconfig", err)
		return
	}

	// Step 2: delete vCluster and its host namespace (idempotent).
	if err := deleteVClusterFn(ctx, tenant.Slug, hostNamespace); err != nil {
		s.failJob(ctx, log, tenant.TenantID, jobID, "delete vCluster", err)
		return
	}

	// Step 3: mark tenant DELETED.
	if err := s.tenants.UpdateStatus(ctx, tenant.TenantID, accounts.TenantStatusDeleted); err != nil {
		s.failJob(ctx, log, tenant.TenantID, jobID, "set tenant DELETED", err)
		return
	}

	if err := s.jobs.Complete(ctx, tenant.TenantID, jobID, `{"deprovisioned":true}`); err != nil {
		log.Error("mark deprovision job complete", "err", err)
	}
	log.Info("tenant deprovisioned")
}

// ── Private helpers ───────────────────────────────────────────────────────────

// failJob records a FAILED job state and logs the error.
func (s *CFProvisionerService) failJob(ctx context.Context, log *slog.Logger, tenantID, jobID uuid.UUID, step string, err error) {
	log.Error("provisioning step failed", "step", step, "err", err)
	if ferr := s.jobs.Fail(ctx, tenantID, jobID, step+": "+err.Error()); ferr != nil {
		log.Error("write job failure", "err", ferr)
	}
}

// logger returns the service logger, falling back to the default handler if
// no logger was injected (simplifies tests that don't care about log output).
func (s *CFProvisionerService) logger() *slog.Logger {
	if s.log != nil {
		return s.log
	}
	return slog.Default()
}

// createNamespace creates a Kubernetes namespace idempotently using
// kubectl apply with a dry-run-generated manifest.
func createNamespace(ctx context.Context, namespace string) error {
	cmd := exec.CommandContext(ctx, "kubectl", "create", "namespace", namespace, "--dry-run=client", "-o", "yaml") //nolint:gosec // namespace is validated by the caller; args are literal strings
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl dry-run namespace: %w", err)
	}
	return kubectlApplyBytesFn(ctx, stdout.Bytes())
}

// applyIsolationPolicies renders and applies the two Cilium policies for a
// tenant namespace: TenantIsolationPolicy (default-deny) and
// ProvisionerAccessPolicy (cf-system → vCluster port 6443 only).
func applyIsolationPolicies(ctx context.Context, namespace string) error {
	isolation, err := tenantIsolationPolicyFn(namespace)
	if err != nil {
		return err
	}
	if err := kubectlApplyBytesFn(ctx, isolation); err != nil { //nolint:govet // idiomatic if-err pattern; scoped err does not escape the block
		return fmt.Errorf("apply tenant-isolation: %w", err)
	}

	access, err := provisionerAccessPolicyFn(namespace)
	if err != nil {
		return err
	}
	if err := kubectlApplyBytesFn(ctx, access); err != nil {
		return fmt.Errorf("apply provisioner-access: %w", err)
	}
	return nil
}

// kubectlApplyBytes pipes manifest YAML to `kubectl apply -f -`.
func kubectlApplyBytes(ctx context.Context, manifest []byte) error {
	cmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", "-")
	cmd.Stdin = bytes.NewReader(manifest)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl apply: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
