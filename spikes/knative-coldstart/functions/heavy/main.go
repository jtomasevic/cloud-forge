// Package main is the CloudForge spike "heavy" Knative function variant.
//
// It embeds a synthetic 200 MB binary payload to simulate a function that
// bundles a large model checkpoint, a full ONNX runtime library, or similar
// heavyweight asset.  This is the worst-case scenario for cold-start latency.
//
// # Purpose
//
// Used by the cold-start benchmark (cmd/measure) to establish the upper bound
// of scale-to-zero latency.  The findings from this variant directly inform
// the platform's minimum-replica guidance for heavy ML functions.
//
// # Expected container image size
//
// ~500 MB (200 MB payload + Go binary + ubuntu:22.04 base layers).
//
// # Pre-requisites
//
// Run `make gen-payloads` before building or deploying this function.
// See functions/medium/main.go for more details.
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

// payload is the synthetic 200 MB binary embedded at compile time.
//
// Replace the 1-byte placeholder by running `make gen-payloads` before deploying.
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

	logger.Info("heavy function starting",
		"addr", ":"+port,
		"payload_bytes", len(payload),
	)

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handlerStart := time.Now()

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Variant", "heavy")

		fmt.Fprintf(w, "variant=heavy payload_bytes=%d method=%s path=%s\n",
			len(payload), r.Method, r.URL.Path)

		logger.Info("request handled",
			"variant", "heavy",
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
