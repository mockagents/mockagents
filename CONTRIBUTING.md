# Contributing to MockAgents

Thank you for your interest in contributing!

## Development Setup

```bash
git clone https://github.com/mockagents/mockagents.git
cd mockagents
make setup
```

**Requirements:** Go 1.26+, Python 3.10+ (for SDK)

### Branch model & git hooks

Only `main` is published to the public repo — feature branches stay local (or
on a private remote). `make setup` runs `make hooks`, which points
`core.hooksPath` at the tracked `hooks/` directory and enables a `pre-push`
guard that refuses to push any branch other than `main` to `origin`. Pushes to
other remotes are unaffected; override once with `git push --no-verify`. If you
skip `make setup`, activate the hook directly with `make hooks`.

## Running Tests

```bash
make test          # Go tests
make test-python   # Python SDK tests
make test-all      # All tests
make lint          # Code quality checks
```

### Race detection

The Go race detector needs `CGO_ENABLED=1` **and** a C compiler (gcc/clang).
MockAgents is otherwise pure-Go on purpose — SQLite is `modernc.org/sqlite`,
so the normal build and `make test` need no cgo and cross-compile cleanly.

The trade-off: `make test-race` (`go test -race`) only runs where a C
compiler is present. On a bare Windows dev box without mingw it fails with
`-race requires cgo`; that is expected, not a bug. Two ways to get race
coverage:

- **Locally:** install a C toolchain (Linux/macOS already have one; on
  Windows install mingw-w64), then `make test-race`.
- **In CI (recommended):** the Go workflow runs `-race` on its Linux and
  macOS legs, which always have a C toolchain. The Windows leg runs the
  suite **without** `-race` (it still gates compilation and behavior there);
  race coverage for the shared, platform-independent code comes from the
  Linux/macOS legs. Don't add `-race` to the Windows leg — it makes the job
  depend on whatever C compiler happens to be on the runner image.

## Project Structure

See [ARCHITECTURE.md](ARCHITECTURE.md) for the request flow, package
responsibilities, design rules (import direction, no-cgo, the authorization
chokepoint), and a step-by-step guide to adding a provider adapter.

| Directory | Description |
|-----------|-------------|
| `cmd/mockagents/` | CLI entry point (Cobra commands) |
| `internal/adapter/` | OpenAI + Anthropic + Gemini protocol adapters |
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
