# syntax=docker/dockerfile:1.7
# ── CloudGate — Multi-stage Docker build ─────────────────────────────────────
# Stage 1: Build (full Go toolchain)
# Stage 2: Runtime (minimal distroless image)
# ─────────────────────────────────────────────────────────────────────────────

# Build on the runner's native platform and cross-compile for each target, so
# the arm64 image really contains arm64 binaries (GOARCH was hardcoded to amd64).
FROM --platform=$BUILDPLATFORM golang:1.26.11-alpine AS builder
ARG TARGETOS TARGETARCH

WORKDIR /workspace

# Copy dependency manifests first to leverage Docker layer cache
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build both binaries
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" \
    -o /bin/cloudgate-gateway ./cmd/gateway

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" \
    -o /bin/cloudgate-admin ./cmd/admin

# ── Runtime image ─────────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

LABEL org.opencontainers.image.title="CloudGate"
LABEL org.opencontainers.image.description="Distributed API Gateway with rate limiting and circuit breaker"
LABEL org.opencontainers.image.source="https://github.com/aryaMehta26/CloudGate-Distributed-API-GATEWAY"
LABEL org.opencontainers.image.licenses="MIT"

COPY --from=builder /bin/cloudgate-gateway /cloudgate-gateway
COPY --from=builder /bin/cloudgate-admin   /cloudgate-admin

# Gateway ports
EXPOSE 8080 9090 50051

USER nonroot:nonroot

ENTRYPOINT ["/cloudgate-gateway"]
