# Build stage
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build with CGO for SQLite
RUN CGO_ENABLED=1 go build -ldflags="-s -w" -o /local-model-router ./cmd/router

# Runtime stage
FROM alpine:3.19

RUN apk add --no-cache ca-certificates sqlite-libs

# Create non-root user
RUN adduser -D -g '' appuser

WORKDIR /app

# Copy binary
COPY --from=builder /local-model-router /app/local-model-router

# Copy default config
COPY config.example.yaml /app/config.yaml

# Create data directory
RUN mkdir -p /app/data && chown -R appuser:appuser /app

USER appuser

# Expose ports
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/local-model-router"]
CMD ["-config", "/app/config.yaml"]
