# ============================================================================
# ATTChat Gateway Dockerfile
# Multi-stage build for minimal image size
# ============================================================================

# Stage 1: Build
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o /gateway \
    .

# Stage 2: Runtime
FROM gcr.io/distroless/static:nonroot

# Copy timezone data
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Copy CA certificates
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy binary
COPY --from=builder /gateway /gateway

# Copy config (optional, can be overridden by env vars)
COPY config.yaml /config.yaml

# Expose ports
EXPOSE 8086 9090

# Run as non-root user
USER nonroot:nonroot

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ["/gateway", "-health"]

# Entry point
ENTRYPOINT ["/gateway"]

