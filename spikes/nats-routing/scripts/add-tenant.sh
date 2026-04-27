#!/usr/bin/env bash
# =============================================================================
# add-tenant.sh — Dynamically provision a new NATS account without restart.
# CloudForge Spike 0.6 — NATS Multi-Tenant Routing
# =============================================================================
#
# USAGE
#   ./scripts/add-tenant.sh <TENANT_ID> <USERNAME> <PASSWORD>
#
# EXAMPLE
#   ./scripts/add-tenant.sh ACME_CORP acme-user acme-pass-secret
#
# WHAT IT DOES
#   1. Reads config/nats.conf.
#   2. Appends a new account block for the given tenant.
#   3. Copies the updated config into the nats-1 Docker container.
#   4. Sends SIGHUP to nats-1, triggering a live config reload.
#   5. Connects via the nats CLI to verify the new account is reachable.
#
# PREREQUISITES
#   - Docker Compose cluster is running  (docker compose -f config/nats-cluster.yaml up -d)
#   - NATS CLI is installed              (brew install nats-io/nats-tools/nats)
#
# NOTES ON SIGHUP RELOAD
#   NATS server reloads accounts in-place on SIGHUP.  Existing connections
#   from other accounts are NOT disrupted.  The server logs "Reloaded server
#   configuration" when the reload completes (typically < 50ms).
# =============================================================================

set -euo pipefail

TENANT_ID="${1:?Usage: $0 <TENANT_ID> <USERNAME> <PASSWORD>}"
USERNAME="${2:?Usage: $0 <TENANT_ID> <USERNAME> <PASSWORD>}"
PASSWORD="${3:?Usage: $0 <TENANT_ID> <USERNAME> <PASSWORD>}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONF_SRC="${SCRIPT_DIR}/../config/nats.conf"
CONF_TMP="$(mktemp /tmp/nats-XXXXXX.conf)"

echo "▶ Adding account ${TENANT_ID} (user: ${USERNAME}) to NATS config…"

# ---------------------------------------------------------------------------
# Append the new account block before the system_account directive so the
# file structure stays valid.  We use a sed address to insert before the
# marker line rather than appending at the end.
# ---------------------------------------------------------------------------
NEW_BLOCK="
  # Dynamically provisioned by add-tenant.sh on $(date -u +%Y-%m-%dT%H:%M:%SZ)
  ${TENANT_ID} {
    users = [{ user: \"${USERNAME}\", password: \"${PASSWORD}\" }]
    jetstream: enabled
  }
"

# Check if the account already exists.
if grep -q "^  ${TENANT_ID} {" "${CONF_SRC}"; then
  echo "⚠  Account ${TENANT_ID} already exists in nats.conf — skipping add."
else
  # Insert the new block immediately before the `system_account:` line.
  sed "/^# Designate the system account/i\\${NEW_BLOCK}" "${CONF_SRC}" > "${CONF_TMP}"
  cp "${CONF_TMP}" "${CONF_SRC}"
  echo "✓ nats.conf updated."
fi

rm -f "${CONF_TMP}"

# ---------------------------------------------------------------------------
# Copy the updated config into all running nats containers and reload each.
# ---------------------------------------------------------------------------
CONTAINERS=("nats-1" "nats-2" "nats-3")
for CONTAINER in "${CONTAINERS[@]}"; do
  if docker inspect --format='{{.State.Running}}' "${CONTAINER}" 2>/dev/null | grep -q "true"; then
    echo "▶ Copying config to ${CONTAINER}…"
    docker cp "${CONF_SRC}" "${CONTAINER}:/config/nats.conf"
    echo "▶ Sending SIGHUP to ${CONTAINER}…"
    docker kill --signal=HUP "${CONTAINER}"
    echo "✓ ${CONTAINER} reloaded."
  else
    echo "⚠  ${CONTAINER} is not running — skipping."
  fi
done

# Brief wait for reload propagation.
sleep 0.5

# ---------------------------------------------------------------------------
# Verify the new account is reachable.
# ---------------------------------------------------------------------------
echo "▶ Verifying connectivity as ${USERNAME}…"
if nats pub --server="nats://localhost:4222" \
            --user="${USERNAME}" --password="${PASSWORD}" \
            "probe.$(date +%s)" "ping" 2>/dev/null; then
  echo "✓ Account ${TENANT_ID} is live — dynamic provisioning succeeded."
else
  echo "✗ Could not connect as ${USERNAME}. Check the NATS server logs:"
  echo "  docker logs nats-1 --tail 50"
  exit 1
fi
