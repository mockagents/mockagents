# Contributing

## Development Setup

```bash
git clone https://github.com/mockagents/mockagents.git
cd mockagents
make setup
```

## Running Tests

```bash
make test          # Go tests
make test-python   # Python SDK tests
make test-all      # Both
make test-race     # Go tests with race detector
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
cmd/mockagents/          # CLI entry point
internal/
  adapter/               # OpenAI + Anthropic protocol adapters
  cli/                   # CLI scaffolding and color utilities
  config/                # YAML loading and validation
  engine/                # Core mock engine (matching, generation, tools)
  engine/state/          # Session state management
  server/                # HTTP server, middleware, handlers
  storage/               # SQLite interaction logging
  streaming/             # SSE streaming (OpenAI + Anthropic)
  types/                 # Domain types (agent, tool, behavior)
sdk/python/              # Python SDK
schema/                  # JSON Schema for agent definitions
examples/                # Example agent definitions
site/                    # Documentation (MkDocs)
```

## Pull Request Process

1. Fork the repository
2. Create a feature branch from `main`
3. Write tests for new functionality
4. Ensure `make test-all` passes
5. Submit a PR with a clear description
