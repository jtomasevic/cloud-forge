CLUSTER       ?= cloudforge-dev
BINARY        := bin/bench
SCYLLADB_HOST ?= 127.0.0.1
SCYLLADB_PORT ?= 9042
# Schema path relative to the working directory where `make run` is executed.
SCHEMA        := schema/schema.cql

# Default seed / benchmark parameters (override on the command line).
SEED          ?= 1000
OPS           ?= 2000
CONC          ?= 50
WRITERS       ?= 20

.DEFAULT_GOAL := help

# ─────────────────────────────────────────────────────────────────────────────
.PHONY: help
help: ## Show this help message
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} \
	  /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# ─────────────────────────────────────────────────────────────────────────────
# Primary workflow
# ─────────────────────────────────────────────────────────────────────────────

.PHONY: run
run: check-cluster build ## Run the full benchmark suite against k3d ScyllaDB
	@echo "── ScyllaDB Account Store Spike ─────────────────────────────────────────────"
	@echo "   host=$(SCYLLADB_HOST)  port=$(SCYLLADB_PORT)"
	@echo "   seed=$(SEED)  ops=$(OPS)  concurrency=$(CONC)  lwt-writers=$(WRITERS)"
	@echo ""
	$(BINARY) \
	  -host    $(SCYLLADB_HOST) \
	  -port    $(SCYLLADB_PORT) \
	  -schema  $(SCHEMA) \
	  -seed    $(SEED) \
	  -ops     $(OPS) \
	  -conc    $(CONC) \
	  -writers $(WRITERS) \
	  -drop
# NOTE: run `bin/bench` directly from the terminal for richer output —
# stdout/stderr are separated, so LWT debug/error messages appear on stderr.

.PHONY: measure
measure: build ## Re-run benchmarks without seeding (uses existing data, no -drop)
	@echo "── Re-running benchmark (no seed, no schema drop) ───────────────────────────"
	$(BINARY) \
	  -host    $(SCYLLADB_HOST) \
	  -port    $(SCYLLADB_PORT) \
	  -schema  $(SCHEMA) \
	  -seed    0 \
	  -ops     $(OPS) \
	  -conc    $(CONC) \
	  -writers $(WRITERS)

.PHONY: destroy
destroy: ## Drop the cf keyspace from ScyllaDB (clean up spike data)
	@echo "── Dropping cf keyspace ─────────────────────────────────────────────────────"
	kubectl exec -n cf-data \
	  $$(kubectl get pod -n cf-data -l scylla/cluster=cloudforge-scylla -o jsonpath='{.items[0].metadata.name}') \
	  -- cqlsh -e "DROP KEYSPACE IF EXISTS cf;" \
	  || echo "Hint: run 'cqlsh 127.0.0.1 $(SCYLLADB_PORT)' and execute DROP KEYSPACE IF EXISTS cf;"

# ─────────────────────────────────────────────────────────────────────────────
# Build
# ─────────────────────────────────────────────────────────────────────────────

.PHONY: build
build: ## Compile the benchmark binary to bin/bench
	@mkdir -p bin
	go build -o $(BINARY) ./cmd/bench/

# ─────────────────────────────────────────────────────────────────────────────
# Testing
# ─────────────────────────────────────────────────────────────────────────────

.PHONY: test
test: ## Run unit tests (no ScyllaDB required)
	go test -short -count=1 -race ./internal/bench/...

.PHONY: test-coverage
test-coverage: ## Run unit tests with HTML coverage report
	@mkdir -p bin
	go test -short -count=1 -coverprofile=coverage.out ./internal/bench/...
	go tool cover -func=coverage.out
	go tool cover -html=coverage.out -o bin/coverage.html
	@echo "\n  Coverage report: bin/coverage.html"

.PHONY: test-integration
test-integration: build ## Run integration tests (requires ScyllaDB at SCYLLADB_HOST:9042)
	@echo "── Integration tests (SCYLLADB_HOST=$(SCYLLADB_HOST)) ───────────────────────"
	SCYLLADB_HOST=$(SCYLLADB_HOST) \
	  go test -v -count=1 -run TestIntegration ./internal/bench/...

# ─────────────────────────────────────────────────────────────────────────────
# Schema utilities
# ─────────────────────────────────────────────────────────────────────────────

.PHONY: apply-schema
apply-schema: ## Apply schema.cql to the running ScyllaDB (via cqlsh)
	@echo "── Applying schema/schema.cql ───────────────────────────────────────────────"
	cqlsh $(SCYLLADB_HOST) $(SCYLLADB_PORT) -f $(SCHEMA)

.PHONY: describe-schema
describe-schema: ## Describe the cf keyspace tables via cqlsh
	cqlsh $(SCYLLADB_HOST) $(SCYLLADB_PORT) \
	  -e "DESCRIBE KEYSPACE cf;"

# ─────────────────────────────────────────────────────────────────────────────
# Cluster guard
# ─────────────────────────────────────────────────────────────────────────────

.PHONY: check-cluster
check-cluster: ## Verify the k3d cluster and ScyllaDB pod are running
	@kubectl config current-context | grep -q "$(CLUSTER)" \
	  || (echo "ERROR: kubectl context does not point to $(CLUSTER). Run: kubectl config use-context k3d-$(CLUSTER)" && exit 1)
	@kubectl get pods -n cf-data -l scylla/cluster=cloudforge-scylla --no-headers 2>/dev/null \
	  | grep -q "Running" \
	  || (echo "ERROR: No running ScyllaDB pods found in namespace 'cf-data'." \
	        && echo "       Run 'make deploy-scylladb' from the repo root first." && exit 1)
	@echo "✓ Cluster $(CLUSTER) running, ScyllaDB pods Ready"

.PHONY: setup
setup: ## Download Go module dependencies
	go mod tidy
