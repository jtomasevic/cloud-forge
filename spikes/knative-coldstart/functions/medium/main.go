// Package main is the CloudForge spike "medium" Knative function variant.
//
// It embeds a synthetic 50 MB binary payload to simulate a real-world function
// that carries a small ML model, a pre-trained embedding index, or a native
// shared library. The embedding is resolved at compile time by the Go toolchain,
// producing a binary that is ~50 MB larger than the minimal variant.
//
// # Purpose
//
// Used by the cold-start benchmark (cmd/measure) to measure the impact of
// image size on scale-to-zero latency.  The extra 50 MB adds to the image
// layer pull time on first cold start when the image is not yet in the node cache.
//
// # Expected container image size
//
// ~100 MB (50 MB payload + Go binary + ubuntu:22.04 base layers).
//
// # Pre-requisites
//
// The file functions/medium/payload.bin must exist before building this binary.
// A 1-byte placeholder is committed in the repository.  Run:
//
//	make gen-payloads
//
// to replace it with the real 50 MB payload before deploying to a cluster.
//
// # Environment variables
//
//   - PORT (default "8080"): HTTP listen port.
package main

import (
	_ "embed"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"
)

// payload is the synthetic 50 MB binary embedded at compile time.
//
// The //go:embed directive reads functions/medium/payload.bin at build time.
// Accessing len(payload) at runtime prevents the compiler from optimising the
// embed away — the data occupies real process memory and appears in the image layer.
//
//go:embed payload.bin
var payload []byte

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Log the payload length on startup so operators can confirm the embed worked.
	logger.Info("medium function starting",
		"addr", ":"+port,
		"payload_bytes", len(payload),
	)

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handlerStart := time.Now()

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Variant", "medium")

		// Report the payload length so the benchmark tool can verify
		// the embed is present and correctly sized.
		fmt.Fprintf(w, "variant=medium payload_bytes=%d method=%s path=%s\n",
			len(payload), r.Method, r.URL.Path)

		logger.Info("request handled",
			"variant", "medium",
			"method", r.Method,
			"path", r.URL.Path,
			"handler_us", time.Since(handlerStart).Microseconds(),
		)
	})

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		logger.Error("server exited", "error", err)
		os.Exit(1)
	}
}
