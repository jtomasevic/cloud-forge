# ── Build stage ──────────────────────────────────────────────────────────────
# The heavy variant embeds a 200 MB payload.bin at compile time.
# Run make gen-payloads before docker build.
FROM golang:1.26 AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# payload.bin is embedded via //go:embed — the binary is ~200 MB.
# GOARCH=arm64 must match the k3d node architecture on Apple Silicon.
# Override via: docker build --build-arg GOARCH=amd64 for x86-64 clusters.
ARG GOARCH=arm64
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${GOARCH} \
    go build -trimpath -ldflags="-s -w" \
    -o /bin/fn-heavy \
    ./functions/heavy

# ── Runtime stage ─────────────────────────────────────────────────────────────
# ubuntu:22.04 base adds ~80 MB on top of the 200 MB embedded payload binary,
# reaching the ~500 MB target image size for the heavy cold-start test.
FROM ubuntu:22.04
RUN apt-get clean && rm -rf /var/lib/apt/lists/*
COPY --from=builder /bin/fn-heavy /fn-heavy
EXPOSE 8080
CMD ["/fn-heavy"]
