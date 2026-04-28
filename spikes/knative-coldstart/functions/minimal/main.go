// Package main is the CloudForge spike "minimal" Knative function variant.
//
// It is the lightest possible Go HTTP server — no embedded assets, no
// dependencies beyond the standard library — and is built on a
// gcr.io/distroless/static-debian12 base image via ko.
//
// # Purpose
//
// This binary is deployed as a Knative Service with scale-to-zero enabled.
// The cold-start benchmark tool (cmd/measure) measures the time from the
// first request to the moment this handler writes its response.
//
// # Expected container image size
//
// < 10 MB (ko static binary + distroless base layer).
//
// # Environment variables
//
//   - PORT (default "8080"): the port the HTTP server binds to.
//     Knative injects PORT automatically; this default is for local testing.
package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"
)

func main() {
	// Use structured logging so Knative log collectors can parse the output.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Knative injects PORT into the container environment.
	// Fall back to 8080 for local `go run` usage.
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()

	// Root handler: echoes the request method and path so the benchmark
	// tool can confirm it received a real response from this function.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Record the wall-clock time at which the first byte is written.
		// Knative measures from "first request" to "pod ready" but we also
		// want to see handler execution time in the logs.
		handlerStart := time.Now()

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Variant", "minimal")
		fmt.Fprintf(w, "variant=minimal method=%s path=%s\n", r.Method, r.URL.Path)

		logger.Info("request handled",
			"variant", "minimal",
			"method", r.Method,
			"path", r.URL.Path,
			"handler_us", time.Since(handlerStart).Microseconds(),
		)
	})

	// Health check endpoint used by Knative probes.
	// Must return 200 OK quickly so the pod is marked Ready and the
	// autoscaler forwards traffic without further delay.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	addr := ":" + port
	logger.Info("minimal function starting", "addr", addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		logger.Error("server exited", "error", err)
		os.Exit(1)
	}
}
