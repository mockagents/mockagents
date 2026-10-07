# Agent Playground image. Build from the REPOSITORY ROOT (the playground is
# part of the mockagents Go module and embeds its own UI, OpenAPI document,
# config and mockagents fixtures):
#
#   docker build -f demo/agent-playground/Dockerfile -t mockagents-playground .
#   docker run --rm -p 7070:7070 mockagents-playground
#
# Stage 1: build the static binary.
FROM golang:1.26.6-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/playground ./demo/agent-playground/cmd/playground

# Stage 2: minimal runtime.
FROM alpine:3.19

RUN apk add --no-cache ca-certificates \
    && addgroup -S playground \
    && adduser -S playground -G playground \
    && mkdir -p /data && chown playground:playground /data

COPY --from=builder /bin/playground /usr/local/bin/playground

USER playground
WORKDIR /data
EXPOSE 7070

HEALTHCHECK --interval=15s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:7070/readyz || exit 1

ENTRYPOINT ["playground"]
# Embedded mock by default. Point at an external mockagents with
#   --mock http://mockagents:8080   (see docker-compose.yml, profile "external").
CMD ["serve", "--addr", "0.0.0.0:7070", "--state-file", "/data/runs.json"]
