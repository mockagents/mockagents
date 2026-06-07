# Architecture

A map of the MockAgents codebase for contributors. For *using* the tool, see
the [README](README.md) and the [docs site](site/docs/index.md).

## Request flow

```
HTTP request
  → server (net/http mux, middleware: auth, CORS, logging, quota)
    → protocol adapter (OpenAI / Anthropic / Gemini)   wire JSON → engine types
      → engine                                          match agent + scenario
        → response generator                            expand templates, tools
      ← engine.Response
    ← adapter                                           engine types → wire JSON
  ← HTTP response (JSON, or SSE stream when stream=true)
        ↘ interaction logged to SQLite (async worker pool)
```

The engine is provider-agnostic: adapters translate each provider's wire format
to/from the engine's internal `types`, so the matching/generation logic is
written once.

## Packages (`internal/`)

| Package | Role |
|---|---|
| `adapter/` | Wire-format translators. `openai.go`, `anthropic.go`, `gemini.go` convert provider JSON ↔ engine types. `registry.go` is the extension seam (`Adapter` interface + `DefaultRegistry`). `token.go` counts tokens; `decode.go` pools buffers for request decoding. |
| `engine/` | The core. `engine.go` is the entry point; `agent_registry.go` looks up an agent (by-model index + `*ForTenant` visibility), `scenario_matcher.go` picks a scenario, `response_generator.go` produces content (template expansion). `tool_processor.go`/`tool_validator.go` handle simulated tool calls. `reqmeta.go` carries `RequestMeta` + tenant id on the context. State lives in `state/`. |
| `server/` | `net/http` server, middleware, and route handlers for the LLM + management APIs. `route_authz.go` is the single role-floor table + `mountManaged` chokepoint. `log_worker.go`/`log_broadcaster.go` own async logging + SSE fan-out. |
| `tenancy/` | Multi-tenant store (SQLite + Postgres impls of `Store`), bcrypt API keys, RBAC middleware, SSO users/sessions, `auth_cache.go`. |
| `audit/` | Append-only audit log (SQLite, WAL). `Recorder` takes a principal-extraction function so it never imports `tenancy`. |
| `quota/` | Per-tenant rate + monthly-spend enforcement (token bucket + shared spend ledger). |
| `oidcauth/` | OIDC relying-party seam for SSO login. |
| `pricing/` | Per-model cost table + usage extractor. |
| `mcp/` | JSON-RPC 2.0 Model Context Protocol mock (HTTP + stdio + bidirectional SSE). |
| `recording/` | Record/replay cassettes (incl. SSE). |
| `streaming/` | SSE chunking per provider (`openai.go`, `anthropic.go`, `gemini.go`). |
| `storage/` | SQLite interaction logging (pure-Go `modernc.org/sqlite`, WAL). |
| `config/` | YAML loader + validator (schema at `schema/mockagents-v1-agent.json`). |
| `runner/`, `contract/` | TestSuite executor (JUnit output) and contract extract/diff. |
| `types/` | Domain types shared across packages. Changes here ripple widely. |
| `observability/` | OpenTelemetry tracer wiring (zero overhead when disabled). |

Outside `internal/`: `cmd/mockagents/` (Cobra CLI), `gui/` (Next.js console),
`sdk/{python,typescript,go}/`, `deploy/` (Helm, GitHub/GitLab CI), `site/` (MkDocs).

## Design rules (keep these intact)

- **No cgo.** SQLite is `modernc.org/sqlite` so cross-compilation and goreleaser
  stay simple. (Side effect: `go test -race` is unavailable.)
- **Import direction.** `tenancy` may import `engine`, never the reverse. The
  engine reads the tenant id from the context (`engine.WithTenantID` /
  `TenantIDFromContext`) instead of importing `tenancy`. The `audit` recorder
  receives a principal-extraction *function* from the server for the same reason.
  This keeps the engine cycle-free and provider/tenant-agnostic.
- **One authorization chokepoint.** Every `/api/v1` management route goes through
  `server.mountManaged`, which applies the floor from `managementRouteFloors`
  (`route_authz.go`) and **panics on a route with no entry** — an ungated route
  can't slip in. The table is snapshot-tested (`route_authz_test.go`); changing
  a floor must be mirrored in [`docs/guides/multi-tenant.md`](docs/guides/multi-tenant.md).
- **The agent YAML schema is authoritative** in `schema/mockagents-v1-agent.json`.
  Run `make validate` after touching config/types.
- **Hot path is benchmarked.** `docs/benchmarks/latest.{json,md}` is checked in;
  rerun `make bench-report` for perf-affecting changes and don't regress it.

## Adding a provider adapter

The adapter registry (REF-05) makes this self-contained — no edits to the
server's route wiring. To add provider `foo` (see `gemini.go` as the template):

1. **`internal/adapter/foo.go`** — implement the `Adapter` interface
   (`Name() string`, `Routes() []Route`) and a handler that decodes the wire
   request → `engine.InboundRequest`, calls `Engine.ProcessRequestContext`, and
   formats `engine.Response` back to the wire shape (incl. tool calls + errors).
   Stamp `meta.Protocol` early.
2. **`internal/streaming/foo.go`** — a `StreamFoo` function for the SSE path.
3. **Register** the handler in `adapter.DefaultRegistry` (`registry.go`).
4. **Schema + validator** — add the protocol string to the `protocol` enum in
   `schema/mockagents-v1-agent.json` and to `validProtocols` in
   `internal/config/validator.go`.
5. **Example + test** — add `examples/foo-agent.yaml` and
   `internal/adapter/foo_test.go`; update the `registry_test.go` order/route
   assertions. Run `make validate` + `go test ./internal/adapter/...`.

## Testing

- `make test`. Note: `go test -race` needs cgo, which this project deliberately
  does not use (pure-Go SQLite), so the race detector is unavailable on **all**
  platforms — see Design rules above.
- Cross-adapter integration: `internal/server/conformance_test.go`.
- Scenario-match semantics: `internal/engine/scenario_matcher_test.go` + the
  `_e008_` files — consult before changing match precedence.
- Store conformance runs against Postgres when `MOCKAGENTS_TEST_PG_DSN` is set.
- SDKs: `make test-python` / `make test-typescript`.

## Where to look first

- A new wire field isn't returned → the relevant `adapter/*.go`.
- A scenario matches the wrong response → `engine/scenario_matcher.go` + tests.
- A `{{ template }}` expression → `engine/response_generator.go`.
- A 401/403 on a management route → `server/route_authz.go`.
- A new CLI command → `cmd/mockagents/`.
