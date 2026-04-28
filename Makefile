MODULE  := github.com/jtomasevic/cloud-forge
REGISTRY := ghcr.io/jtomasevic/cloud-forge

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
dev-up: tools-check ## Start local k3d cluster, bootstrap infrastructure, and deploy ScyllaDB
	k3d cluster create --config deploy/k3d/cluster.yaml
	kubectl apply -k deploy/kustomize/base/
	bash scripts/dev-bootstrap.sh
	$(MAKE) deploy-scylladb

.PHONY: dev-down
dev-down: ## Stop and delete local k3d cluster
	k3d cluster delete cloudforge-dev

.PHONY: dev-reset
dev-reset: dev-down dev-up ## Destroy and recreate local cluster from scratch

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
