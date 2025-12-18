# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache gcc musl-dev git

# Copy go mod files
COPY go.mod go.sum* ./

# Download dependencies (creates go.sum if doesn't exist)
RUN go mod download || go mod tidy

# Copy source code
COPY . .

# Build with optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
    -ldflags="-w -s -extldflags '-static'" \
    -a -installsuffix cgo \
    -o fingerprint-converter \
    cmd/api/main.go

# Runtime stage
FROM alpine:3.20

# Install runtime dependencies
RUN apk add --no-cache \
    ffmpeg \
    ffmpeg-libs \
    ca-certificates \
    tini \
    curl \
    && rm -rf /var/cache/apk/*

# Create non-root user
RUN addgroup -g 1000 -S appuser && \
    adduser -u 1000 -S appuser -G appuser

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/fingerprint-converter .

# Create cache directory
RUN mkdir -p /tmp/media-cache && \
    chown -R appuser:appuser /app /tmp/media-cache

# Use tmpfs for cache (mounted at runtime)
VOLUME ["/tmp/media-cache"]

# Environment variables
ENV PORT=5001 \
    GOGC=100 \
    GOMEMLIMIT=2GiB \
    GOMAXPROCS=0 \
    MAX_WORKERS=0 \
    BUFFER_POOL_SIZE=100 \
    BUFFER_SIZE=10485760 \
    REQUEST_TIMEOUT=5m \
    BODY_LIMIT=524288000 \
    CACHE_DIR=/tmp/media-cache \
    CACHE_TTL=28m \
    FILE_TTL=30m \
    ENABLE_CACHE=true \
    DEFAULT_AF_LEVEL=moderate

# Change to non-root user
USER appuser

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:5001/api/health || exit 1

# Use tini for proper signal handling
ENTRYPOINT ["/sbin/tini", "--"]

# Start the application
CMD ["./fingerprint-converter"]

EXPOSE 5001
