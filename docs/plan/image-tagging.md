# CloudForge — Image Tagging Strategy

All CloudForge service container images are published to **GitHub Container Registry**:

```
ghcr.io/jtomasevic/cloud-forge/<service>:<tag>
```

---

## Tag Formats

| Trigger | Tag format | Example |
|---|---|---|
| Push to `main` | `main-<YYYYMMDD>-<sha7>` | `main-20260426-a1b2c3d` |
| Any commit (secondary) | `sha-<sha7>` | `sha-a1b2c3d` |
| Semver release tag | `v<major>.<minor>.<patch>` | `v0.1.0` |

Every image always receives **two tags** simultaneously: its primary tag (above) plus the short SHA tag. This means you can always pull a specific commit regardless of what triggered the build.

---

## Examples

```bash
# Pull the latest main-branch build of cf-iam
docker pull ghcr.io/jtomasevic/cloud-forge/cf-iam:main-20260426-a1b2c3d

# Pull by exact commit SHA
docker pull ghcr.io/jtomasevic/cloud-forge/cf-iam:sha-a1b2c3d

# Pull a release
docker pull ghcr.io/jtomasevic/cloud-forge/cf-iam:v0.1.0
```

---

## OCI Labels

Every image is labeled with standard OCI annotations:

| Label | Value | Set by |
|---|---|---|
| `org.opencontainers.image.version` | The primary tag (e.g. `v0.1.0`) | `release.yml` |
| `org.opencontainers.image.revision` | Full git SHA (`${{ github.sha }}`) | `release.yml` |
| `org.opencontainers.image.created` | UTC timestamp at build time | `release.yml` |
| `org.opencontainers.image.source` | `https://github.com/jtomasevic/cloud-forge` | `.ko.yaml` |
| `org.opencontainers.image.title` | `cloudforge-<service>` | `Dockerfile.service` |

---

## Tooling

- **CI builds** (`release.yml`) use [`ko`](https://ko.build) — no Dockerfile required for Go-only services. Ko resolves the import path, builds a minimal image on top of `gcr.io/distroless/static:nonroot`, and pushes to GHCR.
- **Local builds** use the same `ko` tooling via `make image-build SERVICE=<name>` or the Dockerfile at `deploy/docker/Dockerfile.service` for services with non-Go assets.

---

## Release Process

1. Merge feature branch → `main` (triggers `main-*` tagged images automatically)
2. When ready to release: create and push a semver git tag:
   ```bash
   git tag v0.1.0
   git push origin v0.1.0
   ```
3. `release.yml` fires, builds all service images tagged `v0.1.0` + `sha-<sha7>`, and creates a GitHub Release with auto-generated release notes.
