MODULE  := github.com/jtomasevic/cloud-forge
REGISTRY := ghcr.io/jtomasevic/cloud-forge

# Cilium CNI — pinned version, must match spikes/cilium-enforcement validation.
CILIUM_VERSION ?= 1.17.3
# hubble CLI must match CILIUM_VERSION to avoid "invalid fieldmask" errors.
# Install: brew install hubble  (then verify: hubble version vs cilium version --client)
HUBBLE_VERSION ?= 1.17.3

SERVICES := cf cf-install cf-iam cf-secrets cf-resource cf-events cf-functions cf-db cf-gateway cf-observe cf-ai
API_SERVICES := ai database events functions gateway iam observability resource secrets storage

# Default target
.DEFAULT_GOAL := help

# ── Help ─────────────────────────────────────────────────────────────────────

.PHONY: help
help: ## Show this help message
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} \
	  /^[a-zA-Z_\/-]+:.*?##/ { printf "  \033[36m%-28s\033[0m %s\n", $$1, $$2 } \
	  /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

# ── Dev cluster lifecycle ─────────────────────────────────────────────────────

##@ Dev Cluster

.PHONY: tools-check
tools-check: ## Verify required dev tools are installed; install missing ones via Homebrew (macOS)
	@bash scripts/tools-check.sh

.PHONY: dev-up
dev-up: tools-check ## Create k3d cluster, install Cilium CNI, bootstrap infrastructure, deploy ScyllaDB and OpenBao
	k3d cluster create --config deploy/k3d/cluster.yaml
	k3d kubeconfig merge cloudforge-dev --kubeconfig-merge-default
	$(MAKE) install-cilium
	kubectl apply -k deploy/kustomize/base/
	bash scripts/dev-bootstrap.sh
	$(MAKE) deploy-cert-manager
	$(MAKE) deploy-scylladb
	$(MAKE) deploy-openbao
	@echo ""
	@echo "╔══════════════════════════════════════════════════════════════════════╗"
	@echo "║  CloudForge dev cluster is ready                                     ║"
	@echo "║                                                                      ║"
	@echo "║  Services:                                                           ║"
	@echo "║    Cilium CNI     + Hubble Relay  →  make cilium-status             ║"
	@echo "║    ScyllaDB       (cf-data)       →  make scylladb-status           ║"
	@echo "║    OpenBao        (cf-security)   →  make openbao-status            ║"
	@echo "║                                                                      ║"
	@echo "║  ⚠  OpenBao is running in DEV MODE:                                 ║"
	@echo "║       • In-memory storage  (secrets lost on pod restart)            ║"
	@echo "║       • Root token         (not Kubernetes auth)                    ║"
	@echo "║       • No TLS             (plaintext HTTP within cluster)          ║"
	@echo "║     This is intentional for local development. Production uses      ║"
	@echo "║     auto-unseal, persistent storage, mTLS, and Kubernetes auth.     ║"
	@echo "║                                                                      ║"
	@echo "║  Quick access:                                                       ║"
	@echo "║    make openbao-port-forward   →  http://localhost:8200              ║"
	@echo "║    token: dev-root-token                                             ║"
	@echo "╚══════════════════════════════════════════════════════════════════════╝"

.PHONY: install-cilium
install-cilium: ## Install Cilium CNI + Hubble Relay on the active cluster and wait for readiness
	@echo "── Installing Cilium $(CILIUM_VERSION) ─────────────────────────────────────────"
	cilium install --version $(CILIUM_VERSION)
	@echo "── Waiting for Cilium to be ready (up to 6 minutes) ───────────────"
	@echo "   Note: k3d boot time for 3 nodes is ~4.5 min (documented in spikes/cilium-enforcement/FINDINGS.md)"
	cilium status --wait --wait-duration 360s
	@echo "── Enabling Hubble Relay (flow logs + policy decision observability) "
	cilium hubble enable --relay
	@echo "✓ Cilium $(CILIUM_VERSION) ready — Hubble Relay enabled"

.PHONY: cilium-status
cilium-status: ## Show Cilium CNI health, Hubble Relay status, and active CiliumNetworkPolicies
	@echo "── Cilium status ───────────────────────────────────────────────────"
	cilium status
	@echo ""
	@echo "── CiliumNetworkPolicies ───────────────────────────────────────────"
	kubectl get cnp -A 2>/dev/null || echo "(no CiliumNetworkPolicies found — is Cilium installed?)"
	@echo ""
	@echo "── Hubble Relay pod ────────────────────────────────────────────────"
	kubectl get pod -n kube-system -l k8s-app=hubble-relay 2>/dev/null || echo "(Hubble Relay not deployed)"

##@ Hubble Observability

.PHONY: hubble-enable
hubble-enable: ## Enable Hubble Relay on a running cluster (idempotent; use if Relay was not enabled at install time)
	@echo "── Enabling Hubble Relay ────────────────────────────────────────────"
	cilium hubble enable --relay
	@echo "✓ Hubble Relay enabled — run 'make hubble-port-forward' to start observing"

.PHONY: hubble-port-forward
hubble-port-forward: ## Start Hubble port-forward (keep this terminal open; use hubble-observe in another)
	@echo "── Starting Hubble port-forward on localhost:4245 ──────────────────"
	@echo "   Keep this terminal open. Run 'make hubble-observe' in another terminal."
	@kubectl get service hubble-relay -n kube-system &>/dev/null \
	  || (echo "ERROR: Hubble Relay is not deployed. Run 'make hubble-enable' first." && exit 1)
	cilium hubble port-forward

.PHONY: hubble-observe
hubble-observe: ## Stream all live network flows (requires hubble-port-forward running; Ctrl-C to stop)
	@command -v hubble >/dev/null 2>&1 || (echo "ERROR: hubble CLI not found — run: brew install hubble" && exit 1)
	@echo "── Streaming all network flows (Ctrl-C to stop) ────────────────────"
	hubble observe --follow

.PHONY: hubble-observe-dropped
hubble-observe-dropped: ## Stream only DROPPED flows across all namespaces (isolation violations)
	@command -v hubble >/dev/null 2>&1 || (echo "ERROR: hubble CLI not found — run: brew install hubble" && exit 1)
	@echo "── Streaming DROPPED flows only (Ctrl-C to stop) ───────────────────"
	@echo "   (filtering via grep to avoid CLI/Relay version fieldmask mismatch)"
	hubble observe --follow 2>/dev/null | grep -i "DROPPED" || true

.PHONY: hubble-observe-tenant
hubble-observe-tenant: ## Show last 200 flows for a tenant namespace: make hubble-observe-tenant NS=cilium-tenant-a
	@command -v hubble >/dev/null 2>&1 || (echo "ERROR: hubble CLI not found — run: brew install hubble" && exit 1)
	@test -n "$(NS)" || (echo "ERROR: NS is required. Usage: make hubble-observe-tenant NS=cilium-tenant-a" && exit 1)
	hubble observe --namespace $(NS) --last 200

.PHONY: hubble-ui
hubble-ui: ## Enable Hubble UI and open it in the browser (deploys UI pod if not present)
	@echo "── Enabling Hubble UI ──────────────────────────────────────────────"
	cilium hubble enable --ui
	@echo "── Opening Hubble UI in browser ────────────────────────────────────"
	cilium hubble ui

.PHONY: dev-down
dev-down: ## Stop and delete local k3d cluster
	k3d cluster delete cloudforge-dev

.PHONY: dev-reset
dev-reset: dev-down dev-up ## Destroy and recreate local cluster from scratch

# ── OpenBao ───────────────────────────────────────────────────────────────────

##@ OpenBao (Secrets Management)

.PHONY: deploy-openbao
deploy-openbao: ## Deploy OpenBao in DEV MODE to cf-security namespace (in-memory, root token, no TLS)
	@echo "── Deploying OpenBao (DEV MODE) ────────────────────────────────────"
	@echo "   ⚠  DEV MODE: in-memory storage, root token, no TLS, single replica"
	@echo "   ⚠  All secrets are lost on pod restart"
	@echo "   ⚠  DO NOT use this configuration in staging or production"
	kubectl apply -f deploy/kustomize/base/openbao.yaml
	@echo "── Waiting for OpenBao pod to become ready (up to 60s) ─────────────"
	kubectl rollout status deployment/openbao -n cf-security --timeout=60s
	@echo ""
	@echo "✓ OpenBao ready (DEV MODE)"
	@echo "  Cluster address : http://openbao.cf-security.svc.cluster.local:8200"
	@echo "  Root token      : dev-root-token  (from secret openbao-dev-token)"
	@echo "  Port-forward    : make openbao-port-forward"
	@echo "  Local address   : http://localhost:8200  (after port-forward)"

.PHONY: openbao-status
openbao-status: ## Show OpenBao pod status, health, and mounted secret engines
	@echo "── OpenBao pod ─────────────────────────────────────────────────────"
	kubectl get pod -n cf-security -l app.kubernetes.io/name=openbao -o wide 2>/dev/null \
		|| echo "  (no OpenBao pod found — run: make deploy-openbao)"
	@echo ""
	@echo "── OpenBao health (requires port-forward on :8200) ─────────────────"
	@curl -sf http://localhost:8200/v1/sys/health 2>/dev/null \
		| python3 -m json.tool 2>/dev/null \
		|| echo "  (OpenBao not reachable — run: make openbao-port-forward in another terminal)"
	@echo ""
	@echo "── CiliumNetworkPolicy: secrets-isolation ──────────────────────────"
	kubectl get cnp secrets-isolation -n cf-security -o yaml 2>/dev/null \
		| grep -A 20 "spec:" \
		|| echo "  (CNP not found — is Cilium installed?)"

.PHONY: openbao-port-forward
openbao-port-forward: ## Forward OpenBao API to localhost:8200 (keep this terminal open)
	@echo "── Forwarding OpenBao API → localhost:8200 ──────────────────────────"
	@echo "   ⚠  DEV MODE — root token: dev-root-token"
	@echo "   Set in your shell:"
	@echo "     export VAULT_ADDR=http://localhost:8200"
	@echo "     export VAULT_TOKEN=dev-root-token"
	@echo "   Or for the bao CLI:"
	@echo "     export BAO_ADDR=http://localhost:8200"
	@echo "     export BAO_TOKEN=dev-root-token"
	@echo "   Press Ctrl-C to stop."
	@kubectl get pod -n cf-security -l app.kubernetes.io/name=openbao --no-headers 2>/dev/null | grep -q Running \
		|| (echo "ERROR: OpenBao pod is not running — run: make deploy-openbao" && exit 1)
	kubectl port-forward -n cf-security svc/openbao 8200:8200

.PHONY: deploy-knative
deploy-knative: ## Install Knative Serving + net-kourier on the dev cluster
	$(MAKE) -C spikes/knative-coldstart deploy-knative

.PHONY: measure-coldstart
measure-coldstart: ## Run scale-to-zero cold-start benchmark (requires deploy-knative first)
	$(MAKE) -C spikes/knative-coldstart measure


dev-status: ## Show cluster node and pod status
	k3d cluster list
	kubectl get pods -A

# ── ScyllaDB ──────────────────────────────────────────────────────────────────

##@ ScyllaDB

SCYLLA_OPERATOR_VERSION ?= 1.15.0

.PHONY: deploy-scylladb
deploy-scylladb: ## Install Scylla Operator (Helm) and provision the dev ScyllaDB cluster
	@echo "── Adding ScyllaDB Helm repo ────────────────────────────────────────"
	helm repo add scylladb https://storage.googleapis.com/scylla-operator-charts/stable --force-update
	helm repo update scylladb
	@echo "── Installing / upgrading Scylla Operator v$(SCYLLA_OPERATOR_VERSION) ──"
	helm upgrade --install scylla-operator scylladb/scylla-operator \
		--namespace scylla-operator \
		--create-namespace \
		--version $(SCYLLA_OPERATOR_VERSION) \
		-f deploy/helm/components/scylla-operator/values.yaml \
		--wait
	@echo "── Applying ScyllaDB cluster and schema resources ───────────────────"
	kubectl apply -k deploy/kustomize/components/scylladb/
	@echo "── Waiting for cluster and schema init ─────────────────────────────"
	$(MAKE) wait-scylladb

.PHONY: wait-scylladb
wait-scylladb: ## Wait for ScyllaDB cluster to become Available and schema init Job to complete
	@echo "Waiting for ScyllaCluster cloudforge-scylla to become Available (up to 5 min)..."
	kubectl wait scyllacluster/cloudforge-scylla \
		--for='condition=Available' \
		--timeout=300s \
		-n cf-data
	@echo "Waiting for schema init Job to complete (up to 2 min)..."
	kubectl wait job/scylladb-schema-init \
		--for=condition=complete \
		--timeout=120s \
		-n cf-data
	@echo "✓ ScyllaDB is ready."

.PHONY: scylladb-status
scylladb-status: ## Show ScyllaDB cluster and pod status
	@echo "── ScyllaCluster ───────────────────────────────────────────────────"
	kubectl get scyllacluster -n cf-data
	@echo ""
	@echo "── Pods ────────────────────────────────────────────────────────────"
	kubectl get pods -n cf-data -l scylla/cluster=cloudforge-scylla
	@echo ""
	@echo "── Schema init Job ─────────────────────────────────────────────────"
	kubectl get job scylladb-schema-init -n cf-data 2>/dev/null || echo "(not yet deployed)"

.PHONY: scylladb-shell
scylladb-shell: ## Open a cqlsh session to the dev ScyllaDB cluster (requires cluster running)
	@echo "Connecting as cloudforge_svc to cloudforge_platform keyspace..."
	@echo "Tip: use CF_SCYLLA_PASSWORD env var to override the dev default password."
	kubectl exec -it -n cf-data \
		$$(kubectl get pod -n cf-data -l scylla/cluster=cloudforge-scylla -o jsonpath='{.items[0].metadata.name}') \
		-- cqlsh localhost 9042 \
		-u cloudforge_svc \
		-p $${CF_SCYLLA_PASSWORD:-cf-dev-secret-change-in-prod} \
		--keyspace cloudforge_platform

.PHONY: scylladb-local-shell
scylladb-local-shell: ## Open a local cqlsh session via the exposed port 9042 (requires cqlsh installed)
	@which cqlsh > /dev/null || (echo "cqlsh not found — install: pip install cqlsh" && exit 1)
	cqlsh localhost 9042 \
		-u cloudforge_svc \
		-p $${CF_SCYLLA_PASSWORD:-cf-dev-secret-change-in-prod} \
		--keyspace cloudforge_platform

.PHONY: undeploy-scylladb
undeploy-scylladb: ## Remove ScyllaDB cluster, schema resources, and operator
	@echo "── Removing ScyllaDB cluster and schema resources ───────────────────"
	kubectl delete -k deploy/kustomize/components/scylladb/ --ignore-not-found
	@echo "── Removing Scylla Operator ────────────────────────────────────────"
	helm uninstall scylla-operator -n scylla-operator --ignore-not-found
	kubectl delete namespace scylla-operator --ignore-not-found

# ── Component deployment ──────────────────────────────────────────────────────

##@ Deployment

.PHONY: deploy-component
deploy-component: ## Deploy a named component: make deploy-component SERVICE=cf-iam
	@test -n "$(SERVICE)" || (echo "ERROR: SERVICE is required. Usage: make deploy-component SERVICE=cf-iam" && exit 1)
	helm upgrade --install $(SERVICE) deploy/helm/components/$(SERVICE) -n cf-system --create-namespace

# ── Code generation ───────────────────────────────────────────────────────────

##@ Code Generation

.PHONY: gen-api
gen-api: ## Generate server stubs and client SDK for one service: make gen-api SERVICE=storage
	@test -n "$(SERVICE)" || (echo "ERROR: SERVICE is required. Usage: make gen-api SERVICE=storage" && exit 1)
	@which oapi-codegen > /dev/null || (echo "oapi-codegen not found — run: go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest" && exit 1)
	cd api/$(SERVICE)/v1 && oapi-codegen --config oapi-server.cfg.yaml openapi.yaml
	cd api/$(SERVICE)/v1 && oapi-codegen --config oapi-client.cfg.yaml openapi.yaml

.PHONY: gen-all
gen-all: ## Regenerate all OpenAPI server stubs and client SDKs
	@for svc in $(API_SERVICES); do \
	  echo "→ generating $$svc"; \
	  $(MAKE) gen-api SERVICE=$$svc; \
	done

# ── Testing ───────────────────────────────────────────────────────────────────

##@ Testing

.PHONY: test
test: test-unit ## Alias for test-unit

.PHONY: test-unit
test-unit: ## Run unit tests (excludes integration tests)
	go test -short -race -count=1 ./...

.PHONY: test-integration
test-integration: ## Run integration tests (requires Docker for testcontainers)
	go test -tags=integration -race -count=1 -timeout=120s ./...

.PHONY: test-all
test-all: test-unit test-integration ## Run unit and integration tests

.PHONY: test-coverage
test-coverage: ## Run unit tests with per-package and per-function coverage report
	@echo "Running tests with coverage across all packages..."
	go test -short -race -coverprofile=coverage.out -covermode=atomic \
		$(shell go list ./... \
			| grep -v '/mocks' \
			| grep -v 'internal/testutil' \
			| grep -v '/generated' \
			| grep -v 'pkg/client/')
	@echo ""
	@echo "── Per-package coverage ────────────────────────────────────────────"
	@go tool cover -func=coverage.out | grep -E "^github|^total" | \
		awk '{ printf "%-70s %s\n", $$1, $$NF }'
	@echo ""
	@go tool cover -func=coverage.out | tail -1
	@echo ""
	@echo "── Generating HTML report → coverage.html ──────────────────────────"
	go tool cover -html=coverage.out -o coverage.html
	@echo "Open coverage.html in a browser to browse line-by-line coverage."

# ── Provisioner ───────────────────────────────────────────────────────────────

##@ Provisioner

.PHONY: provisioner-test
provisioner-test: ## Run unit tests for internal/provisioner (no Docker required)
	go test -short -race -count=1 -v ./internal/provisioner/...

.PHONY: provisioner-test-integration
provisioner-test-integration: ## Run integration tests for internal/provisioner (requires Docker)
	@echo "── Starting OpenBao testcontainer + running provisioner integration tests ─"
	@echo "   Requires: Docker running on this host"
	go test -tags=integration -race -count=1 -timeout=120s -v ./internal/provisioner/...

.PHONY: provisioner-coverage
provisioner-coverage: ## Unit-test coverage for internal/provisioner (no Docker)
	@echo "── Unit test coverage: internal/provisioner ─────────────────────────────"
	go test -short -race -coverprofile=provisioner-coverage.out -covermode=atomic \
		./internal/provisioner/...
	@echo ""
	@echo "── Per-function coverage ───────────────────────────────────────────"
	@go tool cover -func=provisioner-coverage.out | awk '{ printf "%-70s %s\n", $$1, $$NF }'
	@echo ""
	@echo "── Generating HTML report → provisioner-coverage.html ──────────────"
	go tool cover -html=provisioner-coverage.out -o provisioner-coverage.html
	@echo "Open provisioner-coverage.html to browse line-by-line coverage."

.PHONY: provisioner-coverage-integration
provisioner-coverage-integration: ## Full coverage (unit + integration) for internal/provisioner — requires Docker
	@echo "── Full coverage (unit + integration): internal/provisioner ─────────────"
	@echo "   Requires: Docker running on this host"
	go test -tags=integration -race -count=1 -timeout=120s \
		-coverprofile=provisioner-coverage.out -covermode=atomic \
		./internal/provisioner/...
	@echo ""
	@echo "── Per-function coverage ───────────────────────────────────────────"
	@go tool cover -func=provisioner-coverage.out | awk '{ printf "%-70s %s\n", $$1, $$NF }'
	@echo ""
	@go tool cover -func=provisioner-coverage.out | tail -1
	@echo ""
	@echo "── Generating HTML report → provisioner-coverage.html ──────────────"
	go tool cover -html=provisioner-coverage.out -o provisioner-coverage.html
	@echo "Open provisioner-coverage.html to browse line-by-line coverage."

# ── Linting and formatting ────────────────────────────────────────────────────

##@ Lint & Format

.PHONY: lint
lint: ## Run golangci-lint (same config as CI)
	@which golangci-lint > /dev/null || (echo "golangci-lint not found — run: brew install golangci-lint" && exit 1)
	golangci-lint run --config=.golangci.yml ./...

.PHONY: lint-fix
lint-fix: ## Run golangci-lint with auto-fix enabled
	@which golangci-lint > /dev/null || (echo "golangci-lint not found — run: brew install golangci-lint" && exit 1)
	golangci-lint run --config=.golangci.yml --fix ./...

.PHONY: fmt
fmt: ## Format all Go files (gofmt + goimports)
	gofmt -w .
	@which goimports > /dev/null && goimports -w -local github.com/jtomasevic/cloud-forge . || \
		echo "goimports not found — run: go install golang.org/x/tools/cmd/goimports@latest"

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: check
check: fmt vet lint ## Run fmt + vet + lint in sequence

# ── Build ─────────────────────────────────────────────────────────────────────

##@ Build

.PHONY: build
build: ## Build all CloudForge binaries to ./bin/
	@mkdir -p bin
	@for svc in $(SERVICES); do \
	  echo "→ building $$svc"; \
	  go build -trimpath -ldflags="-s -w" -o bin/$$svc ./cmd/$$svc; \
	done

.PHONY: build-service
build-service: ## Build a single service binary: make build-service SERVICE=cf-iam
	@test -n "$(SERVICE)" || (echo "ERROR: SERVICE is required. Usage: make build-service SERVICE=cf-iam" && exit 1)
	@mkdir -p bin
	go build -trimpath -ldflags="-s -w" -o bin/$(SERVICE) ./cmd/$(SERVICE)

# ── Container images ──────────────────────────────────────────────────────────

##@ Container Images

.PHONY: image-build
image-build: ## Build a local container image with ko: make image-build SERVICE=cf-iam
	@test -n "$(SERVICE)" || (echo "ERROR: SERVICE is required. Usage: make image-build SERVICE=cf-iam" && exit 1)
	ko build --local ./cmd/$(SERVICE)

.PHONY: image-push
image-push: ## Build and push a container image: make image-push SERVICE=cf-iam
	@test -n "$(SERVICE)" || (echo "ERROR: SERVICE is required. Usage: make image-push SERVICE=cf-iam" && exit 1)
	KO_DOCKER_REPO=$(REGISTRY) ko build ./cmd/$(SERVICE)

.PHONY: image-push-all
image-push-all: ## Build and push images for all services
	@for svc in $(SERVICES); do \
	  echo "→ pushing $$svc"; \
	  $(MAKE) image-push SERVICE=$$svc; \
	done

# ── Utilities ─────────────────────────────────────────────────────────────────

##@ Utilities

.PHONY: tidy
tidy: ## Run go work sync and go mod tidy
	go work sync
	go mod tidy

.PHONY: clean
clean: ## Remove build artifacts (bin/, coverage files, dist/)
	rm -rf bin/ coverage.out coverage.html dist/

.PHONY: verify
verify: tidy fmt vet lint test-unit ## Full local verification (tidy → fmt → vet → lint → test)
