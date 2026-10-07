# Base images are pinned by digest so a build is reproducible and a tag cannot
# be moved underneath it; Dependabot (docker ecosystem) proposes updates.
# alpine:3.19 was end-of-life (review O-19).

# Stage 1: Build the Go binary
FROM golang:1.27.1-alpine@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS builder

WORKDIR /src

# Cache dependency downloads.
COPY go.mod go.sum ./
RUN go mod download

# VERSION is stamped into the binary; the release workflow passes the tag.
# A local `docker build` reports "docker".
ARG VERSION=docker

# Copy source and build. -trimpath drops local paths from the binary so builds
# are reproducible.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /bin/mockagents \
    ./cmd/mockagents

# Licence and NOTICE text of every module linked into the binary (review O-02).
RUN go run ./tools/thirdpartynotices -o /THIRD_PARTY_NOTICES.md

# Stage 2: Minimal runtime image
FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

ARG VERSION=docker
LABEL org.opencontainers.image.title="MockAgents" \
      org.opencontainers.image.description="Mock server for OpenAI, Anthropic, Gemini, Ollama, Bedrock, MCP, A2A and Realtime APIs" \
      org.opencontainers.image.source="https://github.com/mockagents/mockagents" \
      org.opencontainers.image.documentation="https://github.com/mockagents/mockagents/tree/main/site/docs" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${VERSION}"

# A fixed, non-system UID/GID (10001) so the Helm chart's runAsUser and any
# host volume permissions can rely on it; `adduser -S` picked an arbitrary one.
RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 mockagents \
    && adduser -S -u 10001 -G mockagents mockagents

COPY --from=builder /bin/mockagents /usr/local/bin/mockagents
COPY LICENSE NOTICE /usr/share/doc/mockagents/
COPY --from=builder /THIRD_PARTY_NOTICES.md /usr/share/doc/mockagents/

# Create directories for agent definitions and data.
RUN mkdir -p /agents /data \
    && chown mockagents:mockagents /agents /data

VOLUME ["/agents", "/data"]

USER 10001:10001

# Run from the writable data volume, not /. Relative writes — the SQLite
# interaction/audit/tenancy DBs and `mockagents init` scaffolding — land in
# /data (persisted by the volume above) instead of failing with EACCES as
# the non-root user (QA: "mkdir /starter: permission denied",
# "unable to open database file: out of memory (14)" = SQLITE_CANTOPEN).
WORKDIR /data

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/api/v1/health || exit 1

ENTRYPOINT ["mockagents"]
CMD ["start", "--host", "0.0.0.0", "--port", "8080", "--agents-dir", "/agents"]
