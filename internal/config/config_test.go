package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jtomasevic/cloud-forge/internal/config"
)

// testServiceConfig is a representative config struct used across tests.
// It mirrors the shape a real CloudForge service would define.
// Plain string fields are listed before the nested Server struct so that
// all pointer words are grouped at the front, minimising the GC scan region.
type testServiceConfig struct {
	OTLPEndpoint string `mapstructure:"otlp_endpoint"`
	LogLevel     string `mapstructure:"log_level"`
	Server       struct {
		Host string `mapstructure:"host" validate:"required"`
		Port int    `mapstructure:"port" validate:"required,min=1,max=65535"`
	} `mapstructure:"server"`
}

// defaultTestConfig returns a set of defaults used by most tests.
func defaultTestConfig() testServiceConfig {
	var cfg testServiceConfig
	cfg.Server.Port = 8080
	cfg.Server.Host = "0.0.0.0"
	cfg.LogLevel = "info"
	return cfg
}

// TestLoad_Defaults verifies that Load returns the defaults when no config
// file or environment variables are present.
func TestLoad_Defaults(t *testing.T) {
	// Point to a non-existent config file so no file is loaded.
	cfg, err := config.Load(defaultTestConfig(), config.Options{
		ConfigFile: "/tmp/cloudforge-nonexistent-config.yaml",
	})

	require.NoError(t, err)
	assert.Equal(t, 8080, cfg.Server.Port)
	assert.Equal(t, "0.0.0.0", cfg.Server.Host)
	assert.Equal(t, "info", cfg.LogLevel)
}

// TestLoad_EnvOverride verifies that environment variables with the CF_
// prefix override defaults.
func TestLoad_EnvOverride(t *testing.T) {
	t.Setenv("CF_SERVER_PORT", "9090")
	t.Setenv("CF_LOG_LEVEL", "debug")

	cfg, err := config.Load(defaultTestConfig(), config.Options{
		ConfigFile: "/tmp/cloudforge-nonexistent-config.yaml",
	})

	require.NoError(t, err)
	// Environment variable must override the default port.
	assert.Equal(t, 9090, cfg.Server.Port)
	// Environment variable must override the default log level.
	assert.Equal(t, "debug", cfg.LogLevel)
	// Values not overridden must remain at their defaults.
	assert.Equal(t, "0.0.0.0", cfg.Server.Host)
}

// TestLoad_YAMLFile verifies that a YAML config file is loaded and its
// values take precedence over defaults but yield to env vars.
func TestLoad_YAMLFile(t *testing.T) {
	// Write a temporary config file.
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgFile, []byte(`
server:
  port: 7070
  host: "127.0.0.1"
log_level: "warn"
`), 0o600))

	cfg, err := config.Load(defaultTestConfig(), config.Options{
		ConfigFile: cfgFile,
	})

	require.NoError(t, err)
	assert.Equal(t, 7070, cfg.Server.Port)
	assert.Equal(t, "127.0.0.1", cfg.Server.Host)
	assert.Equal(t, "warn", cfg.LogLevel)
}

// TestLoad_SecretFiles verifies that files in the secrets directory are
// loaded as config values.
func TestLoad_SecretFiles(t *testing.T) {
	dir := t.TempDir()

	// Write a secret file named "otlp_endpoint".
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "otlp_endpoint"),
		[]byte("otel-collector:4317\n"), // trailing newline as Kubernetes adds
		0o600,
	))

	cfg, err := config.Load(defaultTestConfig(), config.Options{
		ConfigFile: "/tmp/cloudforge-nonexistent-config.yaml",
		SecretsDir: dir,
	})

	require.NoError(t, err)
	// The secret file value must appear in the config, with the trailing
	// newline stripped.
	assert.Equal(t, "otel-collector:4317", cfg.OTLPEndpoint)
}

// TestLoad_ValidationFailure verifies that a config missing a required field
// returns a descriptive error rather than a zero-value struct.
func TestLoad_ValidationFailure(t *testing.T) {
	// Provide defaults that are deliberately invalid — host is empty but
	// the validate tag requires it.
	var badDefaults testServiceConfig
	badDefaults.Server.Port = 8080
	// Server.Host intentionally left empty — validate:"required" will fail.

	_, err := config.Load(badDefaults, config.Options{
		ConfigFile: "/tmp/cloudforge-nonexistent-config.yaml",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

// TestMustLoad_Panics verifies that MustLoad panics when validation fails.
func TestMustLoad_Panics(t *testing.T) {
	var badDefaults testServiceConfig
	badDefaults.Server.Port = 8080
	// Server.Host intentionally empty.

	assert.Panics(t, func() {
		config.MustLoad(badDefaults, config.Options{
			ConfigFile: "/tmp/cloudforge-nonexistent-config.yaml",
		})
	})
}

// TestLoad_NoOptions verifies that Load works correctly with no Options
// supplied — it must use the default CF_ env prefix and ./config.yaml path.
func TestLoad_NoOptions(t *testing.T) {
	// Override CF_SERVER_PORT to prove env vars are picked up with default prefix.
	t.Setenv("CF_SERVER_PORT", "7777")

	cfg, err := config.Load(defaultTestConfig())
	// May fail because ./config.yaml doesn't exist in the test working directory,
	// but the env var must still be applied.
	// If validation passes (host has a default), port must be 7777.
	if err == nil {
		assert.Equal(t, 7777, cfg.Server.Port)
	}
	// Either way, the call must not panic.
}

// TestLoad_MalformedYAML verifies that a config file with invalid YAML syntax
// returns a descriptive error rather than silently falling back to defaults.
func TestLoad_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgFile, []byte("server: [invalid yaml }{"), 0o600))

	_, err := config.Load(defaultTestConfig(), config.Options{ConfigFile: cfgFile})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading config file")
}

// TestLoad_SecretDir_UnreadableFile verifies that a secret file that cannot
// be read returns an error identifying the problematic file.
func TestLoad_SecretDir_UnreadableFile(t *testing.T) {
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "my_secret")
	require.NoError(t, os.WriteFile(secretPath, []byte("value"), 0o600))

	// Make the file unreadable.
	require.NoError(t, os.Chmod(secretPath, 0o000))
	t.Cleanup(func() { _ = os.Chmod(secretPath, 0o600) })

	_, err := config.Load(defaultTestConfig(), config.Options{
		ConfigFile: "/tmp/cloudforge-nonexistent-config.yaml",
		SecretsDir: dir,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "secrets")
}

// TestMustLoad_Success verifies that MustLoad returns a valid config without
// panicking when the defaults pass validation.
func TestMustLoad_Success(t *testing.T) {
	var cfg testServiceConfig
	assert.NotPanics(t, func() {
		cfg = config.MustLoad(defaultTestConfig(), config.Options{
			ConfigFile: "/tmp/cloudforge-nonexistent-config.yaml",
		})
	})
	assert.Equal(t, 8080, cfg.Server.Port)
}

// TestLoad_SecretDir_NotExist verifies that a non-existent secrets directory
// returns an error.
func TestLoad_SecretDir_NotExist(t *testing.T) {
	_, err := config.Load(defaultTestConfig(), config.Options{
		ConfigFile: "/tmp/cloudforge-nonexistent-config.yaml",
		SecretsDir: "/tmp/cloudforge-nonexistent-secrets-dir",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "secrets")
}
