# ── Build stage ──────────────────────────────────────────────────────────────
# The medium variant embeds payload.bin at compile time via //go:embed.
# The payload file must exist before running docker build (run make gen-payloads first).
FROM golang:1.26 AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# payload.bin is embedded into the binary by the //go:embed directive in
# functions/medium/main.go — the resulting binary is ~50 MB larger than minimal.
ARG GOARCH=arm64
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${GOARCH} \
    go build -trimpath -ldflags="-s -w" \
    -o /bin/fn-medium \
    ./functions/medium

# ── Runtime stage ─────────────────────────────────────────────────────────────
# Use ubuntu base (instead of distroless) to reach the ~100 MB target image size.
# The payload is already embedded in the binary; the ubuntu layer adds ~80 MB.
FROM ubuntu:22.04
RUN apt-get clean && rm -rf /var/lib/apt/lists/*
COPY --from=builder /bin/fn-medium /fn-medium
EXPOSE 8080
CMD ["/fn-medium"]
