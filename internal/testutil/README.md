# `internal/testutil`

Testcontainer helpers for CloudForge integration tests. Each helper starts a real Docker container, returns a pre-configured client, and registers automatic cleanup with `t.Cleanup`.

---

## File layout

| File | Container | Returns |
|------|-----------|---------|
| `postgres.go` | PostgreSQL 16 + pgvector | `*pgxpool.Pool` |
| `nats.go` | NATS 2.10 with JetStream | `*nats.Conn` |
| `minio.go` | MinIO | `*minio.Client` |
| `openbao.go` | OpenBao (Vault-compatible) | `*openbao.Client` |
| `opa.go` | Open Policy Agent | base URL `string` |

---

## Build tag

All files carry `//go:build integration`. Test files that use this package must also carry the tag so they are excluded from the CI unit-test job:

```go
//go:build integration

package mypackage_test
```

Run integration tests explicitly:

```bash
go test -tags integration ./...

# Or via Taskfile:
task test:integration
```

---

## Prerequisites

- Docker must be running on the host (or in CI via a Docker-in-Docker service).
- The `DOCKER_HOST` / `TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE` variables are respected if your Docker socket is in a non-standard location.

---

## PostgreSQL

```go
//go:build integration

func TestMyFeature(t *testing.T) {
    pool, cleanup := testutil.StartPostgres(t)
    defer cleanup()

    // pool is a *pgxpool.Pool connected to a fresh "testdb" database.
    row := pool.QueryRow(context.Background(), "SELECT 1")
    var n int
    require.NoError(t, row.Scan(&n))
    assert.Equal(t, 1, n)
}
```

**Container details:**

| Setting | Value |
|---------|-------|
| Image | `pgvector/pgvector:pg16` (PostgreSQL 16 + pgvector extension) |
| Database | `testdb` |
| User | `testuser` |
| Password | `testpassword` |
| Port | random (mapped by Docker) |

The pgvector extension enables vector similarity search tests without additional setup. The pool is verified with a `Ping` before being returned.

---

## NATS (JetStream)

```go
//go:build integration

func TestEventStreaming(t *testing.T) {
    nc, cleanup := testutil.StartNATS(t)
    defer cleanup()

    // nc is a *nats.Conn with JetStream enabled.
    js, err := nc.JetStream()
    require.NoError(t, err)

    // Create a stream:
    _, err = js.AddStream(&nats.StreamConfig{
        Name:     "EVENTS",
        Subjects: []string{"events.>"},
    })
    require.NoError(t, err)
}
```

**Container details:**

| Setting | Value |
|---------|-------|
| Image | `nats:2.10-alpine` |
| JetStream | Enabled via `-js` flag |
| Port | random (mapped by Docker) |

The connection is configured with `MaxReconnects(-1)` to avoid test flakiness during short container startup delays. Cleanup calls `Drain()` to ensure in-flight messages are processed before the connection closes.

---

## MinIO

```go
//go:build integration

func TestObjectStorage(t *testing.T) {
    client, cleanup := testutil.StartMinIO(t)
    defer cleanup()

    ctx := context.Background()

    // Create a bucket:
    err := client.MakeBucket(ctx, "test-bucket", minio.MakeBucketOptions{})
    require.NoError(t, err)

    // Upload an object:
    _, err = client.PutObject(ctx, "test-bucket", "hello.txt",
        strings.NewReader("hello world"), 11,
        minio.PutObjectOptions{ContentType: "text/plain"})
    require.NoError(t, err)
}
```

**Container details:**

| Setting | Value |
|---------|-------|
| Image | `minio/minio:RELEASE.2024-01-01T00-00-00Z` |
| Access key | `minio-test-access` |
| Secret key | `minio-test-secret` |
| TLS | Disabled (plain HTTP within the test network) |

---

## OpenBao (Vault-compatible secrets)

```go
//go:build integration

func TestSecretStorage(t *testing.T) {
    client, cleanup := testutil.StartOpenBao(t)
    defer cleanup()

    ctx := context.Background()

    // Write a secret:
    _, err := client.KVv2("secret").Put(ctx, "myapp/config", map[string]interface{}{
        "db_password": "s3cr3t",
    })
    require.NoError(t, err)

    // Read the secret back:
    secret, err := client.KVv2("secret").Get(ctx, "myapp/config")
    require.NoError(t, err)
    assert.Equal(t, "s3cr3t", secret.Data["db_password"])
}
```

**Container details:**

| Setting | Value |
|---------|-------|
| Image | `quay.io/openbao/openbao:latest` |
| Mode | Dev (unsealed, in-memory, no persistence) |
| Root token | `root-test-token` |
| API port | random (mapped by Docker) |

Dev mode starts with all secret engines pre-mounted (KV, PKI, Transit, etc.). The root token is used for test authentication — in production, services use short-lived AppRole or Kubernetes auth tokens.

---

## OPA (Open Policy Agent)

```go
//go:build integration

func TestPolicyEvaluation(t *testing.T) {
    baseURL, cleanup := testutil.StartOPA(t)
    defer cleanup()

    ctx := context.Background()

    // Upload a Rego policy:
    policy := `package authz
    default allow := false
    allow if input.user == "alice"`

    req, _ := http.NewRequestWithContext(ctx, http.MethodPut,
        baseURL+"/v1/policies/authz", strings.NewReader(policy))
    req.Header.Set("Content-Type", "text/plain")
    resp, err := http.DefaultClient.Do(req)
    require.NoError(t, err)
    require.Equal(t, http.StatusOK, resp.StatusCode)

    // Evaluate the policy:
    body := `{"input": {"user": "alice"}}`
    req, _ = http.NewRequestWithContext(ctx, http.MethodPost,
        baseURL+"/v1/data/authz/allow", strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    resp, err = http.DefaultClient.Do(req)
    require.NoError(t, err)
    require.Equal(t, http.StatusOK, resp.StatusCode)
}
```

**Container details:**

| Setting | Value |
|---------|-------|
| Image | `openpolicyagent/opa:latest` |
| API port | random (mapped by Docker) |
| Auth | None (anonymous mode for tests) |
| Log level | `error` (suppresses verbose OPA logs in test output) |

---

## Automatic cleanup

Every helper registers a `t.Cleanup` callback so containers are always stopped when the test ends — even if the test panics or the caller forgets to call the returned `cleanup` function:

```go
// Both patterns are equivalent — the t.Cleanup is always registered:
pool, cleanup := testutil.StartPostgres(t)
defer cleanup()  // explicit defer is redundant but harmless and documents intent

// Or rely solely on t.Cleanup (simpler for table-driven tests):
pool, _ := testutil.StartPostgres(t)
```
