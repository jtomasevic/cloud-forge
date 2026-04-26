# `internal/config`

Viper-based configuration loader for all CloudForge services. Merges defaults, a YAML file, environment variables, and Kubernetes secret files into a typed, validated Go struct in a single call.

---

## File layout

| File | Contents |
|------|----------|
| `config.go` | `Loader[T]` interface, `Options` type |
| `loader.go` | `Load[T]`, `MustLoad[T]`, Viper wiring, secret-file reader |

---

## Loading order (later overrides earlier)

| Priority | Source | Notes |
|----------|--------|-------|
| 1 (lowest) | Struct defaults passed to `Load` | Zero values are skipped |
| 2 | YAML config file | Path from `CF_CONFIG_FILE` env var or `./config.yaml` |
| 3 | Environment variables | Prefix `CF_`, e.g. `CF_SERVER_PORT` → `server.port` |
| 4 (highest) | Kubernetes secret files | Directory from `CF_SECRETS_DIR`; filename = key, content = value |

---

## Quick start

### 1. Define your config struct

```go
type ServiceConfig struct {
    Server struct {
        Host string `mapstructure:"host" validate:"required"`
        Port int    `mapstructure:"port" validate:"required,min=1,max=65535"`
    } `mapstructure:"server"`

    OTLPEndpoint string `mapstructure:"otlp_endpoint"`
    LogLevel     string `mapstructure:"log_level"`
}
```

**Tag rules:**
- `mapstructure:"key"` — maps the struct field to a config key (and its `CF_KEY` env var).
- `validate:"..."` — constraints checked after loading; any failure returns a descriptive error.

### 2. Load in `main()`

```go
func main() {
    cfg := config.MustLoad(ServiceConfig{
        Server: struct{Host string; Port int}{Host: "0.0.0.0", Port: 8080},
        LogLevel: "info",
    })
    // cfg.Server.Port is 8080 unless overridden by CF_SERVER_PORT or config.yaml
}
```

`MustLoad` panics on error — appropriate for service startup where misconfiguration must prevent the service from running. Use `Load` in tests or library code where you want to handle errors explicitly.

---

## Environment variable mapping

The `CF_` prefix is stripped and the remaining name is lowercased with underscores mapped to dots for nested structs:

| Env var | Config key | Struct field |
|---------|-----------|--------------|
| `CF_SERVER_PORT` | `server.port` | `Server.Port` |
| `CF_SERVER_HOST` | `server.host` | `Server.Host` |
| `CF_LOG_LEVEL` | `log_level` | `LogLevel` |
| `CF_OTLP_ENDPOINT` | `otlp_endpoint` | `OTLPEndpoint` |

---

## YAML config file

```yaml
# config.yaml
server:
  host: "0.0.0.0"
  port: 8080

log_level: "info"
otlp_endpoint: "otel-collector:4317"
```

The file path is resolved in this order:
1. `Options.ConfigFile` (passed programmatically)
2. `CF_CONFIG_FILE` environment variable
3. `./config.yaml` (default)

A missing file is **not** an error — the service runs using defaults and env vars.

---

## Kubernetes secret files

When the `CF_SECRETS_DIR` environment variable (or `Options.SecretsDir`) points to a directory, every regular file in that directory is read as a config value:

```
/run/secrets/
  database_password    ← key: "database_password", value: file contents (trimmed)
  jwt_signing_key      ← key: "jwt_signing_key"
```

Files starting with `.` and subdirectories are ignored. The trailing newline that Kubernetes appends when mounting secrets is automatically stripped.

---

## Options

```go
cfg, err := config.Load(defaults, config.Options{
    ConfigFile: "/etc/myservice/config.yaml", // override config file path
    SecretsDir: "/run/secrets",               // override secrets directory
    EnvPrefix:  "MYSERVICE",                  // override env prefix (default: "CF")
})
```

---

## Validation

Add `validate` struct tags to enforce constraints after loading:

```go
type Config struct {
    Port    int    `mapstructure:"port"     validate:"required,min=1,max=65535"`
    DSN     string `mapstructure:"dsn"      validate:"required,url"`
    Region  string `mapstructure:"region"   validate:"oneof=us-east-1 eu-west-1"`
}
```

The full tag syntax is documented in [go-playground/validator](https://pkg.go.dev/github.com/go-playground/validator/v10).

---

## Error handling

```go
cfg, err := config.Load(defaults)
if err != nil {
    // err.Error() describes exactly which field failed and why, e.g.:
    // "config: validation failed: Key: 'Config.Port' Error: Field validation
    //  for 'Port' failed on the 'required' tag"
    log.Fatal(err)
}
```
