# Stage 1: Build the Go binary
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Cache dependency downloads.
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=docker" \
    -o /bin/mockagents \
    ./cmd/mockagents

# Stage 2: Minimal runtime image
FROM alpine:3.19

RUN apk add --no-cache ca-certificates \
    && addgroup -S mockagents \
    && adduser -S mockagents -G mockagents

COPY --from=builder /bin/mockagents /usr/local/bin/mockagents

# Create directories for agent definitions and data.
RUN mkdir -p /agents /data \
    && chown mockagents:mockagents /agents /data

VOLUME ["/agents", "/data"]

USER mockagents

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/api/v1/health || exit 1

ENTRYPOINT ["mockagents"]
CMD ["start", "--port", "8080", "--agents-dir", "/agents"]
