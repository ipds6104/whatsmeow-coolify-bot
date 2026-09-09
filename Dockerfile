# ==========================================
# Stage 1: Build the Go Application
# ==========================================
FROM golang:1.26-alpine AS builder

WORKDIR /build

# Copy dependency specifications and vendored modules (100% offline, hermetic build)
COPY go.mod go.sum ./
COPY vendor/ ./vendor/

# Copy application source files
COPY cmd/ ./cmd/
COPY internal/ ./internal/

# Build static binary with optimizations using vendored dependencies
RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -ldflags="-w -s" -trimpath -o /build/bot ./cmd/bot

# ==========================================
# Stage 2: Minimal Production Runtime
# ==========================================
FROM alpine:3.20

WORKDIR /app

# Install essential runtime utilities:
# - ca-certificates: TLS verification for WhatsApp websocket servers
# - tzdata: Accurate timestamps in backups and logs
# - curl: Healthcheck probe for Traefik and Coolify
RUN apk add --no-cache ca-certificates tzdata curl && \
    addgroup -S appgroup && adduser -S appuser -G appgroup

# Create persistent storage directories with appropriate permissions
RUN mkdir -p /app/backups && chown -R appuser:appgroup /app

COPY --from=builder --chown=appuser:appgroup /build/bot /app/bot

USER appuser

EXPOSE 8080

# Coolify / Docker Container Healthcheck
HEALTHCHECK --interval=20s --timeout=5s --start-period=15s --retries=3 \
  CMD curl -f http://localhost:8080/healthz || exit 1

ENTRYPOINT ["/app/bot"]
