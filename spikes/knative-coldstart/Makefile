##
## Spike 0.8 — Knative Scale-to-Zero Cold Start Benchmark
##
## One-command run:   make run       (cluster → Knative → functions → wait → benchmark)
## One-command clean: make destroy   (removes everything including the k3d cluster)
##
## Prerequisites: Docker daemon running, k3d, kubectl, Go 1.26+
##

# Cluster name as set in deploy/k3d/cluster.yaml
CLUSTER   := cloudforge-dev

# ko pushes images to the k3d in-cluster registry by default.
# Override KO_DOCKER_REPO to push to an external registry.
# ko image registry (used only when USE_KO=1).
KO_DOCKER_REPO ?= k3d-cloudforge-registry:5001

# Set USE_KO=1 to build images with ko instead of docker.
# ko produces smaller images but requires `brew install ko` on macOS.
# Default: USE_KO=0 — use plain docker build + k3d image import.
USE_KO ?= 0

# Knative Serving version to install (pinned so the spike is reproducible).
KNATIVE_VERSION := v1.15.0

# k3d cluster name (must match deploy/k3d/cluster.yaml at the repo root).
CLUSTER := cloudforge-dev

# Number of cold-start samples per variant.  Override with: make measure SAMPLES=5
SAMPLES ?= 10

# Kubernetes namespace where the Knative Services live.
NAMESPACE ?= default

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS=":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\n"} \
	  /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-26s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# ── Setup ─────────────────────────────────────────────────────────────────────

.PHONY: setup
setup: gen-stubs ## Download Go module dependencies and create payload stubs (run after fresh clone)
	go mod tidy

# ── Knative Serving installation ──────────────────────────────────────────────

.PHONY: deploy-knative
deploy-knative: ## Install Knative Serving CRDs, core, and net-kourier on the k3d cluster
	@echo "── Knative Serving CRDs ─────────────────────────────────────────────"
	@# --validate=false is required for CRD manifests on fresh k3d clusters.
	@# kubectl tries to download the OpenAPI schema to validate CRDs before
	@# applying them, but on a new k3s API server this endpoint is not yet
	@# populated, causing "failed to download openapi".  --validate=false
	@# skips client-side validation and is safe for CRD objects.
	kubectl apply --validate=false \
		-f https://github.com/knative/serving/releases/download/knative-$(KNATIVE_VERSION)/serving-crds.yaml
	@echo "── Knative Serving core ─────────────────────────────────────────────"
	kubectl apply --validate=false \
		-f https://github.com/knative/serving/releases/download/knative-$(KNATIVE_VERSION)/serving-core.yaml
	@echo "── net-kourier networking layer ─────────────────────────────────────"
	kubectl apply --validate=false \
		-f https://github.com/knative/net-kourier/releases/download/knative-$(KNATIVE_VERSION)/kourier.yaml
	@echo "── Configuring Knative to use Kourier ───────────────────────────────"
	kubectl patch configmap/config-network \
		--namespace knative-serving \
		--type merge \
		--patch '{"data":{"ingress-class":"kourier.ingress.networking.knative.dev"}}'
	@echo "── Enabling initial-scale=0 (required for cold-start benchmark) ─────"
	@# Knative defaults allow-zero-initial-scale to false.  Setting it to true
	@# allows service manifests to specify initial-scale=0, which ensures a
	@# freshly-deployed function starts with zero pods — the correct starting
	@# state for a cold-start measurement.
	kubectl patch configmap/config-autoscaler \
		--namespace knative-serving \
		--type merge \
		--patch '{"data":{"allow-zero-initial-scale":"true"}}'
	@echo "── Skipping remote digest resolution for dev.local images ───────────"
	@# When imagePullPolicy=Never, kubelet uses the locally-imported image.
	@# But Knative's revision controller still tries to resolve the image digest
	@# from Docker Hub BEFORE scheduling the pod — causing 401 Unauthorized.
	@# registries-skipping-tag-resolving tells Knative to skip digest resolution
	@# for any image whose registry host matches the listed prefixes.
	@# dev.local is Knative's well-known prefix for local / in-cluster images.
	kubectl patch configmap/config-deployment \
		--namespace knative-serving \
		--type merge \
		--patch '{"data":{"registries-skipping-tag-resolving":"dev.local,ko.local"}}'
	@echo "── Applying magic DNS domain (127.0.0.1.sslip.io) ───────────────────"
	kubectl apply -f deploy/config-domain.yaml
	@echo "── Waiting for Knative pods to become Available ─────────────────────"
	kubectl wait deployment --all \
		--for=condition=Available \
		--timeout=120s \
		-n knative-serving
	@echo "✓ Knative Serving is ready."
	@echo "  Services will be available at: http://fn-<variant>.$(NAMESPACE).127.0.0.1.sslip.io:9080"

.PHONY: undeploy-knative
undeploy-knative: ## Remove Knative Serving and net-kourier from the cluster
	kubectl delete -f https://github.com/knative/net-kourier/releases/download/knative-$(KNATIVE_VERSION)/kourier.yaml --ignore-not-found
	kubectl delete -f https://github.com/knative/serving/releases/download/knative-$(KNATIVE_VERSION)/serving-core.yaml --ignore-not-found
	kubectl delete -f https://github.com/knative/serving/releases/download/knative-$(KNATIVE_VERSION)/serving-crds.yaml --ignore-not-found

.PHONY: knative-status
knative-status: ## Show Knative Serving pod and deployment status
	@echo "── Pods ─────────────────────────────────────────────────────────────"
	kubectl get pods -n knative-serving
	@echo "── Deployments ──────────────────────────────────────────────────────"
	kubectl get deployments -n knative-serving

# ── Function payloads ─────────────────────────────────────────────────────────

.PHONY: gen-stubs
gen-stubs: ## Create 1-byte placeholder payload.bin files required for go:embed (fresh clone)
	@# After `git clone`, functions/medium/payload.bin and functions/heavy/payload.bin
	@# do not exist (they are gitignored).  The //go:embed payload.bin directives in
	@# those packages will cause `go build` to fail without at least a 1-byte file.
	@# Run this target once after cloning to unblock compilation.
	@# Run `make gen-payloads` before benchmarking to replace stubs with real sizes.
	@mkdir -p functions/medium functions/heavy
	@if [ ! -f functions/medium/payload.bin ]; then \
		printf '\x00' > functions/medium/payload.bin; \
		echo "✓ Created 1-byte stub: functions/medium/payload.bin"; \
	else \
		echo "  functions/medium/payload.bin already exists — skipping"; \
	fi
	@if [ ! -f functions/heavy/payload.bin ]; then \
		printf '\x00' > functions/heavy/payload.bin; \
		echo "✓ Created 1-byte stub: functions/heavy/payload.bin"; \
	else \
		echo "  functions/heavy/payload.bin already exists — skipping"; \
	fi

.PHONY: gen-payloads
gen-payloads: gen-stubs ## Generate synthetic payload files for medium (50MB) and heavy (200MB) variants
	@# Skip if payloads are already full-sized (> 1 byte).
	@# DO NOT git-add these files after regeneration — they are gitignored in large form.
	@MEDIUM_SIZE=$$(wc -c < functions/medium/payload.bin 2>/dev/null || echo 0); \
	if [ "$$MEDIUM_SIZE" -gt 1024 ]; then \
		echo "✓ medium payload already generated ($$MEDIUM_SIZE bytes) — skipping"; \
	else \
		echo "Generating medium payload (50 MB)..."; \
		dd if=/dev/urandom of=functions/medium/payload.bin bs=1M count=50 status=progress; \
	fi
	@HEAVY_SIZE=$$(wc -c < functions/heavy/payload.bin 2>/dev/null || echo 0); \
	if [ "$$HEAVY_SIZE" -gt 1024 ]; then \
		echo "✓ heavy payload already generated ($$HEAVY_SIZE bytes) — skipping"; \
	else \
		echo "Generating heavy payload (200 MB)..."; \
		dd if=/dev/urandom of=functions/heavy/payload.bin bs=1M count=200 status=progress; \
	fi
	@echo "✓ Payloads ready."

# ── Function deployment ───────────────────────────────────────────────────────

.PHONY: deploy-functions
deploy-functions: gen-payloads ## Build images (docker or ko) and apply Knative Services
ifeq ($(USE_KO),1)
	@which ko > /dev/null || \
		(echo "ko not found — install with: brew install ko  (macOS)" && \
		 echo "             or visit:       https://ko.build/install/" && exit 1)
	@echo "── Building and deploying with ko ────────────────────────────────────"
	KO_DOCKER_REPO=$(KO_DOCKER_REPO) ko apply -f deploy/service-minimal.yaml
	KO_DOCKER_REPO=$(KO_DOCKER_REPO) ko apply -f deploy/service-medium.yaml
	KO_DOCKER_REPO=$(KO_DOCKER_REPO) ko apply -f deploy/service-heavy.yaml
else
	@echo "── Building images with docker (USE_KO=0) ────────────────────────────"
	@echo "   To use ko instead: make deploy-functions USE_KO=1"
	$(MAKE) _docker-build-and-import
	@echo "── Applying Knative Services ─────────────────────────────────────────"
	kubectl apply -f deploy/service-minimal-docker.yaml
	kubectl apply -f deploy/service-medium-docker.yaml
	kubectl apply -f deploy/service-heavy-docker.yaml
endif
	@echo "── Waiting for all Knative Services to become Ready (up to 2 min) ────"
	kubectl wait ksvc/fn-minimal ksvc/fn-medium ksvc/fn-heavy \
		--for=condition=Ready \
		--timeout=120s \
		-n $(NAMESPACE)
	@echo "✓ Functions deployed. Waiting for scale-to-zero (30s idle)..."

# Detect the host CPU architecture so we build the right binary for k3d nodes
# running on the same machine (arm64 on Apple Silicon, amd64 on Intel/x86).
HOST_ARCH := $(shell uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')

.PHONY: _docker-build-and-import
_docker-build-and-import: ## (internal) docker build + k3d image import for all variants
	@# Images are tagged with the dev.local/ prefix so Knative's revision
	@# controller skips remote digest resolution (configured above via
	@# registries-skipping-tag-resolving: dev.local).  Without this prefix,
	@# the controller tries to resolve fn-minimal:spike against Docker Hub
	@# and gets a 401 even though imagePullPolicy=Never would work fine at runtime.
	@echo "Building dev.local/fn-minimal (arch=$(HOST_ARCH))..."
	docker build \
		--build-arg GOARCH=$(HOST_ARCH) \
		-t dev.local/fn-minimal:spike \
		-f functions/minimal/Dockerfile \
		.
	@echo "Building dev.local/fn-medium (arch=$(HOST_ARCH))..."
	docker build \
		--build-arg GOARCH=$(HOST_ARCH) \
		-t dev.local/fn-medium:spike \
		-f functions/medium/Dockerfile \
		.
	@echo "Building dev.local/fn-heavy (arch=$(HOST_ARCH))..."
	docker build \
		--build-arg GOARCH=$(HOST_ARCH) \
		-t dev.local/fn-heavy:spike \
		-f functions/heavy/Dockerfile \
		.
	@echo "── Importing images into k3d cluster $(CLUSTER) ──────────────────────"
	k3d image import dev.local/fn-minimal:spike dev.local/fn-medium:spike dev.local/fn-heavy:spike \
		-c $(CLUSTER)
	@echo "✓ Images imported into k3d."


.PHONY: undeploy-functions
undeploy-functions: ## Delete all three Knative Services
	kubectl delete ksvc/fn-minimal ksvc/fn-medium ksvc/fn-heavy \
		-n $(NAMESPACE) --ignore-not-found

.PHONY: functions-status
functions-status: ## Show Knative Service and backing pod status
	@echo "── Knative Services ──────────────────────────────────────────────────"
	kubectl get ksvc -n $(NAMESPACE) -l "cloudforge.io/spike=0.8-coldstart"
	@echo "── Pods (should be 0 when scaled to zero) ───────────────────────────"
	kubectl get pods -n $(NAMESPACE) -l "cloudforge.io/spike=0.8-coldstart"

# ── Benchmark ─────────────────────────────────────────────────────────────────

.PHONY: measure
measure: ## Run cold-start benchmark for all variants and print the performance table
	go run ./cmd/measure \
		--service=all \
		--samples=$(SAMPLES) \
		--namespace=$(NAMESPACE) \
		--knative-version=$(KNATIVE_VERSION)

.PHONY: measure-minimal
measure-minimal: ## Run cold-start benchmark for the minimal variant only
	go run ./cmd/measure --service=minimal --samples=$(SAMPLES) --namespace=$(NAMESPACE)

.PHONY: measure-medium
measure-medium: ## Run cold-start benchmark for the medium variant only
	go run ./cmd/measure --service=medium --samples=$(SAMPLES) --namespace=$(NAMESPACE)

.PHONY: measure-heavy
measure-heavy: ## Run cold-start benchmark for the heavy variant only
	go run ./cmd/measure --service=heavy --samples=$(SAMPLES) --namespace=$(NAMESPACE)

# ── Testing ───────────────────────────────────────────────────────────────────

.PHONY: test
test: ## Run all unit tests
	go test -race -count=1 ./internal/...

.PHONY: test-coverage
test-coverage: ## Run unit tests with coverage report (target ≥ 90%)
	@echo "Running tests with coverage..."
	go test -race -count=1 \
		-coverprofile=coverage.out \
		-covermode=atomic \
		./internal/...
	@echo ""
	@echo "── Per-function coverage ────────────────────────────────────────────"
	@go tool cover -func=coverage.out | grep -E "^github|^total" | \
		awk '{ printf "%-70s %s\n", $$1, $$NF }'
	@echo ""
	@go tool cover -func=coverage.out | tail -1
	@echo ""
	@echo "── Generating HTML report → coverage.html ──────────────────────────"
	go tool cover -html=coverage.out -o coverage.html
	@echo "Open coverage.html to browse line-by-line coverage."

# ── Code quality ──────────────────────────────────────────────────────────────

.PHONY: fmt
fmt: ## Format all Go files with gofmt
	gofmt -w .

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint (requires golangci-lint installed)
	@which golangci-lint > /dev/null || (echo "golangci-lint not found — run: brew install golangci-lint" && exit 1)
	golangci-lint run ./...

.PHONY: tidy
tidy: ## Tidy go.mod and go.sum
	go mod tidy

# ── Full spike lifecycle ───────────────────────────────────────────────────────

##@ One-command lifecycle

# How long to wait (seconds) after functions are Ready before probing.
# Knative needs this idle window to trigger scale-to-zero.
SCALE_DOWN_WAIT ?= 35

.PHONY: run
run: ## ★ Full run: create cluster → deploy Knative → deploy functions → wait → benchmark
	@echo ""
	@echo "╔══════════════════════════════════════════════════════════════════════╗"
	@echo "║     Spike 0.8 — Knative Scale-to-Zero Cold Start Benchmark          ║"
	@echo "╚══════════════════════════════════════════════════════════════════════╝"
	@echo ""
	@echo "Step 1/5  — Cluster"
	@echo "───────────────────────────────────────────────────────────────────────"
	@if k3d cluster list 2>/dev/null | grep -q "$(CLUSTER)"; then \
		echo "  ✓ k3d cluster '$(CLUSTER)' already running — skipping create"; \
	else \
		echo "  Creating k3d cluster '$(CLUSTER)'..."; \
		k3d cluster create --config ../../deploy/k3d/cluster.yaml; \
	fi
	@echo "  Merging kubeconfig and switching context to k3d-$(CLUSTER)..."
	k3d kubeconfig merge $(CLUSTER) --kubeconfig-merge-default
	kubectl config use-context k3d-$(CLUSTER)
	@echo "  Waiting for cluster nodes to become Ready..."
	kubectl wait node --all --for=condition=Ready --timeout=60s
	@echo "  ✓ Cluster ready."
	@echo ""
	@echo "Step 2/5  — Knative Serving"
	@echo "───────────────────────────────────────────────────────────────────────"
	@if kubectl get namespace knative-serving >/dev/null 2>&1; then \
		echo "  ✓ Knative Serving already installed — applying config patches only"; \
		kubectl patch configmap/config-network \
			--namespace knative-serving --type merge \
			--patch '{"data":{"ingress-class":"kourier.ingress.networking.knative.dev"}}' 2>/dev/null || true; \
		kubectl patch configmap/config-autoscaler \
			--namespace knative-serving --type merge \
			--patch '{"data":{"allow-zero-initial-scale":"true"}}' 2>/dev/null || true; \
		kubectl patch configmap/config-deployment \
			--namespace knative-serving --type merge \
			--patch '{"data":{"registries-skipping-tag-resolving":"dev.local,ko.local"}}' 2>/dev/null || true; \
	else \
		$(MAKE) deploy-knative || exit 1; \
	fi
	@echo ""
	@echo "Step 3/5  — Function images & Knative Services"
	@echo "───────────────────────────────────────────────────────────────────────"
	$(MAKE) deploy-functions
	@echo ""
	@echo "Step 4/5  — Waiting $(SCALE_DOWN_WAIT)s for scale-to-zero"
	@echo "───────────────────────────────────────────────────────────────────────"
	@echo "  (Knative terminates idle pods after 30s of no traffic)"
	@i=$(SCALE_DOWN_WAIT); \
	while [ $$i -gt 0 ]; do \
		printf "\r  ⏳ %2ds remaining..." $$i; \
		sleep 1; \
		i=$$((i-1)); \
	done; \
	printf "\r  ✓ Scale-to-zero window complete.              \n"
	@echo ""
	@echo "Step 5/5  — Benchmark"
	@echo "───────────────────────────────────────────────────────────────────────"
	$(MAKE) measure
	@echo ""
	@echo "Done. Results printed above."
	@echo "To clean up everything: make destroy"
	@echo ""

.PHONY: destroy
destroy: ## ★ Full cleanup: remove functions, Knative, domain config, and delete the k3d cluster
	@echo ""
	@echo "╔══════════════════════════════════════════════════════════════════════╗"
	@echo "║     Spike 0.8 — Full Teardown                                       ║"
	@echo "╚══════════════════════════════════════════════════════════════════════╝"
	@echo ""
	@echo "Step 1/3  — Removing Knative Services"
	kubectl delete ksvc/fn-minimal ksvc/fn-medium ksvc/fn-heavy \
		-n $(NAMESPACE) --ignore-not-found || true
	@echo ""
	@echo "Step 2/3  — Removing Knative Serving"
	kubectl delete -f deploy/config-domain.yaml --ignore-not-found || true
	kubectl delete -f https://github.com/knative/net-kourier/releases/download/knative-$(KNATIVE_VERSION)/kourier.yaml \
		--ignore-not-found || true
	kubectl delete -f https://github.com/knative/serving/releases/download/knative-$(KNATIVE_VERSION)/serving-core.yaml \
		--ignore-not-found || true
	kubectl delete -f https://github.com/knative/serving/releases/download/knative-$(KNATIVE_VERSION)/serving-crds.yaml \
		--ignore-not-found || true
	@echo ""
	@echo "Step 3/3  — Deleting k3d cluster '$(CLUSTER)'"
	k3d cluster delete $(CLUSTER) || true
	@echo ""
	@echo "✓ Everything removed."
	@echo ""

.PHONY: spike-run
spike-run: deploy-knative deploy-functions ## Deploy Knative + functions (no cluster create, no auto-measure)
	@echo ""
	@echo "Knative and all function variants are deployed."
	@echo "Wait $(SCALE_DOWN_WAIT)s for scale-to-zero, then run: make measure"

.PHONY: teardown
teardown: undeploy-functions undeploy-knative ## Remove spike components only — keeps the k3d cluster running
	kubectl delete -f deploy/config-domain.yaml --ignore-not-found
	@echo "✓ Spike components removed (cluster still running). For full cleanup: make destroy"

# ── Utilities ─────────────────────────────────────────────────────────────────

.PHONY: clean
clean: ## Remove build artifacts and coverage files
	rm -rf bin/ coverage.out coverage.html
