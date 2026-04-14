# Contributing to MockAgents

Thank you for your interest in contributing!

## Development Setup

```bash
git clone https://github.com/mockagents/mockagents.git
cd mockagents
make setup
```

**Requirements:** Go 1.26+, Python 3.10+ (for SDK)

## Running Tests

```bash
make test          # Go tests
make test-python   # Python SDK tests
make test-all      # All tests
make lint          # Code quality checks
```

## Project Structure

| Directory | Description |
|-----------|-------------|
| `cmd/mockagents/` | CLI entry point (Cobra commands) |
| `internal/adapter/` | OpenAI + Anthropic protocol adapters |
| `internal/engine/` | Core mock engine |
| `internal/server/` | HTTP server and middleware |
| `internal/streaming/` | SSE streaming |
| `internal/storage/` | SQLite interaction logging |
| `internal/config/` | YAML loading and validation |
| `internal/types/` | Domain types |
| `sdk/python/` | Python SDK |
| `examples/` | Example agent definitions |
| `schema/` | JSON Schema for agent definitions |
| `site/` | Documentation (MkDocs) |

## Pull Request Process

1. Fork the repository and create a feature branch
2. Write tests for new functionality
3. Ensure all tests pass: `make test-all`
4. Follow existing code style (gofmt for Go, ruff for Python)
5. Submit a PR with a clear description of what and why

## Code Style

- **Go:** Standard `gofmt` formatting, `go vet` clean
- **Python:** PEP 8, enforced by `ruff`
- **YAML:** 2-space indentation
- **Commits:** Conventional commits preferred (`feat:`, `fix:`, `docs:`)
