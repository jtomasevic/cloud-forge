package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigFromEnv_Defaults(t *testing.T) {
	t.Setenv("SCYLLA_HOSTS", "")
	t.Setenv("SCYLLA_PORT", "")
	t.Setenv("OPENBAO_ADDR", "")
	t.Setenv("OPENBAO_TOKEN", "")
	t.Setenv("LISTEN_ADDR", "")
	t.Setenv("SCYLLA_USER", "")
	t.Setenv("SCYLLA_PASS", "")

	cfg, err := configFromEnv()

	require.NoError(t, err)
	assert.Equal(t, []string{"127.0.0.1"}, cfg.Scylla.Hosts)
	assert.Equal(t, 19042, cfg.Scylla.Port)
	assert.Equal(t, "http://localhost:8200", cfg.OpenBaoAddr)
	assert.Equal(t, "dev-root-token", cfg.OpenBaoToken)
	assert.Equal(t, ":8080", cfg.ListenAddr)
	assert.Empty(t, cfg.Scylla.Username)
	assert.Empty(t, cfg.Scylla.Password)
}

func TestConfigFromEnv_CustomValues(t *testing.T) {
	t.Setenv("SCYLLA_HOSTS", "10.0.0.1,10.0.0.2")
	t.Setenv("SCYLLA_PORT", "9042")
	t.Setenv("SCYLLA_USER", "admin")
	t.Setenv("SCYLLA_PASS", "s3cret")
	t.Setenv("OPENBAO_ADDR", "http://openbao.cf-security:8200")
	t.Setenv("OPENBAO_TOKEN", "prod-token")
	t.Setenv("LISTEN_ADDR", ":9090")

	cfg, err := configFromEnv()

	require.NoError(t, err)
	assert.Equal(t, []string{"10.0.0.1", "10.0.0.2"}, cfg.Scylla.Hosts)
	assert.Equal(t, 9042, cfg.Scylla.Port)
	assert.Equal(t, "admin", cfg.Scylla.Username)
	assert.Equal(t, "s3cret", cfg.Scylla.Password)
	assert.Equal(t, "http://openbao.cf-security:8200", cfg.OpenBaoAddr)
	assert.Equal(t, "prod-token", cfg.OpenBaoToken)
	assert.Equal(t, ":9090", cfg.ListenAddr)
}

func TestConfigFromEnv_InvalidPort_ReturnsError(t *testing.T) {
	t.Setenv("SCYLLA_PORT", "not-a-number")

	_, err := configFromEnv()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid SCYLLA_PORT")
}

func TestConfigFromEnv_TimeoutsArePositive(t *testing.T) {
	t.Setenv("SCYLLA_HOSTS", "")
	t.Setenv("SCYLLA_PORT", "")

	cfg, err := configFromEnv()

	require.NoError(t, err)
	assert.Greater(t, cfg.Scylla.ConnectTimeout.Milliseconds(), int64(0))
	assert.Greater(t, cfg.Scylla.QueryTimeout.Milliseconds(), int64(0))
}
