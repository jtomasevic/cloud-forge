package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jtomasevic/cloud-forge/internal/accounts"
)

// appConfig holds all runtime configuration for cf-accounts.
// Values are sourced exclusively from environment variables via configFromEnv.
type appConfig struct {
	OpenBaoAddr  string
	OpenBaoToken string
	ListenAddr   string
	Scylla       accounts.Config
}

// configFromEnv reads all configuration from environment variables.
// Returns an error for any value that fails to parse.
//
// Environment variables:
//
//	SCYLLA_HOSTS   Comma-separated contact points  (default: 127.0.0.1)
//	SCYLLA_PORT    CQL native transport port        (default: 19042)
//	SCYLLA_USER    ScyllaDB username (optional)
//	SCYLLA_PASS    ScyllaDB password (optional)
//	OPENBAO_ADDR   OpenBao API address              (default: http://localhost:8200)
//	OPENBAO_TOKEN  OpenBao root/service token       (default: dev-root-token)
//	LISTEN_ADDR    HTTP bind address                (default: :8082)
func configFromEnv() (appConfig, error) {
	hosts := os.Getenv("SCYLLA_HOSTS")
	if hosts == "" {
		hosts = "127.0.0.1"
	}

	port := 19042
	if p := os.Getenv("SCYLLA_PORT"); p != "" {
		if _, err := fmt.Sscanf(p, "%d", &port); err != nil {
			return appConfig{}, fmt.Errorf("invalid SCYLLA_PORT %q: %w", p, err)
		}
	}

	baoAddr := os.Getenv("OPENBAO_ADDR")
	if baoAddr == "" {
		baoAddr = "http://localhost:8200"
	}
	baoToken := os.Getenv("OPENBAO_TOKEN")
	if baoToken == "" {
		baoToken = "dev-root-token" //nolint:gosec // default token for local dev only; production uses OPENBAO_TOKEN env var
	}

	listenAddr := os.Getenv("LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":8082"
	}

	return appConfig{
		Scylla: accounts.Config{
			Hosts:          strings.Split(hosts, ","),
			Port:           port,
			Username:       os.Getenv("SCYLLA_USER"),
			Password:       os.Getenv("SCYLLA_PASS"),
			ConnectTimeout: 10 * time.Second,
			QueryTimeout:   5 * time.Second,
		},
		OpenBaoAddr:  baoAddr,
		OpenBaoToken: baoToken,
		ListenAddr:   listenAddr,
	}, nil
}
