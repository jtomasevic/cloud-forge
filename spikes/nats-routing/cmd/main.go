// Package main is the entry point for Spike 0.6 — NATS JetStream Multi-Tenant
// Routing.
//
// All business logic lives in
// [github.com/jtomasevic/cloud-forge/spikes/nats-routing/internal/spike].
// This file is intentionally minimal so it stays out of the coverage picture.
//
// USAGE
//
//	docker compose -f config/nats-cluster.yaml up -d
//	go run ./cmd
//	# or:
//	NATS_URL=nats://localhost:4223 go run ./cmd   # connect to node 2
package main

import (
	"log/slog"
	"os"
	"time"

	"github.com/jtomasevic/cloud-forge/spikes/nats-routing/internal/spike"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	url := spike.GetEnvOrDefault("NATS_URL", spike.DefaultNATSURL)

	// ConfigPath resolves config/nats.conf relative to the source tree.
	// Pass it to RunWithTimeout so the config-reload demo can locate the file.
	spike.RunWithTimeout(10*time.Minute, url, spike.ConfigPath(), logger)
}
