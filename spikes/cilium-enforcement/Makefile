# ── Spike: Cilium Network Policy Enforcement ─────────────────────────────────
# Makefile for the Cilium enforcement spike.
#
# Two top-level commands:
#
#   make run     — install tools, DELETE the current k3d cluster, recreate it
#                  with flannel disabled, install Cilium, run all 5 tests.
#                  NOTE: destroys all running workloads (ScyllaDB, NATS, vClusters).
#
#   make clean   — delete the Cilium cluster and restore the standard
#                  dev cluster via make dev-up (also Cilium-based since the
#                  root cluster.yaml was updated to use Cilium as CNI).
#
# Additional helpers:
#
#   make test             — run Go unit tests (no cluster required)
#   make test-coverage    — run unit tests and print coverage summary
#   make build            — build the probe binary
#   make run-probe        — run probe against an existing Cilium cluster
#   make check-tools      — verify required CLI tools are present

.PHONY: run clean test test-coverage build install-tools warn-recreate \
        recreate-cluster install-cilium run-probe check-tools help

# ── Configuration ─────────────────────────────────────────────────────────────

CLUSTER        ?= cloudforge-dev
# Cilium version to install. Pinned for reproducibility.
CILIUM_VERSION ?= 1.17.3
BINARY         := bin/probe

# ── Default target ─────────────────────────────────────────────────────────────

.DEFAULT_GOAL := help

# ──────────────────────────────────────────────────────────────────────────────
# run — full end-to-end: tools → recreate cluster → install Cilium → probe
# ──────────────────────────────────────────────────────────────────────────────

run: install-tools warn-recreate recreate-cluster install-cilium run-probe ## Run the full spike (recreate cluster with Cilium, run all 5 tests)

# ──────────────────────────────────────────────────────────────────────────────
# warn-recreate — 5-second abort window before destroying the cluster
# ──────────────────────────────────────────────────────────────────────────────

warn-recreate:
	@echo ""
	@echo "⚠️  WARNING: 'make run' will DELETE the k3d cluster '$(CLUSTER)'"
	@echo "   All running workloads (ScyllaDB, NATS, vClusters) will be lost."
	@echo "   Run 'make clean' afterwards to restore the standard dev cluster."
	@echo ""
	@echo "   Press Ctrl-C to abort. Continuing in 5 seconds..."
	@sleep 5

# ──────────────────────────────────────────────────────────────────────────────
# recreate-cluster — delete existing k3d cluster and create a Cilium-compatible one
# ──────────────────────────────────────────────────────────────────────────────

recreate-cluster: ## Delete existing cluster and create one with flannel disabled
	@echo "── Deleting existing k3d cluster '$(CLUSTER)' ──────────────────────"
	k3d cluster delete $(CLUSTER) 2>/dev/null || echo "  (no existing cluster to delete)"
	@echo "── Creating Cilium-compatible k3d cluster ───────────────────────────"
	@echo "   flannel-backend=none + disable-network-policy = Cilium takes over CNI"
	k3d cluster create $(CLUSTER) \
	  --k3s-arg "--disable=traefik@server:*" \
	  --k3s-arg "--flannel-backend=none@server:*" \
	  --k3s-arg "--disable-network-policy@server:*" \
	  --agents 2 \
	  --wait
	@echo "✓ Cluster '$(CLUSTER)' created (no CNI installed yet — waiting for Cilium)"

# ──────────────────────────────────────────────────────────────────────────────
# install-cilium — install Cilium CNI via the cilium CLI
# ──────────────────────────────────────────────────────────────────────────────

install-cilium: ## Install Cilium CNI and wait for it to be ready
	@echo "── Installing Cilium $(CILIUM_VERSION) ─────────────────────────────────────────"
	cilium install --version $(CILIUM_VERSION)
	@echo "── Waiting for Cilium to be ready (up to 3 minutes) ───────────────"
	cilium status --wait --wait-duration 360s
	@echo "✓ Cilium $(CILIUM_VERSION) ready"

# ──────────────────────────────────────────────────────────────────────────────
# clean — delete Cilium cluster and restore the standard dev cluster
# ──────────────────────────────────────────────────────────────────────────────

clean: ## Delete Cilium cluster and restore the standard dev cluster (now also Cilium-based)
	@echo "── Deleting Cilium cluster '$(CLUSTER)' ────────────────────────────"
	k3d cluster delete $(CLUSTER) || true
	@echo "── Restoring standard dev cluster (Cilium CNI) ─────────────────────"
	@echo "   Running: make dev-up from repo root"
	cd ../.. && make dev-up
	@echo "✓ Standard dev cluster restored (Cilium $(CILIUM_VERSION))."
	@echo "  If you need ScyllaDB: cd ../.. && make deploy-scylladb"
	@echo "  If you need vClusters: cd ../tenant-isolation && make run"

# ──────────────────────────────────────────────────────────────────────────────
# install-tools — check and install required CLI tools
# ──────────────────────────────────────────────────────────────────────────────

install-tools: ## Check required tools, install missing ones via Homebrew
	@echo "── Checking required tools ─────────────────────────────────────────"
	@command -v kubectl  >/dev/null 2>&1 \
	  || (echo "kubectl not found — install: brew install kubectl" && exit 1)
	@command -v k3d      >/dev/null 2>&1 \
	  || (echo "k3d not found — installing via brew..." && brew install k3d)
	@command -v helm     >/dev/null 2>&1 \
	  || (echo "helm not found — installing via brew..." && brew install helm)
	@command -v cilium   >/dev/null 2>&1 \
	  || (echo "cilium CLI not found — installing via brew..." && brew install cilium-cli)
	@command -v vcluster >/dev/null 2>&1 \
	  || (echo "vcluster not found — installing via brew..." && brew install loft-sh/tap/vcluster)
	@echo "✓ All required tools available"
	@echo "   kubectl:  $$(kubectl version --client --short 2>/dev/null | head -1)"
	@echo "   k3d:      $$(k3d version | head -1)"
	@echo "   helm:     $$(helm version --short 2>/dev/null | head -1)"
	@echo "   cilium:   $$(cilium version --client 2>/dev/null | head -1)"
	@echo "   vcluster: $$(vcluster version 2>/dev/null | head -1)"

# ──────────────────────────────────────────────────────────────────────────────
# run-probe — run the probe binary against the existing Cilium cluster
# ──────────────────────────────────────────────────────────────────────────────

run-probe: build ## Run the probe binary (assumes Cilium cluster already exists)
	@echo ""
	@echo "── Cilium Enforcement Spike ────────────────────────────────────────"
	@echo "   cluster=$(CLUSTER)  cilium=$(CILIUM_VERSION)"
	@echo ""
	$(BINARY) -verbose

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
	@go tool cover -func=coverage.out | grep -E "^total:" | tail -1
	@echo ""
	@echo "HTML report: go tool cover -html=coverage.out"

# ──────────────────────────────────────────────────────────────────────────────
# check-tools — display tool versions (no installation)
# ──────────────────────────────────────────────────────────────────────────────

check-tools: ## Check installed tool versions (no installation)
	@echo "Tool versions:"
	@kubectl version --client --short 2>/dev/null   || echo "  kubectl:  NOT FOUND"
	@k3d version 2>/dev/null | head -1              || echo "  k3d:      NOT FOUND"
	@helm version --short 2>/dev/null | head -1     || echo "  helm:     NOT FOUND"
	@cilium version --client 2>/dev/null | head -1  || echo "  cilium:   NOT FOUND"
	@vcluster version 2>/dev/null | head -1         || echo "  vcluster: NOT FOUND"

# ──────────────────────────────────────────────────────────────────────────────
# help
# ──────────────────────────────────────────────────────────────────────────────

help: ## Show available targets
	@echo ""
	@echo "Spike: Cilium Network Policy Enforcement"
	@echo ""
	@echo "  make run        Full end-to-end: recreate cluster with Cilium → run 5 tests"
	@echo "                  ⚠️  DESTROYS current k3d cluster and all workloads"
	@echo "  make clean      Delete Cilium cluster + restore standard dev cluster"
	@echo ""
	@echo "  make test             Unit tests (no cluster)"
	@echo "  make test-coverage    Unit tests + coverage report"
	@echo "  make build            Build the probe binary"
	@echo "  make run-probe        Run probe against existing Cilium cluster"
	@echo "  make check-tools      Show installed tool versions"
	@echo ""
	@echo "Configuration:"
	@echo "  CLUSTER=$(CLUSTER)          k3d cluster name"
	@echo "  CILIUM_VERSION=$(CILIUM_VERSION)  Cilium version to install"
	@echo ""
