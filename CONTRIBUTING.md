# Contributing to MockAgents

Thank you for your interest in contributing!

## Your first contribution, in five minutes

If you just want to fix one thing and get out, this is the whole path:

```bash
git clone https://github.com/mockagents/mockagents.git
cd mockagents
go build ./... && go test ./internal/...      # ~1 min; no Python, no Docker, no cgo
```

That's the full toolchain. Go 1.26+ is the only hard requirement — SQLite is
`modernc.org/sqlite`, so there is no C compiler, no database to install, and
nothing to run in the background.

Then:

1. Pick something from
   [the good-first-issue list](https://github.com/mockagents/mockagents/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22).
   Each one names the file, the change, and what "done" looks like.
2. Comment on it to claim it, so two people don't write the same patch.
3. Fix it, add a test, `go test ./internal/<package>` — run the one package you
   touched, not the whole suite.
4. Open a PR. CI runs the rest.

Docs-only change? Skip step 3 entirely; there is nothing to build.

### What we owe you back

- **A first response within a week** on any issue or PR, even if it is only
  "looks right, need a day to read it properly".
- If you have heard nothing after **two weeks**, bump the thread. Silence is a
  bug in our process, not a verdict on your contribution.
- A PR that stalls on review gets merged or gets a concrete reason. It does not
  get quietly closed.

Filing an issue is a contribution on its own. A bug report from someone who is
not a maintainer is the most useful signal this project gets, so please open
one even if you are not going to fix it — and especially if the docs were wrong.

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

## Ways to Contribute

MockAgents is early-stage and the surface is wide. The highest-value contributions
right now are **good-first-issue fixes** (well-scoped, test-covered, no architecture
decisions needed) and **docs** (examples, drop-in recipes, framework guides). New
starter templates (`mockagents init --template`), framework recipes not yet in the
docs (AutoGen, Haystack, Semantic Kernel), and CI integrations beyond GitHub Actions
/ GitLab CI (Bitbucket Pipelines, CircleCI) are all welcome. Open a discussion first
for anything that touches `internal/types` — those changes ripple widely.

### Good first issues

Every one is filed with the `good first issue` label, and each names the file to
change, what the fix involves, and what "done" looks like. Comment on the issue
to claim it so two people don't write the same patch.

| # | What | Where |
| --- | --- | --- |
| [#37](https://github.com/mockagents/mockagents/issues/37) | `Cassette.Append` rewrites the whole file every call — O(n²) recording | `internal/recording/cassette.go` |
| [#38](https://github.com/mockagents/mockagents/issues/38) | One torn last line makes a whole cassette unloadable | `internal/recording/cassette.go` |
| [#39](https://github.com/mockagents/mockagents/issues/39) | `mockagents import` has no `--redact`, so imported cassettes keep their secrets | `cmd/mockagents/import.go` |
| [#40](https://github.com/mockagents/mockagents/issues/40) | One oversized line aborts the whole stored-completions import | `internal/recording/import_openai.go` |
| [#41](https://github.com/mockagents/mockagents/issues/41) | Connection-fault requests are logged as HTTP 200 | `internal/server/log_handlers.go` |
| [#42](https://github.com/mockagents/mockagents/issues/42) | Record the README demo GIF (no Go needed) | `README.md` |
| [#43](https://github.com/mockagents/mockagents/issues/43) | Gemini `inlineData` parts never trigger `has_image` matching | `internal/adapter/gemini.go` |
| [#44](https://github.com/mockagents/mockagents/issues/44) | Anthropic streaming omits thinking blocks and cache usage | `internal/adapter/anthropic.go` |

[Browse the live list →](https://github.com/mockagents/mockagents/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22)

The detail lives in the issues rather than here, so there is one copy to keep
true.
