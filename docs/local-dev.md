# CloudForge — Local Development Guide

This guide walks you through setting up a full CloudForge development environment on your local machine using k3d (Kubernetes in Docker).

---

## Prerequisites

Install the following tools before proceeding.

| Tool | Min version | Install |
|---|---|---|
| Docker Desktop (or Colima) | 24.x | https://www.docker.com/products/docker-desktop |
| k3d | 5.7+ | `brew install k3d` |
| kubectl | 1.29+ | `brew install kubectl` |
| Helm | 3.14+ | `brew install helm` |
| Go | 1.26+ | `brew install go` |
| Task | 3.x | `brew install go-task` |
| openssl | system | pre-installed on macOS |

Or run `make tools-check` which will verify all of the above and install any missing tools via Homebrew on macOS.

---

## Quick Start

```bash
# 1. Clone the repo (if you haven't already)
git clone git@github.com:jtomasevic/cloud-forge.git
cd cloud-forge

# 2. Verify / install all required tools
make tools-check

# 3. Start the cluster and bootstrap the environment
make dev-up

# 4. Point kubectl at the dev cluster
export KUBECONFIG=$(pwd)/.dev/kubeconfig

# 5. Verify all namespaces are present
kubectl get ns
```

Expected output from `kubectl get ns`:

```
NAME               STATUS   AGE
cf-system          Active   ...
cf-identity        Active   ...
cf-data            Active   ...
cf-events          Active   ...
cf-compute         Active   ...
cf-observability   Active   ...
cf-gateway         Active   ...
```

---

## What `make dev-up` Does

1. **`make tools-check`** — verifies required tools are installed
2. **`k3d cluster create`** — creates a 3-node k3d cluster (`cloudforge-dev`) from `deploy/k3d/cluster.yaml`
3. **`kubectl apply -k deploy/kustomize/base/`** — creates all platform namespaces, service accounts, and resource quotas
4. **`scripts/dev-bootstrap.sh`** — generates TLS certificates, stores them as Kubernetes secrets, creates an initial admin password

---

## Cluster Details

| Property | Value |
|---|---|
| Cluster name | `cloudforge-dev` |
| Nodes | 1 server + 2 agents |
| Kubernetes version | v1.31 |
| HTTP port | `localhost:8080` → cluster port 80 |
| HTTPS port | `localhost:8443` → cluster port 443 |
| NATS port | `localhost:4222` |
| Ingress controller | Contour (Traefik disabled) |
| kubeconfig path | `.dev/kubeconfig` |

---

## Local DNS

Services are exposed under `*.cloudforge.local`. Add the following to your `/etc/hosts`:

```bash
echo '127.0.0.1  cloudforge.local api.cloudforge.local grafana.cloudforge.local' | sudo tee -a /etc/hosts
```

---

## Bootstrap Outputs

After `make dev-up`, the `.dev/` directory is created (gitignored):

```
.dev/
├── kubeconfig          # kubectl config for the dev cluster
├── admin-credentials   # admin username + password (plaintext, local only)
└── certs/
    ├── ca.crt          # self-signed CA certificate
    ├── ca.key          # CA private key
    ├── tls.crt         # wildcard cert for *.cloudforge.local
    └── tls.key         # TLS private key
```

The TLS cert is also stored as a Kubernetes secret (`cloudforge-tls`) in the `cf-gateway` and `cf-system` namespaces.

---

## Day-to-Day Commands

```bash
# Start cluster
make dev-up

# Stop and delete cluster
make dev-down

# Full reset (destroy + recreate)
make dev-reset

# Check cluster and pod status
make dev-status

# Deploy a specific CloudForge component (once Helm charts exist)
make deploy-component SERVICE=cf-iam

# Run unit tests
make test-unit

# Run linter
make lint

# Build all binaries
make build
```

---

## GPU / AI Development

The local dev cluster does **not** require a GPU. When running AI inference locally:

- **Ollama (CPU mode)** is used as a drop-in substitute for vLLM
- The same OpenAI-compatible API (`/v1/chat/completions`) is served on CPU
- GPU-accelerated inference is validated separately in Spike 0.9 against a cloud GPU node

---

## Troubleshooting

### `k3d cluster create` fails with "port already in use"

Port 8080 or 8443 is occupied by another process.

```bash
# Find what is using the port
lsof -i :8080
lsof -i :8443
# Stop the conflicting process, then retry make dev-up
```

### `kubectl` commands return "connection refused"

The kubeconfig is not set.

```bash
export KUBECONFIG=$(pwd)/.dev/kubeconfig
kubectl get ns
```

### Bootstrap script fails: "Cluster is not running"

The `dev-bootstrap.sh` script runs after the cluster is created. If you run it manually before `k3d cluster create`, it will fail. Always use `make dev-up` rather than running steps individually.

### Resetting everything

```bash
make dev-reset   # destroys cluster and recreates from scratch
```

Note: `make dev-reset` does **not** delete the `.dev/` directory. Certs and credentials are preserved across resets. To also regenerate certs and credentials:

```bash
make dev-down
rm -rf .dev/
make dev-up
```

---

## Adding a New Namespace (Tenant)

Tenant namespaces (`cf-tenant-*`) are created dynamically by CF-ResourceController when a tenant is onboarded. Do not add them here manually. For testing purposes during development:

```bash
kubectl create namespace cf-tenant-acme
kubectl label namespace cf-tenant-acme \
  cloudforge.io/tier=tenant \
  cloudforge.io/tenant=acme
```
