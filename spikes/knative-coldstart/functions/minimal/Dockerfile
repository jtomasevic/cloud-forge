# ── Build stage ──────────────────────────────────────────────────────────────
# Compile a fully-static binary so it runs on a distroless base with no libc.
FROM golang:1.26 AS builder
WORKDIR /src

# Copy module files first so Docker layer-caches the dependency download
# separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

# Copy all sources — the embed directive in medium/heavy needs the payload.bin
# to be present at compile time, but minimal has no embed so any source order works.
COPY . .

# CGO_ENABLED=0 + -ldflags="-s -w":
#   - static binary (no libc dependency)
#   - stripped debug info (smaller image layer)
#   - -trimpath removes local paths from stack traces (reproducible builds)
# GOARCH is injected by the Makefile based on `uname -m` so the binary
# matches the k3d node architecture (arm64 on Apple Silicon, amd64 on Intel).
ARG GOARCH=arm64
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${GOARCH} \
    go build -trimpath -ldflags="-s -w" \
    -o /bin/fn-minimal \
    ./functions/minimal

# ── Runtime stage ─────────────────────────────────────────────────────────────
# distroless/static has no shell, no package manager, and no libc —
# the smallest possible attack surface for a compiled Go binary.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /bin/fn-minimal /fn-minimal
EXPOSE 8080
CMD ["/fn-minimal"]
