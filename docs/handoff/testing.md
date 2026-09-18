# Testing and verification strategy

Run the smallest behavior test while editing, then the applicable complete gate from [AGENTS.md](../../AGENTS.md), [CONTRIBUTING.md](../../CONTRIBUTING.md), and [RELEASING.md](../RELEASING.md). This page maps checks to evidence; passing one layer does not establish the others.

| Layer | What it verifies | Location / command |
| --- | --- | --- |
| Config/types | Parsing, defaults, invalid bounds, cross-document references and schema parity | `go test ./internal/config/...` |
| Engine/state | Matching, templates, tool errors, strict tools, isolation, rollback, pipelines | `go test ./internal/engine/... ./internal/runner/...` |
| Wire/stream | Provider request normalization, errors, SSE/events, resource lifecycles | `go test ./internal/adapter/... ./internal/streaming/...` |
| Control plane | Roles/ownership, revision edits, logs, quota, health/ready, tenancy stores | Server/tenancy/quota/storage/audit tests |
| MCP/A2A/Realtime/vector | Protocol state machines, faults, limits and transport contracts | Corresponding internal packages and conformance fixtures |
| Recording | Hash/encoding, streaming capture, redaction, append failures and replay misses | `go test ./internal/recording/...` |
| SDK/application | Client behavior, subprocess lifecycle, assertions and framework adapters | SDK package tests; application-owned integration tests |
| GUI | Types/build, components/accessibility, actual browser interactions | `make gui-verify` (includes Playwright) |
| Deployment | Rendered topology/probes/auth/secrets and runnable container | Helm targets plus local container smoke |
| Distribution | Immutable candidate artifacts, checksums, packaging, installation | Candidate verification and install-path workflows |

## Core repository gate

```bash
go vet ./...
go test ./... -count=1 -timeout 5m
go run ./tools/driftcheck
go run ./tools/liquidcheck
go run ./tools/doccheck
go build ./cmd/mockagents
go run ./cmd/mockagents validate examples/
```

`make docs-check` runs drift, Liquid and local Markdown-path checks. `doccheck` examines tracked Markdown only and validates file targets, not URL availability or heading anchors. New untracked pages need a supplementary link check before staging; this handoff's [review evidence](../reviews/2026-09-18-review-summary.md) records that check. `driftcheck` verifies selected type/schema relationships and OpenAPI references; it does not prove all mounted routes have an OpenAPI operation.

`go run ./tools/handoffcatalog` refreshes the handoff appendices from tracked source. Compare the generated output with route/type changes. Generation is an inventory, not semantic validation.

The [documentation re-review](../reviews/2026-09-18-documentation-review-summary.md) adds direct HTTP probes and a runtime registry comparison. Those probes exposed behavior despite a green full Go suite: Responses history is not tenant-keyed, `store: false` still retains it, and Go validation accepts some values the authoring schema rejects. Passing tests establish the covered assertions; they do not establish every documented isolation, retention or validation guarantee.

## Additional gates

`make test-all` includes Go, Python, example tests and TypeScript; it is not a substitute for GUI, Helm, or release gates. SDK changes require that package's build and tests. npx uses its own Node tests, Vitest/Jest helpers their package tests. Do not run network-dependent package installation merely to interpret a source manifest.

Concurrency-sensitive Go changes require `make test-race` on Linux with `CGO_ENABLED=1` and a C compiler. The production binary's pure-Go/no-cgo build does not remove the race detector's C-toolchain requirement. Store conformance requiring Postgres needs its configured test backend; a passing default SQLite run does not establish a live Postgres result.

Deployment changes require `make helm-lint`, `make helm-template`, and a container smoke test. Release verification includes vulnerability checking, artifact manifests/digests, package-version alignment and public install-channel evidence tied to the exact candidate SHA. Those checks are broader than documentation delivery.

## Failure-path test matrix

| Change surface | Required meaningful cases |
| --- | --- |
| Tool loop | Echoed IDs, invalid arguments, repeated-call convergence, genuinely different next call, strict off/warn/strict |
| Tenant lookup/write | Global vs owned name collision; foreign owner request; spoofed tenant header/body; key rotation/role change |
| Pipeline | Missing ref, multiple roots, diamond first-input semantics, conditional skips, parallel partial errors, cancellation |
| Session | Same-ID concurrency, failed generation rollback, TTL eviction, independent agent/tenant IDs, suite cleanup |
| Persistence | Disk error before registration, failed deletion, missing source, stale ETag, explicit runtime-only receipt |
| Streams | Empty/normal finish, malformed/truncated chunk, cancellation, usage accounting and transport-specific errors |
| Recording | Redaction preserves structure, raw binary/text encoding, torn final line, repeated identical requests, strict no-upstream miss |
| GUI contract | Unknown capability/server state, 412 preserving draft, partial pipeline result, source/truncation labels, bounded exports |

Performance reports should record hardware, toolchain, workload, commit and measurements. Existing benchmark artifacts are historical evidence for their recorded conditions, not universal latency guarantees. Do not add implementation-mirroring tests for prose edits; validate commands, links, generated references and the behavior underlying material claims instead.
