# MockAgents — Implementation Progress

**Last updated:** 2026-04-13
**Source of truth:** this file. Other `docs/` pages describe the *design
intent* from the original product plan; when those pages and this file
disagree, this file wins.

PROGRESS.md is intentionally terse and scannable. Every row is a slice
that has landed end-to-end with tests and, where relevant, a live
smoke-test run on the built binary.

---

## 1. Status Overview

| Phase             | Goal                                                      | Status                        |
| ----------------- | --------------------------------------------------------- | ----------------------------- |
| **Phase 1 — MVP** | Single-agent mocking for OpenAI/Anthropic, CLI, SDKs      | **Complete**                  |
| Phase 2           | Multi-agent, testing, record/replay, framework adapters   | **Complete** (slices listed)  |
| Phase 3           | Chaos engineering, MCP server mocking                     | **Complete** (slices listed)  |
| Phase 4           | Contracts, OTel, SDKs, GUI, Helm, multi-tenancy           | **v0.1 slices complete**      |

Two residual P1 items from the original MVP are still carried:

| Story   | Status  | Gap                                                      |
| ------- | ------- | -------------------------------------------------------- |
| US-2.3  | Partial | Manual reload endpoint exists; no fsnotify auto-watcher. |
| US-12.2 | Partial | Benchmarks exist as Go tests; no published report.       |

---

## 2. Slices Landed

Each row: what shipped, the code path, where the tests live, and the
verification. "**Live**" means an actual binary was spun up and probed
with curl or the SDK under test.

### 2.1 Phase 1 — Foundation MVP

44 of 46 stories fully done; 2 carry-over partials above. See
`product-backlog.md` Section 1A for the per-epic breakdown. Packages:
`internal/{adapter,config,engine,server,streaming,storage,cli,types}`,
`cmd/mockagents/{init,start,validate,logs}.go`, `sdk/python/mockagents/`,
`examples/*.yaml`, `schema/mockagents-v1-agent.json`, `site/`.

### 2.2 Multi-agent pipelines + TestSuite runner  *(Phase 2)*

| Item                    | Location                                                                                                             |
| ----------------------- | -------------------------------------------------------------------------------------------------------------------- |
| `kind: Pipeline` type   | `internal/types/pipeline.go`                                                                                         |
| `kind: TestSuite` type  | `internal/types/testsuite.go`                                                                                        |
| Executor (seq/par/graph)| `internal/engine/pipeline.go`, `internal/engine/pipeline_registry.go`                                                |
| Test runner + assertions| `internal/runner/runner.go`                                                                                          |
| CLI subcommand          | `cmd/mockagents/test.go`                                                                                             |
| Loader dispatch         | `internal/config/loader.go` `LoadAllDocuments`                                                                       |
| Schemas                 | `schema/mockagents-v1-pipeline.json`, `schema/mockagents-v1-testsuite.json`                                          |
| Examples                | `examples/research-pipeline.yaml`, `examples/web-researcher.yaml`, `examples/summary-writer.yaml`, `examples/research-suite.yaml` |
| Tests                   | `internal/engine/pipeline_test.go` (3), `internal/runner/runner_test.go` (3)                                         |
| Verification            | **Live**: `mockagents test --agents-dir examples examples/research-suite.yaml` → `1 passed, 0 failed`                |

Topology semantics:
- `sequential` — previous node output feeds next node's user message
- `parallel` — fan-out via goroutines, same input to every agent
- `graph` — start node = first with no inbound edge, edges have optional `when_contains` substring guards

Assertion types: `tool_call`, `response_contains`, `scenario_matched`,
`latency_ms_lt`. Pipeline-target cases support `node_id` to scope an
assertion to a specific node.

### 2.3 Chaos engineering  *(Phase 3)*

| Item                     | Location                                                |
| ------------------------ | ------------------------------------------------------- |
| Expanded `ChaosConfig`   | `internal/types/behavior.go`                            |
| Injector middleware      | `internal/engine/chaos.go`                              |
| Wired into ProcessRequest| `internal/engine/engine.go`                             |
| HTTP error translation   | `internal/adapter/chaos.go`, `openai.go`, `anthropic.go`|
| Example agent            | `examples/chaos-agent.yaml`                             |
| Tests                    | `internal/engine/chaos_test.go` (9)                     |
| Schema                   | `schema/mockagents-v1-agent.json` (ChaosConfig block)   |

Capabilities:
- Latency — `fixed` / `uniform` / `normal` distributions
- Errors — single `status_code`, random pick from `status_codes`, or `timeout` mode with `timeout_ms` sleep
- Rate limiting — rolling window token bucket, 429 + `Retry-After`

Deterministic tests: injectable `Now`, `Sleep`, `RandSrc` on
`ChaosInjector`. `ChaosError` is propagated to the adapters which set
the right provider error-type label (`rate_limit_error`,
`authentication_error`, `overloaded_error`, etc.).

### 2.4 Record and playback  *(Phase 2)*

| Item                  | Location                                         |
| --------------------- | ------------------------------------------------ |
| Cassette store        | `internal/recording/cassette.go`                 |
| Proxy (record)        | `internal/recording/proxy.go`                    |
| Replay handler        | `internal/recording/replay.go`                   |
| CLI: record / replay  | `cmd/mockagents/record.go`, `cmd/mockagents/replay.go` |
| Tests                 | `internal/recording/recording_test.go` (8)       |

Cassette format: JSON lines, atomic tmpfile+rename on append.
Request hash canonicalizes JSON (sorted keys at every level) so SDK
reorderings still hit the same entry. Streaming is **not** captured in
v1 — the proxy buffers the response body and stores it as JSON.

### 2.5 CrewAI / LangGraph adapters (Python SDK)  *(Phase 2)*

| Item                 | Location                                                         |
| -------------------- | ---------------------------------------------------------------- |
| Adapter subpackage   | `sdk/python/mockagents/adapters/{__init__,_common,langchain,crewai}.py` |
| Lazy imports         | `require_module()` — raises install hint when peer absent        |
| Tests                | `sdk/python/tests/test_adapters.py` (11, no real peers installed) |
| Optional extras      | `sdk/python/pyproject.toml` `[project.optional-dependencies]`    |

Factories: `chat_openai(server)`, `chat_anthropic(server)`,
`crewai_mock_llm(server)`, plus `patched_env(server)` context manager
for LangGraph prebuilt agents that build their own ChatModel from env.

### 2.6 MCP server mocking  *(Phase 3)*

| Item                   | Location                                          |
| ---------------------- | ------------------------------------------------- |
| `kind: MCPServer` type | `internal/types/mcp.go`                           |
| JSON-RPC 2.0 dispatch  | `internal/mcp/jsonrpc.go`, `internal/mcp/server.go` |
| HTTP transport         | `internal/mcp/http.go` (POST /mcp)                |
| stdio transport        | `internal/mcp/stdio.go`                           |
| CLI                    | `cmd/mockagents/mcp.go`                           |
| Schema                 | `schema/mockagents-v1-mcpserver.json`             |
| Example                | `examples/weather-mcp.yaml`                       |
| Tests                  | `internal/mcp/mcp_test.go` (15)                   |
| Verification           | **Live**: `curl POST /mcp` returned `"Tokyo: sunny, 22C"` tool_call and an expanded `prompts/get` |

Methods: `initialize`, `tools/list`, `tools/call` (match + default),
`resources/list`, `resources/read`, `prompts/list`, `prompts/get` (with
`{{arg}}` substitution), `ping`, `notifications/initialized`.
Notifications return no response (HTTP 204 on the HTTP transport).
Streaming notifications / `completion/complete` / `roots` / `sampling`
remain out of scope for v1.

### 2.7 Contract testing  *(Phase 4)*

| Item               | Location                                            |
| ------------------ | --------------------------------------------------- |
| Contract + diff    | `internal/contract/contract.go`                     |
| CLI                | `cmd/mockagents/contract.go`                        |
| Tests              | `internal/contract/contract_test.go` (9)            |

`Extract(AgentDefinition) → Contract` is deterministic (tools +
scenarios sorted; parameters JSON-round-tripped so YAML vs JSON loader
paths produce identical trees — a subtle bug caught live).
`Diff(old, new) → []Change` classifies:

- **breaking**: tool removed, new required param, property schema
  change, scenario removed, streaming disabled, protocol change
- **additive**: tool added, required param relaxed, property added,
  scenario added, streaming enabled
- **info**: model name change, description change

`mockagents contract diff` exits non-zero on any breaking change, so it
drops cleanly into CI. Both arguments accept either agent YAML or
already-extracted contract JSON (auto-detected by first character).

### 2.8 OpenTelemetry tracing  *(Phase 4)*

| Item                 | Location                                    |
| -------------------- | ------------------------------------------- |
| Tracer provider      | `internal/observability/tracing.go`         |
| Engine instrumentation | `internal/engine/engine.go` `ProcessRequestContext` |
| HTTP middleware      | `internal/observability/tracing.go` + `server.go` |
| Tests                | `internal/observability/tracing_test.go` (5)|

Defaults to **NoOp** — zero runtime cost until an env var opts in:

| Env                              | Effect                                   |
| -------------------------------- | ---------------------------------------- |
| `OTEL_EXPORTER_OTLP_ENDPOINT=...`| OTLP/HTTP exporter (no gRPC on the hot path) |
| `MOCKAGENTS_OTEL_STDOUT=1`       | Pretty-print spans to stdout             |

Span shape: outer `http.request` (method, route, status_code) wraps
inner `engine.process_request` (agent.name, agent.model, agent.protocol,
agent.scenario, agent.tool_calls). Chaos errors, empty-message, and
generation failures mark the span `Error`.

New deps: `go.opentelemetry.io/otel`, `sdk`, `exporters/otlp/otlptrace/otlptracehttp`, `exporters/stdout/stdouttrace`, `semconv/v1.26.0`.

### 2.9 TypeScript SDK  *(Phase 4)*

| Item               | Location                                |
| ------------------ | --------------------------------------- |
| Package root       | `sdk/typescript/`                       |
| Client             | `sdk/typescript/src/client.ts`          |
| Server manager     | `sdk/typescript/src/server.ts`          |
| Scenario runner    | `sdk/typescript/src/scenario.ts`        |
| Assertions         | `sdk/typescript/src/assertions.ts`      |
| Adapters           | `sdk/typescript/src/adapters/`          |
| Tests (vitest)     | `sdk/typescript/tests/*.test.ts` (25)   |
| Build              | `npm run build` → `dist/` via tsc       |

Zero runtime deps — uses Node 18+ native `fetch`. Adapters for
`@langchain/openai`, `@langchain/anthropic`, `@ai-sdk/openai`, plus a
`patchEnv` helper. Makefile: `make test-typescript`, `make test-all`
now runs Go + Python + TypeScript.

### 2.10 Go SDK  *(Phase 4)*

| Item               | Location                                                 |
| ------------------ | -------------------------------------------------------- |
| Package            | `sdk/go/mockagents/` (same module as the monorepo root)  |
| Client             | `sdk/go/mockagents/client.go`                            |
| Server manager     | `sdk/go/mockagents/server.go`                            |
| Scenario           | `sdk/go/mockagents/scenario.go`                          |
| Expect helpers     | `sdk/go/mockagents/expect.go` (integrates with `testing.TB`) |
| Tests              | `sdk/go/mockagents/*_test.go` (17)                       |

Cross-SDK parity: Python, TypeScript, and Go SDKs now expose the same
shape — `Server` lifecycle, `Client` with chat/message/health/listAgents,
`Scenario` + run, fluent `Expect`. No framework adapters because Go
apps typically call the OpenAI/Anthropic APIs directly.

### 2.11 Web console GUI v0.1  *(Phase 4)*

| Item            | Location                                      |
| --------------- | --------------------------------------------- |
| Next.js 15 app  | `gui/`                                        |
| API client      | `gui/lib/api.ts`                              |
| Agent catalog   | `gui/app/page.tsx`                            |
| Agent detail    | `gui/app/agents/[name]/page.tsx`              |
| Logs view       | `gui/app/logs/page.tsx`                       |
| Styling         | `gui/app/globals.css` (no Tailwind)           |
| Verification    | **Live**: `next build` + `next start` rendered 9 agent cards and empty-logs state against a running Go server |

Reads from `MOCKAGENTS_API_URL` (defaults to `http://localhost:8080`).
Out of v0.1: workflow editor, config editor, WebSocket live updates,
auth (read-only dev tool). Makefile: `gui-dev`, `gui-build`.

### 2.12 Helm chart  *(Phase 4)*

| Item               | Location                                      |
| ------------------ | --------------------------------------------- |
| Chart              | `deploy/helm/mockagents/`                     |
| CI test values     | `deploy/helm/mockagents/ci/test-values.yaml`  |
| Makefile targets   | `helm-lint`, `helm-template`, `helm-package`  |
| Verification       | `helm lint` clean, `helm template` renders 6 resources, `helm package` produces 6 KB tgz |

Resources rendered: ServiceAccount, ConfigMap (inline agents), Service,
Deployment (non-root, read-only rootfs, probes, env, extraArgs),
Ingress (gated), `helm test` Pod that wget's `/api/v1/health`.
Agent definitions come from either `agents.inline` or
`agents.existingConfigMap`.

### 2.13 Multi-tenant auth + RBAC  *(Phase 4 — first SaaS slice)*

| Item                 | Location                                        |
| -------------------- | ------------------------------------------------ |
| Tenancy package      | `internal/tenancy/{types,store,middleware}.go`  |
| CRUD handlers        | `internal/server/tenancy_handlers.go`            |
| Server wiring        | `internal/server/server.go` `skipAuth`           |
| Bootstrap            | `cmd/mockagents/start.go` `bootstrapTenancy`     |
| Tests                | `internal/tenancy/tenancy_test.go` (11)          |
| Verification         | **Live**: bootstrap printed admin key; role matrix verified end-to-end (200/401/403) |

Opt-in via `MOCKAGENTS_MULTI_TENANT=1`. SQLite backend
(`.mockagents-tenancy.db`), API keys bcrypt-hashed, prefix-indexed
lookup, three roles (`viewer` < `editor` < `admin`). Bootstrap prints
the admin key to stderr exactly once; re-runs detect the existing key.

Critical bug caught live: `Resolve` was deadlocking against itself
because `SetMaxOpenConns(1)` held the connection inside a still-open
`Rows` iterator while `UPDATE last_used` ran. Fix: drain candidates
into memory and close rows before the update. Documented inline.

**Scope limit**: this slice protects only the control plane (`/api/v1/*`).
LLM endpoints (`/v1/chat/completions`, `/v1/messages`, `/v1/models`)
remain open — clients send their own provider API keys which MockAgents
deliberately ignores.

---

## 3. CLI Commands Shipped

```
mockagents init      start    validate   logs     test
                     record   replay     mcp      contract
```

- `init` — scaffold project
- `start` — run HTTP mock server (supports `MOCKAGENTS_MULTI_TENANT=1`)
- `validate` — lint agent YAML against schema
- `logs` — query interaction logs
- `test` — run TestSuite YAML against agents or pipelines
- `record` — proxy an upstream LLM API and capture to cassette
- `replay` — serve a cassette over the mock endpoints
- `mcp` — serve a `kind: MCPServer` over HTTP or stdio
- `contract extract | diff` — extract or diff agent contracts (CI-friendly)

---

## 4. Document Kinds Supported

Every YAML document under `--agents-dir` is dispatched by its `kind:`
field in `config.LoadAllDocuments`:

| Kind         | Purpose                                              |
| ------------ | ---------------------------------------------------- |
| `Agent`      | Mock LLM agent                                       |
| `Pipeline`   | Multi-agent topology (sequential/parallel/graph)     |
| `TestSuite`  | Declarative tests for agents or pipelines            |
| `MCPServer`  | Mock Model Context Protocol server                   |

Schemas live under `schema/mockagents-v1-{agent,pipeline,testsuite,mcpserver}.json`.

---

## 5. Test Suite Footprint

| Package                            | Tests | Notes                                                   |
| ---------------------------------- | ----- | ------------------------------------------------------- |
| `internal/adapter`                 | many  | OpenAI + Anthropic + token estimation + conformance    |
| `internal/build`                   | many  | Binary smoke + conformance                              |
| `internal/cli`                     | many  | Scaffold + integration                                  |
| `internal/config`                  | many  | Loader, validator, defaults, kind dispatch              |
| `internal/contract`                | 9     | Diff severity classification                            |
| `internal/engine`                  | many  | Scenario matcher, tool processor, template gen, **pipeline (3)**, **chaos (9)** |
| `internal/engine/state`            | many  | Session store                                           |
| `internal/mcp`                     | 15    | JSON-RPC dispatch, HTTP, stdio                          |
| `internal/observability`           | 5     | Span attributes, HTTP middleware                        |
| `internal/recording`               | 8     | Cassette hash, proxy, replay, full round-trip           |
| `internal/runner`                  | 3     | Agent + pipeline target, assertion failure paths        |
| `internal/server`                  | many  | Handlers, middleware, conformance, security             |
| `internal/storage`                 | many  | SQLite log store                                        |
| `internal/streaming`               | many  | SSE chunking, tool-call streaming                       |
| `internal/tenancy`                 | 11    | Store CRUD, auth middleware, RBAC                       |
| `sdk/go/mockagents`                | 17    | Client, scenario, expect                                |
| `sdk/python/mockagents` (pytest)   | 76    | Includes 11 adapter tests                               |
| `sdk/typescript/mockagents` (vitest)| 25    | Client, scenario, assertions, server, adapters         |

Running `make test-all` covers Go + Python + TypeScript. GUI type
safety is covered by `next build` running `tsc --strict`.

---

## 6. Known Gaps (intentional)

From the SaaS slice README and per-feature READMEs, the following are
deferred deliberately:

| Area             | Gap                                                      | Earmarked for          |
| ---------------- | -------------------------------------------------------- | ---------------------- |
| Multi-tenancy    | Tenant-scoped agent data isolation (engine rewire)       | Next SaaS slice        |
| Multi-tenancy    | Postgres store, SSO/OAuth, key rotation, billing/quotas  | Future SaaS slices     |
| Record/replay    | Streaming response capture (SSE passthrough)             | Follow-up              |
| Go SDK           | Streaming helpers, in-process engine mode                | Follow-up              |
| Python SDK       | Streaming helper parity with chat_stream                 | Follow-up              |
| TypeScript SDK   | Streaming helper, WebSocket/SSE integration              | Follow-up              |
| GUI              | Workflow editor, config editor, WS live feed, auth       | GUI v0.2               |
| Helm             | HPA, NetworkPolicy, Prometheus ServiceMonitor            | Helm v0.2              |
| MCP              | Streaming notifications, `completion/complete`, `sampling`, `roots` | MCP v0.2    |
| Hot reload (US-2.3) | fsnotify auto-watcher (manual reload endpoint exists) | Carry-over             |
| Performance (US-12.2) | Published benchmark report, pprof bottleneck audit | Carry-over             |

---

## 7. How to Update This File

When you ship a new slice:

1. Add a subsection under §2 describing what landed, with a table of
   file paths and a tests row.
2. If it adds a new kind of document, extend §4.
3. If it adds a new package or bumps an existing test count meaningfully,
   update §5.
4. If it closes a gap, remove the row from §6.
5. Bump the "Last updated" date at the top.

Keep rows tight — a future reader should be able to answer "what
actually works today?" in under 60 seconds by skimming this file.
