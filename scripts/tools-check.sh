#!/usr/bin/env bash
# scripts/tools-check.sh
#
# Verifies all required CloudForge development tools are installed.
# On macOS with Homebrew, missing tools are installed automatically.
# On Linux, the script prints installation instructions and exits non-zero.

set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
ok()      { echo -e "  ${GREEN}✓${NC} $1"; }
missing() { echo -e "  ${RED}✗${NC} $1 — $2"; }
info()    { echo -e "${CYAN}[tools-check]${NC} $*"; }

IS_MACOS=false
HAS_BREW=false
FAILED=0

[[ "$(uname)" == "Darwin" ]] && IS_MACOS=true
command -v brew &>/dev/null && HAS_BREW=true

brew_install() {
  local tool="$1" formula="${2:-$1}"
  if $IS_MACOS && $HAS_BREW; then
    info "Installing ${tool} via Homebrew..."
    brew install "${formula}"
  else
    FAILED=1
  fi
}

echo ""
info "Checking required development tools..."
echo ""

# ── Go ────────────────────────────────────────────────────────────────────────
REQUIRED_GO="1.26"
if command -v go &>/dev/null; then
  GO_VER=$(go version | awk '{print $3}' | sed 's/go//')
  MAJOR=$(echo "$GO_VER" | cut -d. -f1)
  MINOR=$(echo "$GO_VER" | cut -d. -f2)
  if [[ "$MAJOR" -gt 1 ]] || [[ "$MAJOR" -eq 1 && "$MINOR" -ge 26 ]]; then
    ok "go ${GO_VER}"
  else
    missing "go" "found ${GO_VER}, need >= ${REQUIRED_GO}"
    brew_install "go" || true
    FAILED=1
  fi
else
  missing "go" "not found"
  brew_install "go" || true
  FAILED=1
fi

# ── Docker ────────────────────────────────────────────────────────────────────
if command -v docker &>/dev/null && docker info &>/dev/null 2>&1; then
  DOCKER_VER=$(docker version --format '{{.Server.Version}}' 2>/dev/null || echo "unknown")
  ok "docker ${DOCKER_VER} (daemon running)"
else
  missing "docker" "not found or daemon not running"
  echo -e "    ${YELLOW}→ Install Docker Desktop: https://www.docker.com/products/docker-desktop${NC}"
  FAILED=1
fi

# ── k3d ───────────────────────────────────────────────────────────────────────
REQUIRED_K3D="5.7"
if command -v k3d &>/dev/null; then
  K3D_VER=$(k3d version | head -1 | awk '{print $3}' | sed 's/v//')
  ok "k3d ${K3D_VER}"
else
  missing "k3d" "not found (need >= ${REQUIRED_K3D})"
  brew_install "k3d" || true
  if [[ $FAILED -eq 0 ]] && ! command -v k3d &>/dev/null; then FAILED=1; fi
fi

# ── kubectl ───────────────────────────────────────────────────────────────────
if command -v kubectl &>/dev/null; then
  KUBECTL_VER=$(kubectl version --client --output=json 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['clientVersion']['gitVersion'])" 2>/dev/null || kubectl version --client --short 2>/dev/null | awk '{print $3}' || echo "unknown")
  ok "kubectl ${KUBECTL_VER}"
else
  missing "kubectl" "not found"
  brew_install "kubectl" || true
  if [[ $FAILED -eq 0 ]] && ! command -v kubectl &>/dev/null; then FAILED=1; fi
fi

# ── helm ──────────────────────────────────────────────────────────────────────
if command -v helm &>/dev/null; then
  HELM_VER=$(helm version --short 2>/dev/null | sed 's/v//' | cut -d+ -f1)
  ok "helm ${HELM_VER}"
else
  missing "helm" "not found"
  brew_install "helm" || true
  if [[ $FAILED -eq 0 ]] && ! command -v helm &>/dev/null; then FAILED=1; fi
fi

# ── task (go-task) ────────────────────────────────────────────────────────────
if command -v task &>/dev/null; then
  TASK_VER=$(task --version 2>/dev/null | awk '{print $3}' || echo "unknown")
  ok "task ${TASK_VER}"
else
  missing "task" "not found"
  brew_install "task" "go-task" || true
  if [[ $FAILED -eq 0 ]] && ! command -v task &>/dev/null; then FAILED=1; fi
fi

# ── openssl ───────────────────────────────────────────────────────────────────
if command -v openssl &>/dev/null; then
  OPENSSL_VER=$(openssl version | awk '{print $2}')
  ok "openssl ${OPENSSL_VER}"
else
  missing "openssl" "not found"
  brew_install "openssl" || true
  FAILED=1
fi

# ── golangci-lint ─────────────────────────────────────────────────────────────
if command -v golangci-lint &>/dev/null; then
  LINT_VER=$(golangci-lint version --short 2>/dev/null || golangci-lint version 2>/dev/null | awk '{print $4}' || echo "unknown")
  ok "golangci-lint ${LINT_VER}"
else
  missing "golangci-lint" "not found"
  brew_install "golangci-lint" || true
  if [[ $FAILED -eq 0 ]] && ! command -v golangci-lint &>/dev/null; then FAILED=1; fi
fi

# ── ko (optional) ─────────────────────────────────────────────────────────────
# ko is used for spike 0.8 (Knative cold-start) with USE_KO=1.
# The default build path uses plain docker build, so ko is not required.
# NOTE: `go install github.com/ko-build/ko/cmd/ko@latest` was removed in ko v0.18.
#       The only supported install is `brew install ko` (macOS) or the script at https://ko.build/install/.
if command -v ko &>/dev/null; then
  KO_VER=$(ko version 2>/dev/null | awk '{print $2}' || echo "unknown")
  ok "ko ${KO_VER} (optional — for spike 0.8 USE_KO=1 mode)"
else
  echo -e "  ${YELLOW}?${NC} ko — not found (optional)"
  echo -e "    ${YELLOW}→ Install: brew install ko${NC}"
fi

# ── cqlsh (optional) ──────────────────────────────────────────────────────────
# cqlsh is only needed for local ScyllaDB access (make scylladb-local-shell).
# It is NOT required for make dev-up — the schema init Job runs cqlsh inside
# the cluster.  We warn but do not fail if it is absent.
if command -v cqlsh &>/dev/null; then
  CQLSH_VER=$(cqlsh --version 2>/dev/null | awk '{print $2}' || echo "unknown")
  ok "cqlsh ${CQLSH_VER} (optional — for local ScyllaDB shell access)"
else
  echo -e "  ${YELLOW}?${NC} cqlsh — not found (optional)"
  echo -e "    ${YELLOW}→ Install: pip install cqlsh${NC}"
fi

# ── Result ────────────────────────────────────────────────────────────────────
echo ""
if [[ $FAILED -ne 0 ]]; then
  echo -e "${RED}One or more required tools are missing or out of date.${NC}"
  if ! $IS_MACOS || ! $HAS_BREW; then
    echo ""
    echo -e "${YELLOW}On Linux, install missing tools manually:${NC}"
    echo "  k3d:           curl -s https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh | bash"
    echo "  kubectl:       https://kubernetes.io/docs/tasks/tools/install-kubectl-linux/"
    echo "  helm:          https://helm.sh/docs/intro/install/"
    echo "  task:          https://taskfile.dev/installation/"
    echo "  golangci-lint: curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b \$(go env GOPATH)/bin"
  fi
  echo ""
  exit 1
else
  echo -e "${GREEN}All required tools are installed.${NC}"
  echo ""
fi
