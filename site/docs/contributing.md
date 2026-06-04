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
  adapter/               # OpenAI + Anthropic protocol adapters
  audit/                 # Append-only audit log
  cli/                   # CLI scaffolding and color utilities
  config/                # YAML loading and validation (all 4 kinds)
  contract/              # Contract extraction + diffing
  engine/                # Core mock engine (matching, generation, tools)
  engine/state/          # Session state management
  mcp/                   # Mock MCP server (JSON-RPC, HTTP/stdio/SSE)
  observability/         # OpenTelemetry tracing
  pricing/               # Per-model cost table
  recording/             # Record / replay cassettes
  runner/                # TestSuite executor + JUnit
  server/                # HTTP server, middleware, handlers
  storage/               # SQLite interaction logging
  streaming/             # SSE streaming (OpenAI + Anthropic)
  tenancy/               # Multi-tenant store + RBAC + auth cache
  types/                 # Domain types (agent, tool, behavior)
sdk/{python,typescript,go}/  # Three language SDKs
gui/                     # Next.js 15 web console
deploy/                  # Helm chart, CI/CD templates
schema/                  # JSON Schemas for the 4 document kinds
examples/                # Example definitions
site/                    # Documentation (MkDocs)
```

## Pull Request Process

1. Fork the repository
2. Create a feature branch from `main`
3. Write tests for new functionality
4. Ensure `make test-all` passes
5. Submit a PR with a clear description
