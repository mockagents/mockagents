# Contributing to MockAgents

Thank you for your interest in contributing!

## Your first contribution, in five minutes

If you just want to fix one thing and get out, this is the whole path:

```bash
git clone https://github.com/mockagents/mockagents.git
cd mockagents
go build ./... && go test ./internal/...      # focused Go baseline; no Python or Docker
```

That is the focused Go toolchain. Use the exact toolchain selected by `go.mod`
(currently Go 1.26.6). SQLite is
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

Docs-only changes still run `make docs-check`; commands, links, and release
claims are part of the supported product.

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

**Requirements:** the Go toolchain selected by `go.mod`; Python 3.10+ when
changing the Python SDK; Node.js 22.12+ when changing the TypeScript SDKs or
the GUI (the test toolchain — vitest 5, Next.js 16 — needs it, even though the
published SDKs run on older Node versions).

### Branch model & git hooks

Contributors work the usual GitHub way: fork, push a feature branch to your
fork, and open a pull request. `make setup` does not install any git hooks.

Maintainers can opt into `make hooks`, which points `core.hooksPath` at the
tracked `hooks/` directory. Its `pre-push` guard refuses to push a branch other
than `main` to the canonical `mockagents/mockagents` repository (pushes to a
fork are unaffected); override once with `git push --no-verify`.

## Running Tests

```bash
make test          # Go tests
make test-python   # Python SDK tests
make test-all      # All tests
make lint          # Code quality checks
make docs-check    # Documentation drift and links
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
- **In CI (recommended):** the dedicated Linux race job has a C toolchain.
  Windows compatibility is checked separately without `-race`.

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
2. Write tests for new functionality — a test that fails without your change
3. Ensure all tests pass: `make test-all`
4. Follow existing code style (gofmt for Go, ruff for Python)
5. Sign off every commit (see below) and add a `CHANGELOG.md` entry under
   `## [Unreleased]` for anything user-visible
6. Submit a PR with a clear description of what and why; the pull request
   template has the checklist

## Developer Certificate of Origin

Contributions are accepted under the [Apache License 2.0](LICENSE). To certify
that you wrote the change or otherwise have the right to submit it under that
licence, every commit must carry a `Signed-off-by` line matching the commit
author, as defined by the [Developer Certificate of Origin 1.1](https://developercertificate.org/):

```bash
git commit -s -m "fix: describe the change"
```

`-s` adds `Signed-off-by: Your Name <you@example.com>` from your git config. To
sign off commits you already made: `git rebase --signoff main`. Pull requests
with unsigned commits are not merged.

## Code Style

- **Go:** Standard `gofmt` formatting, `go vet` clean
- **Python:** PEP 8, checked with `ruff check sdk/python` (configured in
  `sdk/python/pyproject.toml`)
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

[Browse the open good first issues →](https://github.com/mockagents/mockagents/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22)

The detail lives in the issues rather than here, so there is one copy to keep
true.
