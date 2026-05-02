package main

import (
	"testing"
)

func TestConfigFromEnv_Defaults(t *testing.T) {
	// Ensure no env vars leak from the environment.
	unsetAll(t)

	cfg, err := configFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Scylla.Hosts) != 1 || cfg.Scylla.Hosts[0] != "127.0.0.1" {
		t.Errorf("hosts: got %v", cfg.Scylla.Hosts)
	}
	if cfg.Scylla.Port != 19042 {
		t.Errorf("port: got %d, want 19042", cfg.Scylla.Port)
	}
	if cfg.OpenBaoAddr != "http://localhost:8200" {
		t.Errorf("openbao_addr: got %q", cfg.OpenBaoAddr)
	}
	if cfg.OpenBaoToken != "dev-root-token" {
		t.Errorf("openbao_token: got %q", cfg.OpenBaoToken)
	}
	if cfg.ListenAddr != ":8082" {
		t.Errorf("listen_addr: got %q", cfg.ListenAddr)
	}
}

func TestConfigFromEnv_CustomValues(t *testing.T) {
	unsetAll(t)
	t.Setenv("SCYLLA_HOSTS", "10.0.0.1,10.0.0.2")
	t.Setenv("SCYLLA_PORT", "9042")
	t.Setenv("SCYLLA_USER", "cfuser")
	t.Setenv("SCYLLA_PASS", "cfpass")
	t.Setenv("OPENBAO_ADDR", "http://openbao:8200")
	t.Setenv("OPENBAO_TOKEN", "my-token")
	t.Setenv("LISTEN_ADDR", ":9090")

	cfg, err := configFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Scylla.Hosts) != 2 {
		t.Errorf("hosts: got %v, want 2 entries", cfg.Scylla.Hosts)
	}
	if cfg.Scylla.Port != 9042 {
		t.Errorf("port: got %d, want 9042", cfg.Scylla.Port)
	}
	if cfg.Scylla.Username != "cfuser" {
		t.Errorf("username: got %q", cfg.Scylla.Username)
	}
	if cfg.Scylla.Password != "cfpass" {
		t.Errorf("password: got %q", cfg.Scylla.Password)
	}
	if cfg.OpenBaoAddr != "http://openbao:8200" {
		t.Errorf("openbao_addr: got %q", cfg.OpenBaoAddr)
	}
	if cfg.OpenBaoToken != "my-token" {
		t.Errorf("openbao_token: got %q", cfg.OpenBaoToken)
	}
	if cfg.ListenAddr != ":9090" {
		t.Errorf("listen_addr: got %q", cfg.ListenAddr)
	}
}

func TestConfigFromEnv_InvalidPort(t *testing.T) {
	unsetAll(t)
	t.Setenv("SCYLLA_PORT", "not-a-number")

	_, err := configFromEnv()
	if err == nil {
		t.Fatal("expected error for invalid port, got nil")
	}
}

// unsetAll removes all env vars that configFromEnv reads so that defaults kick in.
func unsetAll(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"SCYLLA_HOSTS", "SCYLLA_PORT", "SCYLLA_USER", "SCYLLA_PASS",
		"OPENBAO_ADDR", "OPENBAO_TOKEN", "LISTEN_ADDR",
	} {
		t.Setenv(key, "") // t.Setenv restores on cleanup; setting "" == absent
	}
}
