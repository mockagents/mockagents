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

## Ways to Contribute

MockAgents is early-stage and the surface is wide. The highest-value contributions
right now are **good-first-issue fixes** (well-scoped, test-covered, no architecture
decisions needed) and **docs** (examples, drop-in recipes, framework guides). New
starter templates (`mockagents init --template`), framework recipes not yet in the
docs (AutoGen, Haystack, Semantic Kernel), and CI integrations beyond GitHub Actions
/ GitLab CI (Bitbucket Pipelines, CircleCI) are all welcome. Open a discussion first
for anything that touches `internal/types` — those changes ripple widely.

### Good first issues

Each item below is a real, well-scoped gap: the file to change, what the fix
involves, and what "done" looks like. Comment on the matching issue (or open one)
to claim it so we don't duplicate work.

**GFI-01 — `Cassette.Append` rewrites the whole file every call.**
`internal/recording/cassette.go` (`Append`) rewrites the entire `.jsonl` cassette
on every append — O(n²) over a long recording session. Switch to an `O_APPEND`
single-line write (see the existing `AppendAll` for the right shape). *Done when:*
recording the Nth interaction is O(1) file I/O, covered by a test.

**GFI-02 — `Cassette.Load` aborts on a torn last line.**
`internal/recording/cassette.go` (`Load`) returns an error on the first
unparseable JSON line, discarding all valid interactions after it. A process killed
mid-write leaves a partial last line. Skip an unparseable *trailing* line with a
warning instead. *Done when:* a cassette with one torn last line loads the prior
interactions intact.

**GFI-03 — `mockagents import` has no `--redact`.**
`cmd/mockagents/import.go`. `mockagents record` supports `--redact` /
`--redact-pattern`; the `import vcr` and `import openai-stored-completions`
subcommands don't (their help text tells users to re-record instead). Wire the
existing `recording.Redactor` (`internal/recording/redact.go`) through before
`AppendAll`. *Done when:* `import vcr --redact` masks secrets, covered by a
round-trip test.

**GFI-04 — `import openai-stored-completions` hard-fails on a >32 MiB line.**
`internal/recording/import_openai.go` (`ImportOpenAIStored`). The scanner is
capped at `MaxCassetteLine`; a single oversized line makes `bufio.Scanner` return
`bufio.ErrTooLong`, aborting the whole import. Detect it, skip that line with a
reason in `ImportResult.SkipReasons`, and continue. *Done when:* a file with one
oversized line and ten valid lines imports the ten.

**GFI-05 — Connection-fault requests log as status 200.**
`internal/server/log_handlers.go` (`captureWriter`). When a `chaos.connection`
fault hijacks the TCP connection, the handler never calls `WriteHeader`, so the
interaction log records the default 200. Add a sentinel status (e.g. `0`) and a
`connection_fault` marker so the log reflects the fault. *Done when:* a
`connection: reset` agent produces a non-200 log entry, covered by a test.

**GFI-06 — Embed the demo GIF.**
`README.md` (the `TODO(RR-03)` comment). Record a ~12s terminal GIF of
`mockagents start` + an SDK call + the web console's live log feed (`vhs` or
`asciinema`; ≤2 MB, `gifsicle -O3`). *Done when:* the README renders the GIF and
the TODO is removed.

**GFI-07 — Gemini vision: surface `has_image` from `inlineData`/`fileData`.**
`internal/adapter/gemini.go` accepts but ignores `inlineData`/`fileData` parts.
OpenAI and Anthropic already count images and enable the `has_image` scenario
rule. Count Gemini image parts the same way. *Done when:* a Gemini request with
an `inlineData` part matches an `has_image: true` scenario, covered by a test.

**GFI-08 — Anthropic streaming: emit thinking blocks + cache usage.**
`internal/adapter/anthropic.go` + `internal/streaming`. The non-streaming
Anthropic path emits synthesized `thinking` blocks and `cache_creation`/
`cache_read` usage; the streaming path doesn't yet. Emit a `thinking`
content-block in the SSE ladder and cache usage in the final `message_delta`.
*Done when:* a streaming request to a `thinking`-enabled agent includes a thinking
block, covered by the Anthropic streaming conformance test.
