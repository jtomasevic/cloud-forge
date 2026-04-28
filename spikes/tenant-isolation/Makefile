# ── Spike: Tenant Network Isolation (vCluster) ───────────────────────────────
# Makefile for the vCluster tenant isolation spike.
#
# Two top-level commands:
#
#   make run     — install prerequisites, create vClusters, run all 6 tests,
#                  print results table. Cluster is left running for inspection.
#
#   make clean   — delete tenant vClusters and generated kubeconfigs.
#                  Does NOT destroy the k3d cluster.
#
# Additional helpers:
#
#   make test            — run Go unit tests (no cluster required)
#   make test-coverage   — run unit tests and print coverage summary
#   make build           — build the probe binary
#   make check-tools     — verify required CLI tools are present
#   make create-vclusters — create tenant-a and tenant-b vClusters only
#   make run-probe       — run the probe binary against existing vClusters

.PHONY: run clean test test-coverage build check-tools create-vclusters \
        run-probe install-vcluster install-helm metrics-server help

# ── Configuration ─────────────────────────────────────────────────────────────

CLUSTER       ?= cloudforge-dev
TENANT_A      ?= tenant-a
TENANT_B      ?= tenant-b
KC_DIR        ?= kubeconfigs
SPEED_SAMPLES ?= 3
BINARY        := bin/probe

# Overhead measurement settings for Test 4 (resource overhead).
#
# OVERHEAD_WAIT: stabilization period before the first sample.
#   vCluster's embedded k3s/etcd consumes high CPU for ~2-3 min after creation.
#   Waiting 2 minutes ensures we measure true idle (steady-state) load.
#   Set to 0 to skip (useful when vClusters have already been running for a while).
#
# OVERHEAD_INTERVAL: pause between consecutive CPU/RAM snapshots (30s).
#   Three samples 30s apart give a p50 resilient to transient reconciliation bursts.
#
# OVERHEAD_SAMPLES: number of snapshots (3 = enough for p50 with minimal wall time).
OVERHEAD_WAIT     ?= 120s
OVERHEAD_INTERVAL ?= 30s
OVERHEAD_SAMPLES  ?= 3

# ── Default target ─────────────────────────────────────────────────────────────

.DEFAULT_GOAL := help

# ──────────────────────────────────────────────────────────────────────────────
# run — full end-to-end: tools → cluster check → vClusters → tests
# ──────────────────────────────────────────────────────────────────────────────

run: install-tools check-cluster build create-vclusters install-metrics-server run-probe ## Run the full spike (install tools, create vClusters, run all 6 tests)

# ──────────────────────────────────────────────────────────────────────────────
# clean — remove vClusters and generated kubeconfigs (keeps the k3d cluster)
# ──────────────────────────────────────────────────────────────────────────────

clean: ## Delete tenant vClusters and generated kubeconfigs
	@echo "── Deleting vCluster: $(TENANT_A) ──────────────────────────────────"
	vcluster delete $(TENANT_A) -n vcluster-$(TENANT_A) || true
	@echo "── Deleting vCluster: $(TENANT_B) ──────────────────────────────────"
	vcluster delete $(TENANT_B) -n vcluster-$(TENANT_B) || true
	@echo "── Removing generated kubeconfigs ──────────────────────────────────"
	rm -rf $(KC_DIR)
	@echo "✓ Cleanup complete (k3d cluster '$(CLUSTER)' preserved)"

# ──────────────────────────────────────────────────────────────────────────────
# install-tools — ensure required CLI tools are present, install if missing
# ──────────────────────────────────────────────────────────────────────────────

install-tools: ## Install missing prerequisites (vcluster, helm, k3d, kubectl)
	@echo "── Checking required tools ─────────────────────────────────────────"
	@command -v kubectl  >/dev/null 2>&1 || (echo "kubectl not found — install: brew install kubectl"  && exit 1)
	@command -v k3d      >/dev/null 2>&1 || (echo "k3d not found — installing via brew..." && brew install k3d)
	@command -v helm     >/dev/null 2>&1 || (echo "helm not found — installing via brew..." && brew install helm)
	@command -v vcluster >/dev/null 2>&1 || $(MAKE) install-vcluster
	@echo "✓ All required tools available"
	@echo "   kubectl:  $$(kubectl version --client --short 2>/dev/null | head -1)"
	@echo "   k3d:      $$(k3d version | head -1)"
	@echo "   helm:     $$(helm version --short 2>/dev/null | head -1)"
	@echo "   vcluster: $$(vcluster version 2>/dev/null | head -1)"

install-vcluster: ## Install vCluster CLI via Homebrew
	@echo "── Installing vCluster CLI ──────────────────────────────────────────"
	brew install loft-sh/tap/vcluster
	@echo "✓ vcluster installed: $$(vcluster version | head -1)"

install-helm: ## Install Helm via Homebrew
	brew install helm

# ──────────────────────────────────────────────────────────────────────────────
# check-cluster — verify the k3d cluster is running and kubectl context is set
# ──────────────────────────────────────────────────────────────────────────────

check-cluster: ## Verify the k3d cluster and kubectl context
	@kubectl config current-context 2>/dev/null | grep -q "$(CLUSTER)" \
	  || (echo "ERROR: kubectl context does not point to k3d-$(CLUSTER)." \
	      && echo "       Run: kubectl config use-context k3d-$(CLUSTER)" && exit 1)
	@kubectl cluster-info >/dev/null 2>&1 \
	  || (echo "ERROR: Cannot reach cluster. Ensure k3d cluster '$(CLUSTER)' is running:" \
	      && echo "       Run from repo root: make dev-up" && exit 1)
	@echo "✓ Cluster k3d-$(CLUSTER) is reachable"

# ──────────────────────────────────────────────────────────────────────────────
# create-vclusters — provision tenant-a and tenant-b vClusters
# ──────────────────────────────────────────────────────────────────────────────

create-vclusters: ## Create tenant-a and tenant-b vClusters on the k3d cluster
	@echo "── Creating namespace vcluster-$(TENANT_A) ─────────────────────────"
	kubectl create namespace vcluster-$(TENANT_A) --dry-run=client -o yaml | kubectl apply -f -
	@echo "── Creating vCluster: $(TENANT_A) ──────────────────────────────────"
	vcluster create $(TENANT_A) -n vcluster-$(TENANT_A) --connect=false \
	  || echo "Note: vCluster '$(TENANT_A)' may already exist"
	@echo "── Creating namespace vcluster-$(TENANT_B) ─────────────────────────"
	kubectl create namespace vcluster-$(TENANT_B) --dry-run=client -o yaml | kubectl apply -f -
	@echo "── Creating vCluster: $(TENANT_B) ──────────────────────────────────"
	vcluster create $(TENANT_B) -n vcluster-$(TENANT_B) --connect=false \
	  || echo "Note: vCluster '$(TENANT_B)' may already exist"
	@echo "── Exporting kubeconfigs ────────────────────────────────────────────"
	@mkdir -p $(KC_DIR)
	vcluster connect $(TENANT_A) -n vcluster-$(TENANT_A) --print > $(KC_DIR)/$(TENANT_A).kubeconfig
	vcluster connect $(TENANT_B) -n vcluster-$(TENANT_B) --print > $(KC_DIR)/$(TENANT_B).kubeconfig
	@echo "✓ kubeconfigs written to $(KC_DIR)/"

# ──────────────────────────────────────────────────────────────────────────────
# install-metrics-server — deploy metrics-server so Test 4 can measure overhead
# ──────────────────────────────────────────────────────────────────────────────

install-metrics-server: ## Deploy metrics-server (required for Test 4: resource overhead)
	@if kubectl get deployment metrics-server -n kube-system >/dev/null 2>&1; then \
	    echo "✓ metrics-server already installed"; \
	else \
	    echo "── Installing metrics-server ────────────────────────────────────────"; \
	    kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml; \
	    kubectl patch deployment metrics-server -n kube-system --type=json \
	        -p='[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--kubelet-insecure-tls"}]'; \
	    kubectl wait deployment -n kube-system metrics-server --for=condition=Available --timeout=120s; \
	    echo "✓ metrics-server ready"; \
	fi

# ──────────────────────────────────────────────────────────────────────────────
# run-probe — run the compiled probe binary against existing vClusters
# ──────────────────────────────────────────────────────────────────────────────

run-probe: build ## Run the probe binary (assumes vClusters already exist)
	@echo ""
	@echo "── Tenant Isolation Spike ──────────────────────────────────────────"
	@echo "   cluster=$(CLUSTER)  tenant-a=$(TENANT_A)  tenant-b=$(TENANT_B)"
	@echo "   speed-samples=$(SPEED_SAMPLES)"
	@echo "   overhead-wait=$(OVERHEAD_WAIT)  overhead-interval=$(OVERHEAD_INTERVAL)  overhead-samples=$(OVERHEAD_SAMPLES)"
	@echo "   NOTE: Test 4 will pause $(OVERHEAD_WAIT) for CPU stabilization then take"
	@echo "         $(OVERHEAD_SAMPLES) samples at $(OVERHEAD_INTERVAL) intervals — total ~$$(echo "$(OVERHEAD_WAIT)" | sed 's/s/ sec/;s/m/ min/') + $$(( $(OVERHEAD_SAMPLES) * 30 ))s"
	@echo ""
	$(BINARY) \
	  -tenant-a          $(TENANT_A) \
	  -tenant-b          $(TENANT_B) \
	  -kubeconfig-dir    $(KC_DIR) \
	  -speed-samples     $(SPEED_SAMPLES) \
	  -overhead-wait     $(OVERHEAD_WAIT) \
	  -overhead-interval $(OVERHEAD_INTERVAL) \
	  -overhead-samples  $(OVERHEAD_SAMPLES) \
	  -skip-create \
	  -verbose

# ──────────────────────────────────────────────────────────────────────────────
# build — compile the probe binary
# ──────────────────────────────────────────────────────────────────────────────

build: ## Build the probe binary to bin/probe
	@mkdir -p bin
	go build -o $(BINARY) ./cmd/probe
	@echo "✓ Built $(BINARY)"

# ──────────────────────────────────────────────────────────────────────────────
# test / test-coverage
# ──────────────────────────────────────────────────────────────────────────────

test: ## Run unit tests (no cluster required)
	go test ./... -count=1

test-coverage: ## Run unit tests with coverage report
	go test ./internal/... -count=1 -coverprofile=coverage.out
	@echo ""
	@go tool cover -func=coverage.out | grep -E "^total:|cluster|probe" | tail -5
	@go tool cover -func=coverage.out | tail -1
	@echo ""
	@echo "HTML report: go tool cover -html=coverage.out"

# ──────────────────────────────────────────────────────────────────────────────
# check-tools — display tool versions (no installation)
# ──────────────────────────────────────────────────────────────────────────────

check-tools: ## Check installed tool versions (no installation)
	@echo "Tool versions:"
	@kubectl version --client --short 2>/dev/null  || echo "  kubectl:  NOT FOUND"
	@k3d version 2>/dev/null | head -1             || echo "  k3d:      NOT FOUND"
	@helm version --short 2>/dev/null | head -1    || echo "  helm:     NOT FOUND"
	@vcluster version 2>/dev/null | head -1        || echo "  vcluster: NOT FOUND"

# ──────────────────────────────────────────────────────────────────────────────
# help
# ──────────────────────────────────────────────────────────────────────────────

help: ## Show available targets
	@echo ""
	@echo "Spike: Tenant Network Isolation (vCluster)"
	@echo ""
	@echo "  make run        Full end-to-end: install tools → create vClusters → run 6 tests"
	@echo "                  Test 4 waits OVERHEAD_WAIT=$(OVERHEAD_WAIT) for CPU stabilization,"
	@echo "                  then takes $(OVERHEAD_SAMPLES) samples every $(OVERHEAD_INTERVAL)."
	@echo "                  Total wall time: ~5-6 minutes."
	@echo ""
	@echo "  make clean      Delete tenant vClusters + kubeconfigs (keeps k3d cluster)"
	@echo ""
	@echo "  make test             Unit tests (no cluster)"
	@echo "  make test-coverage    Unit tests + coverage report"
	@echo "  make build            Build the probe binary"
	@echo "  make create-vclusters Create only the vClusters"
	@echo "  make run-probe        Run probe against existing vClusters (with overhead tuning)"
	@echo "  make check-tools      Show installed tool versions"
	@echo ""
	@echo "Overhead tuning variables (override on command line):"
	@echo "  OVERHEAD_WAIT=$(OVERHEAD_WAIT)   stabilization wait before Test 4 measurement"
	@echo "  OVERHEAD_INTERVAL=$(OVERHEAD_INTERVAL)  interval between overhead samples"
	@echo "  OVERHEAD_SAMPLES=$(OVERHEAD_SAMPLES)   number of overhead CPU/RAM snapshots"
	@echo ""
