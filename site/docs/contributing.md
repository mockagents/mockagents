# Contributing

## Development Setup

```bash
git clone https://github.com/mockagents/mockagents.git
cd mockagents
make setup
```

## Running Tests

```bash
make test            # Go tests
make test-python     # Python SDK tests
make test-typescript # TypeScript SDK tests
make test-all        # Go + Python + TypeScript
make gui-build       # GUI build (tsc --strict gate)
```

## Code Quality

```bash
make lint   # go vet
make fmt    # gofmt
```

## Building

```bash
make build     # Local binary
make docker    # Docker image
make release   # GoReleaser snapshot
```

## Project Structure

```
cmd/mockagents/          # Cobra CLI entry points
internal/
  a2a/                   # Mock Agent2Agent (A2A) server
  adapter/               # Protocol adapters (OpenAI, Anthropic, Gemini, Azure, Realtime bridge)
  audit/                 # Append-only audit log
  cli/                   # CLI scaffolding and color utilities
  config/                # YAML loading and validation (all 5 kinds)
  contract/              # Contract extraction + diffing
  engine/                # Core mock engine (matching, generation, tools, chaos, strict-tools)
  engine/state/          # Session state management
  mcp/                   # Mock MCP server (JSON-RPC, Streamable HTTP/stdio/SSE)
  mcpadmin/              # Agent CRUD exposed as MCP tools (mcp --manage)
  observability/         # OpenTelemetry tracing
  oidcauth/              # OIDC relying-party seam for SSO
  pricing/               # Per-model cost table
  quota/                 # Per-tenant rate + spend enforcement
  realtime/              # OpenAI Realtime WebSocket sessions (server VAD, pacing)
  recording/             # Record / replay cassettes
  runner/                # TestSuite executor + JUnit
  server/                # HTTP server, middleware, handlers
  storage/               # SQLite interaction logging
  streaming/             # SSE streaming + timing physics
  tenancy/               # Multi-tenant store + RBAC + auth cache
  toolschema/            # JSON-Schema tool-argument + strict-subset validators
  types/                 # Domain types (agent, tool, behavior)
sdk/{python,typescript,go}/  # Three language SDKs
gui/                     # Next.js 15 web console
deploy/                  # Helm chart, CI/CD templates
schema/                  # JSON Schemas for the 5 document kinds
examples/                # Example definitions
site/                    # Documentation (MkDocs)
```

## Pull Request Process

1. Fork the repository
2. Create a feature branch from `main`
3. Write tests for new functionality
4. Ensure `make test-all` passes
5. Submit a PR with a clear description
