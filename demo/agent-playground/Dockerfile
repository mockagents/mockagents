# Agent Playground image. Build from the REPOSITORY ROOT (the playground is
# part of the mockagents Go module and embeds its own UI, OpenAPI document,
# config and mockagents fixtures):
#
#   docker build -f demo/agent-playground/Dockerfile -t mockagents-playground .
#   docker run --rm -p 7070:7070 mockagents-playground
#
# Base images are pinned by digest and kept in sync with the root Dockerfile.

# Stage 1: build the static binary.
FROM golang:1.27.1-alpine@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /bin/playground ./demo/agent-playground/cmd/playground

# Stage 2: minimal runtime.
FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

LABEL org.opencontainers.image.title="MockAgents Agent Playground" \
      org.opencontainers.image.description="Multi-agent demo application running on mockagents" \
      org.opencontainers.image.source="https://github.com/mockagents/mockagents" \
      org.opencontainers.image.licenses="Apache-2.0"

RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 playground \
    && adduser -S -u 10001 -G playground playground \
    && mkdir -p /data && chown playground:playground /data

COPY --from=builder /bin/playground /usr/local/bin/playground
COPY LICENSE NOTICE /usr/share/doc/playground/

USER 10001:10001
WORKDIR /data
EXPOSE 7070

HEALTHCHECK --interval=15s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:7070/readyz || exit 1

ENTRYPOINT ["playground"]
# Embedded mock by default. Point at an external mockagents with
#   --mock http://mockagents:8080   (see docker-compose.yml, profile "external").
CMD ["serve", "--addr", "0.0.0.0:7070", "--state-file", "/data/runs.json"]
