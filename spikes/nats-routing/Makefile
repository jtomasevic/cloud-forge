# =============================================================================
# Makefile — Spike 0.6: NATS JetStream Multi-Tenant Routing
# =============================================================================
#
# QUICK START
#   make cluster-up   # start the 3-node NATS cluster
#   make run          # execute the spike program against the cluster
#   make cluster-down # tear everything down
#
# Run `make help` to see all available targets.
# =============================================================================

# ── Configuration ─────────────────────────────────────────────────────────────

# Compose file path (relative to this Makefile's directory)
COMPOSE_FILE := config/nats-cluster.yaml

# NATS client URL used by `make run` and `make status`
NATS_URL     ?= nats://localhost:4222

# Number of latency benchmark messages (override with: make run BENCH_MSGS=100)
BENCH_MSGS   ?= 10000

# Go module root for the spike
MODULE       := github.com/jtomasevic/cloud-forge/spikes/nats-routing

# Coverage output files
COV_OUT      := coverage.out
COV_HTML     := coverage.html

# Minimum expected coverage threshold (informational — not enforced here)
COV_THRESHOLD := 90

# Default goal — show help
.DEFAULT_GOAL := help

# ── Help ──────────────────────────────────────────────────────────────────────

.PHONY: help
help: ## Show this help message
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} \
	  /^[a-zA-Z_\/-]+:.*?##/ { printf "  \033[36m%-28s\033[0m %s\n", $$1, $$2 } \
	  /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

# ── Cluster lifecycle ─────────────────────────────────────────────────────────

##@ Cluster

.PHONY: cluster-up
cluster-up: ## Start the 3-node NATS JetStream cluster (detached)
	@echo "Starting NATS cluster…"
	docker compose -f $(COMPOSE_FILE) up -d
	@echo "Waiting 5s for JetStream leader election…"
	@sleep 5
	@$(MAKE) --no-print-directory cluster-status

.PHONY: cluster-down
cluster-down: ## Stop and remove all cluster containers + volumes
	@echo "Stopping NATS cluster…"
	docker compose -f $(COMPOSE_FILE) down -v --remove-orphans

.PHONY: cluster-restart
cluster-restart: cluster-down cluster-up ## Full cluster restart (removes data)

.PHONY: cluster-status
cluster-status: ## Show running containers and NATS monitoring endpoints
	@echo ""
	@echo "── Container status ─────────────────────────────────────────────"
	@docker compose -f $(COMPOSE_FILE) ps 2>/dev/null || echo "  (no containers found)"
	@echo ""
	@echo "── NATS monitoring ──────────────────────────────────────────────"
	@for port in 8222 8223 8224; do \
	  url="http://localhost:$${port}/healthz"; \
	  status=$$(curl -sf "$${url}" 2>/dev/null && echo "OK" || echo "UNREACHABLE"); \
	  echo "  node :$${port}  →  $${status}"; \
	done
	@echo ""

.PHONY: cluster-logs
cluster-logs: ## Tail cluster logs (Ctrl-C to exit)
	docker compose -f $(COMPOSE_FILE) logs -f

.PHONY: cluster-logs-node
cluster-logs-node: ## Tail logs for a single node: make cluster-logs-node NODE=nats-1
	docker compose -f $(COMPOSE_FILE) logs -f $(NODE)

# ── Spike program ─────────────────────────────────────────────────────────────

##@ Spike

.PHONY: build
build: ## Compile the spike binary to ./spike-bin
	@echo "Building spike binary…"
	go build -o spike-bin ./cmd
	@echo "Binary: ./spike-bin"

.PHONY: run
run: ## Run the full spike against the live cluster (requires cluster-up)
	@echo "Running Spike 0.6 against $(NATS_URL)…"
	NATS_URL=$(NATS_URL) go run ./cmd

.PHONY: run-bench-only
run-bench-only: ## Run only the latency benchmark (quick smoke-test)
	@echo "Running latency benchmark with $(BENCH_MSGS) messages…"
	NATS_URL=$(NATS_URL) BENCH_MSGS=$(BENCH_MSGS) go run ./cmd

# ── Tests ─────────────────────────────────────────────────────────────────────

##@ Tests

.PHONY: test
test: ## Run all unit tests (embedded NATS server — no Docker required)
	@echo "Running unit tests…"
	go test -count=1 -timeout=120s ./...
	@echo "All tests passed."

.PHONY: test-race
test-race: ## Run unit tests with the race detector enabled
	@echo "Running unit tests with race detector…"
	go test -race -count=1 -timeout=180s ./...
	@echo "All tests passed (race-clean)."

.PHONY: test-short
test-short: ## Run only fast tests (skips tests marked testing.Short())
	go test -short -count=1 -timeout=60s ./...

.PHONY: test-coverage
test-coverage: ## Run tests with coverage and open HTML report
	@echo "Running tests with coverage…"
	go test -count=1 -timeout=120s \
	  -coverprofile=$(COV_OUT) -covermode=atomic \
	  ./...
	@echo ""
	@echo "── Per-function coverage ─────────────────────────────────────────"
	@go tool cover -func=$(COV_OUT) | grep -E "^$(MODULE)|total:"
	@echo ""
	@go tool cover -func=$(COV_OUT) | tail -1
	@echo ""
	@echo "── Generating HTML report → $(COV_HTML) ─────────────────────────"
	@go tool cover -html=$(COV_OUT) -o $(COV_HTML)
	@echo "Open $(COV_HTML) in a browser to browse line-by-line coverage."
	@echo ""
	@TOTAL=$$(go tool cover -func=$(COV_OUT) | tail -1 | awk '{print $$NF}' | tr -d '%'); \
	  if [ "$$(echo "$$TOTAL < $(COV_THRESHOLD)" | bc)" = "1" ]; then \
	    echo "⚠️  Coverage $$TOTAL% is below threshold $(COV_THRESHOLD)%"; \
	  else \
	    echo "✓  Coverage $$TOTAL% meets threshold $(COV_THRESHOLD)%"; \
	  fi

# ── Code quality ──────────────────────────────────────────────────────────────

##@ Code Quality

.PHONY: fmt
fmt: ## Format all Go source files with gofmt
	@echo "Formatting Go files…"
	gofmt -w -s .

.PHONY: vet
vet: ## Run go vet on all packages
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint (must be installed — see Makefile for install hint)
	@if ! command -v golangci-lint &>/dev/null; then \
	  echo "golangci-lint not found.  Install via:"; \
	  echo "  brew install golangci-lint   (macOS)"; \
	  echo "  go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	  exit 1; \
	fi
	golangci-lint run ./...

.PHONY: tidy
tidy: ## Run go mod tidy to synchronise go.mod / go.sum
	go mod tidy

# ── Provisioning helpers ──────────────────────────────────────────────────────

##@ Provisioning

.PHONY: add-tenant
add-tenant: ## Dynamically add a tenant via config-reload (requires cluster-up)
	@if [ -z "$(TENANT)" ]; then \
	  echo "Usage: make add-tenant TENANT=acme"; \
	  exit 1; \
	fi
	bash scripts/add-tenant.sh $(TENANT)

.PHONY: nats-info
nats-info: ## Print NATS server info for node 1 (requires cluster-up)
	@curl -sf http://localhost:8222/varz | python3 -m json.tool 2>/dev/null \
	  || echo "NATS node 1 is not reachable on :8222"

.PHONY: nats-streams
nats-streams: ## List JetStream streams on node 1 (requires nats CLI)
	@nats stream ls --server $(NATS_URL) 2>/dev/null \
	  || echo "Install the NATS CLI:  brew install nats-io/nats-tools/nats"

# ── Clean ─────────────────────────────────────────────────────────────────────

##@ Clean

.PHONY: clean
clean: ## Remove build artefacts (binary, coverage files)
	@rm -f spike-bin $(COV_OUT) $(COV_HTML)
	@echo "Cleaned build artefacts."

.PHONY: clean-all
clean-all: clean cluster-down ## Remove artefacts AND stop + delete cluster
