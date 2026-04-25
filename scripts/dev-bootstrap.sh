#!/usr/bin/env bash
# scripts/dev-bootstrap.sh
#
# Bootstraps the local CloudForge development environment after the k3d cluster
# has been created. Safe to run multiple times — all operations are idempotent.
#
# What this script does:
#   1. Exports the k3d kubeconfig to .dev/kubeconfig
#   2. Generates a self-signed CA and wildcard TLS cert for *.cloudforge.local
#   3. Stores the TLS cert as Kubernetes secrets in the relevant namespaces
#   4. Generates a random initial admin password
#   5. Stores the admin password as a Kubernetes secret in cf-system
#   6. Prints a summary of what was created / already existed
#
# Prerequisites: k3d, kubectl, openssl

set -euo pipefail

# ── Colours ───────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
info()    { echo -e "${CYAN}[bootstrap]${NC} $*"; }
success() { echo -e "${GREEN}[bootstrap]${NC} ✓ $*"; }
warn()    { echo -e "${YELLOW}[bootstrap]${NC} ⚠ $*"; }
die()     { echo -e "${RED}[bootstrap]${NC} ✗ $*" >&2; exit 1; }

# ── Config ────────────────────────────────────────────────────────────────────
CLUSTER_NAME="cloudforge-dev"
DEV_DIR=".dev"
CERTS_DIR="${DEV_DIR}/certs"
KUBECONFIG_PATH="${DEV_DIR}/kubeconfig"
DOMAIN="cloudforge.local"
CERT_DAYS=825   # max accepted by modern browsers/tools

TLS_NAMESPACES=("cf-gateway" "cf-system")

# ── Preflight ─────────────────────────────────────────────────────────────────
for cmd in k3d kubectl openssl; do
  command -v "$cmd" &>/dev/null || die "Required command not found: $cmd"
done

k3d cluster list --no-headers 2>/dev/null | grep -q "^${CLUSTER_NAME}" \
  || die "Cluster '${CLUSTER_NAME}' is not running. Run 'make dev-up' first."

# ── Setup dirs ────────────────────────────────────────────────────────────────
mkdir -p "${CERTS_DIR}"

# ── Step 1: Export kubeconfig ─────────────────────────────────────────────────
info "Exporting kubeconfig → ${KUBECONFIG_PATH}"
k3d kubeconfig get "${CLUSTER_NAME}" > "${KUBECONFIG_PATH}"
chmod 600 "${KUBECONFIG_PATH}"
export KUBECONFIG="${KUBECONFIG_PATH}"
success "Kubeconfig written to ${KUBECONFIG_PATH}"

# ── Step 2: Generate CA and TLS certificate ───────────────────────────────────
CA_KEY="${CERTS_DIR}/ca.key"
CA_CERT="${CERTS_DIR}/ca.crt"
TLS_KEY="${CERTS_DIR}/tls.key"
TLS_CERT="${CERTS_DIR}/tls.crt"
TLS_CSR="${CERTS_DIR}/tls.csr"
EXT_FILE="${CERTS_DIR}/san.ext"

if [[ -f "${CA_CERT}" ]]; then
  warn "CA certificate already exists at ${CA_CERT} — skipping generation"
else
  info "Generating self-signed CA for ${DOMAIN}..."

  # CA key + self-signed cert
  openssl genrsa -out "${CA_KEY}" 4096 2>/dev/null
  openssl req -new -x509 \
    -key "${CA_KEY}" \
    -out "${CA_CERT}" \
    -days "${CERT_DAYS}" \
    -subj "/CN=CloudForge Dev CA/O=CloudForge/C=US" 2>/dev/null

  success "CA certificate generated"
fi

if [[ -f "${TLS_CERT}" ]]; then
  warn "TLS certificate already exists at ${TLS_CERT} — skipping generation"
else
  info "Generating wildcard TLS certificate for *.${DOMAIN}..."

  # SAN extension file
  cat > "${EXT_FILE}" <<EOF
[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name
[req_distinguished_name]
[v3_req]
subjectAltName = @alt_names
[alt_names]
DNS.1 = ${DOMAIN}
DNS.2 = *.${DOMAIN}
DNS.3 = localhost
IP.1  = 127.0.0.1
EOF

  # Leaf key + CSR
  openssl genrsa -out "${TLS_KEY}" 2048 2>/dev/null
  openssl req -new \
    -key "${TLS_KEY}" \
    -out "${TLS_CSR}" \
    -subj "/CN=*.${DOMAIN}/O=CloudForge/C=US" \
    -config "${EXT_FILE}" 2>/dev/null

  # Sign with CA
  openssl x509 -req \
    -in "${TLS_CSR}" \
    -CA "${CA_CERT}" \
    -CAkey "${CA_KEY}" \
    -CAcreateserial \
    -out "${TLS_CERT}" \
    -days "${CERT_DAYS}" \
    -extensions v3_req \
    -extfile "${EXT_FILE}" 2>/dev/null

  success "TLS certificate generated (*.${DOMAIN})"
fi

# ── Step 3: Store TLS cert as Kubernetes secrets ──────────────────────────────
info "Storing TLS certificates as Kubernetes secrets..."
for ns in "${TLS_NAMESPACES[@]}"; do
  SECRET_NAME="cloudforge-tls"

  if kubectl get secret "${SECRET_NAME}" -n "${ns}" &>/dev/null; then
    warn "Secret ${SECRET_NAME} already exists in ${ns} — skipping"
  else
    kubectl create secret tls "${SECRET_NAME}" \
      --cert="${TLS_CERT}" \
      --key="${TLS_KEY}" \
      -n "${ns}" \
      --dry-run=client -o yaml | kubectl apply -f -
    success "TLS secret created in namespace ${ns}"
  fi

  CA_SECRET_NAME="cloudforge-ca"
  if kubectl get secret "${CA_SECRET_NAME}" -n "${ns}" &>/dev/null; then
    warn "Secret ${CA_SECRET_NAME} already exists in ${ns} — skipping"
  else
    kubectl create secret generic "${CA_SECRET_NAME}" \
      --from-file=ca.crt="${CA_CERT}" \
      -n "${ns}" \
      --dry-run=client -o yaml | kubectl apply -f -
    success "CA secret created in namespace ${ns}"
  fi
done

# ── Step 4: Generate admin password ───────────────────────────────────────────
ADMIN_CREDS_FILE="${DEV_DIR}/admin-credentials"

if [[ -f "${ADMIN_CREDS_FILE}" ]]; then
  warn "Admin credentials already exist at ${ADMIN_CREDS_FILE} — skipping generation"
  ADMIN_PASSWORD=$(grep "^password=" "${ADMIN_CREDS_FILE}" | cut -d= -f2)
else
  info "Generating initial admin credentials..."
  ADMIN_PASSWORD=$(openssl rand -base64 24 | tr -dc 'a-zA-Z0-9' | head -c 32)
  cat > "${ADMIN_CREDS_FILE}" <<EOF
# CloudForge dev admin credentials — DO NOT COMMIT
username=admin
password=${ADMIN_PASSWORD}
EOF
  chmod 600 "${ADMIN_CREDS_FILE}"
  success "Admin credentials written to ${ADMIN_CREDS_FILE}"
fi

# ── Step 5: Store admin password as Kubernetes secret ────────────────────────
SECRET_NAME="cf-admin-credentials"
if kubectl get secret "${SECRET_NAME}" -n cf-system &>/dev/null; then
  warn "Secret ${SECRET_NAME} already exists in cf-system — skipping"
else
  kubectl create secret generic "${SECRET_NAME}" \
    --from-literal=username=admin \
    --from-literal=password="${ADMIN_PASSWORD}" \
    -n cf-system \
    --dry-run=client -o yaml | kubectl apply -f -
  success "Admin credentials secret created in cf-system"
fi

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}╔══════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║        CloudForge dev environment ready              ║${NC}"
echo -e "${GREEN}╚══════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "  Kubeconfig:    ${CYAN}${KUBECONFIG_PATH}${NC}"
echo -e "  TLS cert:      ${CYAN}${TLS_CERT}${NC}  (*.${DOMAIN})"
echo -e "  Admin creds:   ${CYAN}${ADMIN_CREDS_FILE}${NC}"
echo ""
echo -e "  To use this cluster:"
echo -e "    ${YELLOW}export KUBECONFIG=\$(pwd)/${KUBECONFIG_PATH}${NC}"
echo -e "    ${YELLOW}kubectl get ns${NC}"
echo ""
echo -e "  Add to /etc/hosts for local DNS:"
echo -e "    ${YELLOW}echo '127.0.0.1  cloudforge.local *.cloudforge.local' | sudo tee -a /etc/hosts${NC}"
echo ""
