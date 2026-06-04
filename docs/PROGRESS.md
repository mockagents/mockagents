# MockAgents — Implementation Progress

**Last updated:** 2026-06-04 (GUI console redesign — "MockAgents Console" design system — §2.55)
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
| Phase 4           | Contracts, OTel, SDKs, GUI, Helm, multi-tenancy           | **v0.3 slices in flight**     |

All residual Phase 1 P1 carry-overs are now closed:

- US-2.3 (hot reload) closed by the CI readiness slice (§2.14).
- US-12.2 (performance benchmarking) closed by the benchmark report
  slice (§2.18): `make bench-report` publishes a refreshed JSON +
  Markdown artifact under `docs/benchmarks/`, and the baseline pprof
  hotspot pass is documented there.

---

## 1A. Resume Notes (handoff checkpoint)

> **For future sessions: read this section first.** It is the single
> entry point that explains where the codebase sits and what to
> tackle next. The rest of this file is the slice-by-slice ledger.

### Where we are right now (2026-05-30)

**Phase 1 MVP**: Complete with no residual P1s.
**Phase 2 / 3**: All v0.1 slices landed (multi-agent, record/replay,
framework adapters, chaos, MCP).
**Phase 4 v0.1**: All slices landed (contracts, OTel, TS/Go SDKs,
GUI v0.1, Helm v0.1, multi-tenant auth, audit, cost estimation,
streaming cassettes).
**Phase 4 v0.2**: Complete — perf workstream (§§2.18–2.24), Python
+ TypeScript SDK streaming, GUI v0.2 cost/audit/log-detail/live
feed, Helm v0.2 (HPA/PDB/NetworkPolicy/ServiceMonitor), MCP v0.2
(completion/logging/notifications), tenant-scoped agent isolation.
**Phase 4 v0.3 (current focus)**: 22 slices landed —
Go SDK streaming + in-process (§2.31), GUI admin auth (§2.32),
MCP v0.3 bidirectional (§2.33), GUI live feed via SSE (§2.34),
GUI config editor (§2.35), GUI pipeline DAG viewer (§2.36), API
key rotation (§2.37), pipeline validator + CLI multi-kind
validate (§2.38), Python SDK MCP bidirectional helper (§2.39),
TypeScript SDK MCP bidirectional helper (§2.40), Go SDK MCP
bidirectional helper (§2.41), pipeline graph checks — cycles
and unreachable nodes (§2.42), TestSuite + MCPServer rule-based
validators (§2.43), SSE drop-count signal for slow subscribers
(§2.44), pipeline edge polish — whitespace `when_contains` and
duplicate edge detection (§2.45), cross-document reference
checking — pipelines verify their agent refs, test suites
verify their targets and assertion node_ids (§2.46), self-
rotation — authenticated operators can rotate their own key
in place without an admin credential (§2.47), aggregate
SSE stream metrics — admin-gated snapshot of every
connected subscriber's drop count and buffer utilization
(§2.48), bulk tenant-key rotation — all-or-nothing
transactional rotation of every key in a tenant as an
emergency response to suspected compromise (§2.49),
burn-session emergency rotation — self-rotation that
never returns the new plaintext to a compromised
browser (§2.50), selective bulk rotation —
`?except=self` lets admins rotate every key in a
tenant without locking themselves out (§2.51),
architecture review hardening — localhost bind by
default, principal-derived tenant scope, tenant-scoped
observability, and same-session atomic turn mutation (§2.52),
and a two-package multi-pass security review hardening pass —
cross-tenant key IDOR + tenant-CRUD privilege fixes (platform
role), live-feed routing/flush/shutdown/isolation repairs, route-
authz unification, and a broad auth/validation hardening sweep,
all neuter-verified (§2.53), and a performance handoff
(`docs/PERFORMANCE.md`) plus the four P1 hot-path optimizations —
an O(1) tenant model index, skipping the no-op tracing wrapper,
coarsened auth `last_used` writes, a pooled response encoder, and
two cheap wins (memoized match lowering, single-copy body capture),
each measured and neuter-verified (§2.54), and the full GUI console
redesign to the "MockAgents Console" design system — a `--sr-*` token +
legacy-var alias foundation, a new sidebar shell with light/dark toggle,
and every existing surface (catalog, agent detail, logs, costs, audit,
pipelines, editor, tenants/keys, account) rebuilt to the design, with the
§2.53 GUI security posture preserved (§2.55). All
four document kinds (Agent, Pipeline, TestSuite, MCPServer) flow
through the same rule-based validator plumbing AND a second
cross-document pass resolves every reference across a directory.
All three SDKs ship identical `McpClient` surfaces. The GUI
live feed surfaces server-side backpressure as a sticky badge.
The drag-to-rewire workflow editor and SaaS-tier SSO/Postgres/
billing are the only known gaps left.

### What landed since the v0.1 freeze

| Slice                                                  | Section |
| ------------------------------------------------------ | ------- |
| Cost estimation engine + log cost annotation           | §2.17   |
| Bounded log worker pool (replaced unbounded fan-out)   | §2.20   |
| Auth cache for tenancy Resolve (bcrypt skip)           | §2.21   |
| SQLite multi-conn pool + synchronous=NORMAL            | §2.22   |
| captureWriter sync.Pool                                | §2.23   |
| Adapter JSON decode buffer pool                        | §2.24   |
| Benchmark report + profiling workflow (US-12.2)        | §2.18   |
| Zero-risk micro-optimization slice (-10 to -24 % hot path) | §2.19 |
| Python SDK streaming helper parity                     | §2.25   |
| GUI v0.2 — costs, audit, log detail, live feed         | §2.26   |
| Helm v0.2 — HPA, PDB, NetworkPolicy, ServiceMonitor    | §2.27   |
| TypeScript SDK streaming                               | §2.28   |
| MCP v0.2 — completion, logging, notifications          | §2.29   |
| Tenant-scoped agent isolation                          | §2.30   |
| Go SDK streaming + in-process engine mode              | §2.31   |
| GUI v0.3 — admin auth (login + tenants + keys)         | §2.32   |
| MCP v0.3 — bidirectional transport (sampling/roots)    | §2.33   |
| GUI v0.3 — real live feed (server-sent events)         | §2.34   |
| GUI v0.3 — schema-aware config editor                  | §2.35   |
| GUI v0.3 — pipeline DAG viewer + mgmt API              | §2.36   |
| Multi-tenancy — API key rotation                       | §2.37   |
| Pipeline validator + CLI multi-kind validate           | §2.38   |
| Python SDK MCP bidirectional helper                    | §2.39   |
| TypeScript SDK MCP bidirectional helper                | §2.40   |
| Go SDK MCP bidirectional helper                        | §2.41   |
| Pipeline graph checks (cycles + unreachable)           | §2.42   |
| TestSuite + MCPServer rule-based validators            | §2.43   |
| SSE drop-count signal (slow-subscriber backpressure)   | §2.44   |
| Pipeline edge polish (when_contains + duplicate edges) | §2.45   |
| Cross-document reference checking                      | §2.46   |
| Self-rotation /me/rotate endpoint + /account page      | §2.47   |
| Aggregate SSE stream metrics endpoint                  | §2.48   |
| Bulk tenant-key rotation                               | §2.49   |
| Burn-session emergency rotation + /me/burn             | §2.50   |
| Selective bulk rotation (?except=self)                 | §2.51   |
| Architecture review hardening                          | §2.52   |
| Multi-pass security review hardening (server + tenancy) | §2.53   |
| Performance handoff + P1 hot-path optimizations         | §2.54   |

Test suite headcount at the checkpoint:
- Go: full suite green, `go vet` clean. Total **~721 Go tests**
  (646 at the §2.52 checkpoint + ~75 from the §2.53 security review,
  almost all in `internal/server` and `internal/tenancy`). Notable
  packages:
  - `internal/engine`: 144
  - `internal/config`: 127 (ValidateBytes +7, pipeline validator +9, graph checks +7, TestSuite +9, MCPServer +13, edge polish +5, cross-doc +9)
  - `internal/server`: **144** (was 88; +56 from §2.53 security review — RBAC/route-authz, SSE routing/shutdown/isolation, audit/costs handler coverage, secret-not-logged, IDOR/limits/error-envelope)
  - `internal/tenancy`: **46** (was 27; +19 from §2.53 — cross-tenant IDOR, bulk-rotate rollback, fail-closed auth, platform role, cache/redaction/timing hardening)
  - `sdk/go/mockagents`: 44 (streaming +12, in-process +5, MCP +11)
  - `internal/mcp`: 38 (bidirectional +9 in §2.33)
  - `internal/streaming`: 34
  - `internal/adapter`: 32
  - `internal/tenancy`: 27 (rotation +3 in §2.37)
  - `sdk/go/mockagents`: 33 (streaming + in-process +17 in §2.31)
- Python SDK: **104 tests** (was 76 at v0.1 freeze; +14 MCP
  bidirectional helper in §2.39).
- TypeScript SDK: **53 tests** (was 25 at v0.1 freeze; +15 MCP
  bidirectional helper in §2.40).
- GUI: `npm run build` clean under `tsc --strict`; **15 routes**
  (2 at v0.1 → 15 at v0.3). `/editor`, `/pipelines`,
  `/pipelines/[name]`, `/admin/tenants`, `/admin/tenants/[id]`,
  `/login`, `/api/logs/stream` are the seven v0.3 additions.
- Helm: lint clean, default render (6 resources), full v0.2
  toggle render (10 resources).
- Benchmarks: `docs/benchmarks/latest.{json,md}` **refreshed 2026-06-03**
  off-governor (temp High-performance plan, Balanced restored) — current
  with the engine-review slice and the §2.54 perf work. `allocs/op` flat
  within ±1 vs. the 2026-04-14 baseline (three benches improved), and the
  PERF-01 `GetByModelForTenant_ManyAgents` index lands at 14.3 ns / 0
  allocs. ns/op swings are full-sweep thermal noise (see the 2026-06-03
  refresh note in `docs/benchmarks/README.md`; backlog in
  `docs/PERFORMANCE.md`).

### Recommended next task

Pick one of the remaining §6 items below. Suggested ranking by
ROI vs. complexity:

1. **GUI v0.3 (continued)** — the DAG viewer landed in §2.36;
   a full **workflow editor** (drag-to-rewire, inline YAML
   sync, schema-aware node property editor) is still open.
   Admin auth (§2.32), real live feed (§2.34), schema-aware
   config editor (§2.35), and the read-only DAG viewer (§2.36)
   are done. The editor slice needs a DAG widget (React Flow
   or similar) and is the biggest remaining GUI item.
2. **SaaS-tier multi-tenancy** — Postgres tenancy store, SSO/OAuth,
   per-tenant agent name collisions, key rotation, billing/quotas.
   These need their own design discussion before implementation.

### How to resume

1. Read this section (§1A) and the Status Overview above.
2. Check `docs/benchmarks/latest.md` for the current performance
   baseline — every perf change must keep this green.
3. `make test` and `cd sdk/python && pytest` and `cd sdk/typescript
   && npm test` — all three should be green before you touch
   anything.
4. Pick a §6 item. Add a §2.x entry to this file when you finish,
   bump the "Last updated" banner, refresh §5 test counts, and
   remove the row from §6.
5. Update the matching backlog row in `docs/product-backlog.md`
   §1A "Phase 2-4 slices landed" table.

### Active conventions worth preserving

- **No new dependencies without a measured benefit** (≥2× on a hot
  path benchmark). The perf review explicitly deferred `sonic` /
  `jsoniter` because the pooled-buffer decode (§2.24) hit -39 % B/op
  with stdlib only.
- **Tenancy import direction**: `tenancy` may import `engine` but
  not vice versa. Use `engine.WithTenantID` / `engine.TenantIDFromContext`
  instead of importing `tenancy` from `engine`.
- **Audit recorder import direction**: `audit` does not import
  `tenancy`. The server wires `principalToActor` and the denial
  hook in `server.New`.
- **Page files in `gui/app/`** can only export `default` plus a
  small set of config keys — extract any helper components into
  sibling `.tsx` files.
- **Benchmarks are checked in**: `docs/benchmarks/latest.{json,md}`
  is regenerated by `make bench-report`. Treat regressions outside
  the target envelope (see `docs/benchmarks/README.md`) as bugs.

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
| Tests                 | `internal/recording/recording_test.go` (11)      |

Cassette format: JSON lines, atomic tmpfile+rename on append.
Request hash canonicalizes JSON (sorted keys at every level) so SDK
reorderings still hit the same entry.

**Streaming capture** (2026-04-14): SSE responses are now tee'd in
`Proxy.serveStreaming`. Each upstream read buffer is forwarded to
the client (with a `Flusher.Flush()` per chunk to preserve
incremental delivery) and simultaneously appended to
`Interaction.StreamEvents` alongside a `DelayMs` offset from the
start of the response. Cassettes carry a `streaming: true` marker
so `Replay.serveStreaming` knows to emit `Content-Type:
text/event-stream`, flush headers, and push each captured chunk
back in order. Replay is fast by default (ignores the recorded
delays) so CI suites stay deterministic; set
`Replay.PreserveStreamDelays = true` for demos where realistic
pacing matters. Client disconnects mid-stream still yield a
partial cassette entry — the captured prefix is usable on replay.

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

### 2.50 Burn-session emergency rotation  *(tenancy + GUI)*

| Item                       | Location                                                 |
| -------------------------- | -------------------------------------------------------- |
| `BurnMyAPIKey` handler     | `internal/server/tenancy_handlers.go` — rotates via existing `Store.RotateAPIKey`, discards the returned plaintext without writing it, returns 204 No Content; emits `api_key.rotated` audit with `self: true` AND `burn: true` details |
| Route mount                | `internal/server/server.go` `POST /api/v1/keys/me/burn` behind `RequireRole(RoleViewer, ...)` — same "read key id from context" contract as /me/rotate |
| GUI API helper             | `gui/lib/api.ts` `burnMyAPIKey()` |
| `burnSession` server action | `gui/lib/auth.ts` — calls the API, deletes `AUTH_COOKIE` + `ROLE_COOKIE` locally, returns `{ok}`/`{ok:false,error}` |
| /account page burn UI      | `gui/app/account/page.tsx` — "Burn this session" danger link, two-click confirmation via `?burn=confirm` query param, inline warning banner with Yes/Cancel |
| /login burn notice         | `gui/app/login/page.tsx` — renders a "session burned" banner when `?burned=1` is present so the operator sees confirmation post-redirect |
| Tests                      | `internal/server/tenancy_handlers_test.go` — 2 new: `BurnMyAPIKey` (HTTP 204, empty body, old plaintext stops resolving, key row still present with a new prefix) and `BurnMyAPIKey_Unauthenticated` (missing principal → 401) |
| Verification               | **Live**: `go test ./internal/server/...` runs **87 tests** (was 85 → 87, +2); full `go test ./...` across 21 packages green; `npx next build` clean with 16 routes |

Closes the **rotate-then-logout flow** follow-up explicitly
deferred in §2.47. This is the operator's response to a
*confirmed* compromise of the current browser session — not a
suspected leak that can safely re-use the new plaintext, but a
situation where the new secret must never touch the current
machine. Recovery happens through an out-of-band channel.

What landed:

- **`POST /api/v1/keys/me/burn`.** Mirrors `/me/rotate` — reads
  the caller's Principal from the request context, calls
  `Store.RotateAPIKey(principal.KeyID)` to rotate in place, then
  deliberately discards the returned `*NewAPIKeyResult` before
  writing the response. Returns 204 No Content with an empty
  body. Defensive: the handler explicitly zeros
  `result.Plaintext` and nils the pointer before the 204, so the
  plaintext is out of scope by the time the response header
  flushes. The Go runtime may still keep a copy in bcrypt's
  scratch buffer, but that's the same threat model as every
  other rotation path.
- **`burn: true` audit detail.** Reuses the existing
  `EventAPIKeyRotated` event kind so the audit filter surface
  stays simple. The `burn: true` field in Details
  distinguishes this path from both regular self-rotation
  (§2.47) and bulk rotation (§2.49) — operators who want to
  grep for "this is a suspected compromise" can filter on
  `burn:true` specifically.
- **`burnSession` server action.** New function in
  `gui/lib/auth.ts` that calls `burnMyAPIKey`, then deletes
  both `AUTH_COOKIE` and `ROLE_COOKIE` locally via
  `cookies().delete`. Returns `{ok: true}` on success and
  `{ok: false, error}` on any transport failure so the
  caller can render an inline error without crashing.
- **/account page burn button + two-click confirm.** The
  primary "Burn this session" control is a danger-style
  `<Link>` (not a button) that points at
  `/account?burn=confirm`. Clicking it renders a red
  confirmation banner inline on the same page with two
  buttons: "Yes, burn it" (fires the real `burnAction`
  server action) and "Cancel" (navigates back to
  `/account`). Two-click gating is important because
  Burn is irreversible — the new plaintext is gone by the
  time the operator sees their mistake.
- **/login burned=1 notice.** After a successful burn, the
  `burnAction` redirects to `/login?burned=1`. The login
  page picks up the query param and renders a yellow
  "session burned" banner above the form explaining the
  situation and the recovery path (out-of-band channel).
  Without this, an operator who just burned their session
  sees a naked login form and wonders what happened.

Design notes:

- **Why a new endpoint instead of `POST /me/rotate?burn=true`?**
  Query-param flags on POST endpoints are easy to miss in a
  code audit — a reviewer skimming "rotate endpoint" might not
  notice the flag. A separate `/me/burn` path makes the
  security-relevant semantics obvious from the URL alone:
  anything that hits this endpoint discards the plaintext,
  full stop. The two handlers share all their logic through
  `Store.RotateAPIKey`, so there's no code duplication.
- **Why `btn-danger` on a Link, not a form?** Rendering the
  primary "Burn this session" control as a form button would
  let the operator fire the server action with a single
  click — an accidental Enter keypress while the element had
  focus could burn a session. Gating the first click on a
  navigation (which just adds a query param to the URL) and
  the second click on an explicit red "Yes, burn it" button
  makes an accidental two-step burn vanishingly unlikely.
- **Why delete both cookies locally?** `AUTH_COOKIE` holds
  the old plaintext that's now invalid server-side —
  keeping it would send dead credentials on every subsequent
  request, flooding the auth denial logs. `ROLE_COOKIE`
  holds the inferred role — stale without a valid auth
  cookie anyway, and keeping it would confuse
  `getAuthStatus()` into thinking there's still a session.
- **Why explicitly zero `result.Plaintext`?** Paranoia. The
  struct will be GC'd as soon as the handler returns and
  the plaintext is a Go string (immutable — we can't
  actually zero the bytes). But assigning `""` clears the
  reference, and setting the pointer to `nil` makes the
  intent obvious to anyone reviewing the code for a
  "where does this plaintext go?" question. The compiler
  may optimize the assignment away, which is fine — the
  plaintext is already invisible to any caller of this
  handler because it's never written to `w`.
- **Audit reuses `api_key.rotated`, not a new kind.** Adding
  `api_key.burned` would bloat the audit kind enum and
  force operators to learn a new filter. The `burn: true`
  detail is cheaper and still lets anyone who cares
  distinguish the three rotation flows (admin rotate,
  self rotate, self burn, bulk rotate) via a single
  query filter.

Regression guards:

- **`TestTenancyHandlers_BurnMyAPIKey`** — full HTTP round-
  trip with injected Principal: response is 204, body is
  empty, old plaintext stops resolving, the key row still
  exists (burn ≠ delete) with a new prefix.
- **`TestTenancyHandlers_BurnMyAPIKey_Unauthenticated`** —
  missing Principal → 401 with a clear error.

What stayed deferred:

- **Server-side cookie clearing.** The `burnSession` action
  is a Next.js server action — the cookie clearing happens
  in the Node process, not in the Go server. A future
  slice could add `Set-Cookie: mockagents_api_key=; Max-Age=0`
  headers on the 204 response so non-Next callers (curl,
  third-party SDKs) also see the cookie invalidated. Low
  priority because the cookie is HttpOnly and never used
  outside the GUI.
- **Confirmation via a modal instead of a query param.**
  A proper modal dialog would be friendlier but requires a
  client component. The query-param flow is fine for the
  rare emergency-response scenario and keeps the /account
  page fully server-rendered.
- **Burn-all-my-tokens** (across devices). Today the burn
  only affects the calling key. An operator with multiple
  concurrent sessions (same key used from two browsers)
  only invalidates the one they're currently on. The
  feature would need per-device session tracking, which
  MockAgents deliberately doesn't have — a key is a key,
  not a session. Out of scope.

### 2.49 Bulk tenant-key rotation  *(tenancy + server + GUI)*

| Item                           | Location                                                 |
| ------------------------------ | -------------------------------------------------------- |
| `Store.BulkRotateTenantKeys`   | `internal/tenancy/store.go` — transactional rotation of every key for a tenant; returns parallel slices of `[]*NewAPIKeyResult` + `[]string` (old prefixes); flushes auth cache once on commit |
| Store interface update         | `internal/tenancy/store.go` `Store` interface — new method in the contract so `tenancy.Store` consumers see it |
| HTTP handler                   | `internal/server/tenancy_handlers.go` `BulkRotateTenantKeys` — emits one `api_key.rotated` audit event per key with `bulk: true` detail; wraps the results in a `BulkRotateResult { count, results }` envelope |
| Route mount                    | `internal/server/server.go` `POST /api/v1/tenants/{id}/keys/rotate` — admin-gated |
| GUI API helper                 | `gui/lib/api.ts` `bulkRotateTenantKeys(tenantId)` + `BulkRotateResult` type |
| GUI action + button            | `gui/app/admin/tenants/[id]/page.tsx` `bulkRotateAction` server action; "Rotate all keys" danger button next to the mint-key form; only rendered when at least one key exists |
| GUI bulk reveal banner         | `gui/app/admin/tenants/[id]/page.tsx` `BulkRotationEntry` type + reveal banner listing every fresh plaintext with its key name and prefix |
| GUI bulk-rotation list styles  | `gui/app/globals.css` `.bulk-rotation-list` |
| Store tests                    | `internal/tenancy/bulk_rotate_test.go` — 4 tests: rotates everything + resolves (full round-trip for 3 keys spanning every role), empty tenant no-op, unknown tenant error, auth cache flush |
| Handler tests                  | `internal/server/tenancy_handlers_test.go` — 2 new: `BulkRotateTenantKeys` (HTTP round-trip, old plaintext dies, new resolves) and `BulkRotateTenantKeys_UnknownTenant` (404) |
| Verification                   | **Live**: `go test ./internal/tenancy/...` runs **31 tests** (was 27 → 31, +4); `go test ./internal/server/...` runs **85 tests** (was 83 → 85, +2); full `go test ./...` across 21 packages green; `npx next build` clean with 16 routes |

Closes the **bulk rotation** follow-up explicitly deferred in
§2.37. When an operator suspects a tenant-wide credential
compromise — a leaked config volume, a malicious insider, a
stolen operator laptop — they need every key for that tenant
replaced atomically. The per-key Rotate button from §2.37 leaves
a window where some keys are still the old (compromised)
secrets; this slice gives operators a single-click emergency
response that rotates every key in one database transaction.

What landed:

- **`Store.BulkRotateTenantKeys(ctx, tenantID)`.** New interface
  method with two-slice return (`[]*NewAPIKeyResult`,
  `[]string`) where the indices line up — `results[i]` is the
  newly-rotated key and `oldPrefixes[i]` is the prefix that
  used to identify it (for audit-log correlation). The SQLite
  implementation:
  1. Verifies the tenant exists (clean `ErrNotFound` on typos
     instead of a silent empty-result).
  2. Opens a single `BeginTx` so the entire batch is atomic —
     if any individual bcrypt / update fails, the whole thing
     rolls back and NONE of the keys are rotated.
  3. Selects every key for the tenant ordered by
     `created_at ASC` (stable ordering across repeated calls).
  4. For each row: generates a fresh plaintext+prefix+hash,
     UPDATEs the row, appends the fresh `NewAPIKeyResult` and
     the old prefix to the result slices.
  5. Commits the transaction.
  6. Flushes the auth cache **once** after commit — every
     pre-rotation plaintext is now invalid and any cached
     Principal would otherwise linger until its TTL expired.
- **Empty tenant is a no-op.** A tenant with zero keys returns
  `(nil, nil, nil)`. We still commit the empty transaction for
  symmetry — keeps the code path uniform whether the tenant
  has 0, 1, or 1000 keys.
- **Tenant lookup happens first.** The `GetTenant` fast-path
  means "rotate everything for `ten_bogus`" fails with
  `ErrNotFound` instead of silently succeeding with empty
  results. Operators responding to a compromise deserve a
  loud error when they mistype an id.
- **HTTP handler.** `POST /api/v1/tenants/{id}/keys/rotate`
  is admin-gated via `RequireRole(RoleAdmin, ...)`. On
  success it emits one audit event per rotated key reusing
  the existing `api_key.rotated` kind — no new audit schema
  — with a `bulk: true` detail so operators who want to grep
  for batches can filter on it. On tenant-not-found returns
  404; on any store error returns 500 with the error
  message.
- **`BulkRotateResult` envelope.** Response is
  `{ "count": N, "results": [{key, plaintext}, ...] }` so
  callers can read the count without walking the array. The
  inner objects are the same `*tenancy.NewAPIKeyResult`
  shape that single-key rotation and creation return, so
  clients can reuse their existing parsing logic.
- **GUI "Rotate all keys" button.** Admin tenant detail page
  gained a danger-style button next to the mint-key form. On
  click, a server action calls `bulkRotateTenantKeys`, stashes
  the resulting `{id, name, prefix, plaintext}` entries in a
  URL query param, and redirects back to the same page. The
  reveal banner at the top of the page then lists every new
  secret alongside its key name + prefix in a vertical list
  — operators copy each one in turn before navigating away.
  The banner makes the same "once-only reveal" guarantee as
  the single-key rotation flow (§2.37).
- **Confirmation guardrails.** The button is styled `btn-danger`
  (red outline) and carries a warning `title` attribute so an
  accidental click is unlikely. A future slice could add an
  explicit "Are you sure?" modal — out of scope here.
- **Moved forms out of the mint row.** The bulk rotate button
  couldn't be nested inside the existing Mint-key `<form>`
  (HTML disallows nested forms), so the toolbar is now a
  `<div class="inline-form">` wrapping two sibling `<form>`
  elements side by side. Functionally equivalent, structurally
  valid.

Design notes:

- **Why transactional, not loop-of-RotateAPIKey?** A naive
  implementation could just loop `Store.RotateAPIKey` over
  every key in the tenant. That would work for the happy
  path but has a fatal failure mode: if the loop panics or
  the process crashes halfway through, half the keys would
  be rotated and half would still be the old compromised
  secrets — leaving the operator in a worse state than
  before they clicked the button. A single transaction
  guarantees all-or-nothing so operators never have to
  reason about partial state.
- **Why one audit event per key, not one aggregate event?**
  The audit query surface is keyed per-event on key id. A
  single "bulk rotation of N keys" event wouldn't let
  operators filter the audit log by key id to see "what
  happened to this specific compromised credential?". The
  `bulk: true` detail is the aggregate signal; the per-key
  events are the queryable surface.
- **Why parallel slices instead of a struct?** The Store
  interface is the only caller of this method and both
  slices are used by the handler together. A struct would
  add an indirection without making anything clearer.
  Parallel slices match the existing `RotateAPIKey`
  signature (which also returns `(*NewAPIKeyResult, oldPrefix,
  error)`).
- **Why stash the bulk payload in the URL, not a
  server-side session?** The single-key rotate flow
  already uses `?plaintext=…&name=…` query params, and
  consistency is worth more than an imagined "plaintext in
  URL is bad" concern: the URL is server-rendered only,
  never committed to browser history as an auth token. The
  plaintext is already persisted in the HttpOnly cookie
  for the caller's own session. Any paranoid deployment
  can swap this for a cookie-based "show once" flag —
  separate slice.
- **Bulk flush of auth cache, not per-key.** Doing
  `s.cache.Invalidate()` inside the loop would flush once
  per key and produce a brief cache-cold window for every
  other tenant sharing the cache — O(N) unnecessary work.
  A single flush after commit is O(1) and still preserves
  the "old plaintext cannot outlive rotation" invariant.

Regression guards:

- **`TestBulkRotateTenantKeys_RotatesEverything`** — three
  keys spanning all three roles → full round-trip:
  rotation returns three results, old plaintexts all die,
  new plaintexts all resolve to the same key id + role.
  Tests id, role, name, tenant_id are preserved and the
  reported old prefix matches each key's original prefix.
- **`TestBulkRotateTenantKeys_EmptyTenantIsNoOp`** — a
  tenant with zero keys returns empty slices and no error.
- **`TestBulkRotateTenantKeys_UnknownTenant`** — unknown
  tenant id returns `ErrNotFound`.
- **`TestBulkRotateTenantKeys_FlushesAuthCache`** — a
  cached Principal from a rotated key's old plaintext
  cannot linger past the bulk rotation.
- **`TestTenancyHandlers_BulkRotateTenantKeys`** — full
  HTTP round-trip: admin POST → response carries 2
  results, both old plaintexts dead, both new plaintexts
  resolve.
- **`TestTenancyHandlers_BulkRotateTenantKeys_UnknownTenant`**
  — unknown tenant → 404.

What stayed deferred:

- **Selective bulk rotation.** Operators can't yet say
  "rotate every key in this tenant EXCEPT the one I'm
  currently using" — they'd lock themselves out. For now
  the workaround is: rotate all → copy the new
  plaintext for your own session → paste it into a
  re-login. A future slice could add an `?except=self`
  query param that reads the caller's Principal and skips
  it.
- **Undo window.** There is no "rollback last bulk
  rotation" — the old secrets are bcrypt-hashed and
  immediately discarded. A future slice with a schema
  change could keep the previous hash for a short grace
  window, but that widens the attack surface (now TWO
  secrets can open the door) and requires its own threat
  model.
- **Progress indicator for large tenants.** The endpoint
  is synchronous; a tenant with thousands of keys would
  block the HTTP request for the bcrypt cost of every
  key (bcrypt is intentionally slow — ~100ms per key at
  default cost). If that becomes a pain point, a future
  slice could split the rotation into a background job
  with a polling status endpoint.
- **Per-tenant rate limiting.** Nothing prevents an admin
  from hammering `/keys/rotate` in a loop. In practice
  admins rotate rarely, but a future slice could add a
  cooldown that refuses a second bulk rotation within
  30 seconds to prevent accidental double-clicks.

### 2.48 Aggregate SSE stream metrics endpoint  *(server)*

| Item                       | Location                                                 |
| -------------------------- | -------------------------------------------------------- |
| `LogBroadcaster.Snapshot`  | `internal/server/log_broadcaster.go` — returns `BroadcasterSnapshot` with subscriber count, total + max drops, per-sub {dropped, buffer_cap, buffer_len}; sorted worst-offender-first; nil-safe |
| `BroadcasterSnapshot` type | `internal/server/log_broadcaster.go` — JSON-encodable public shape |
| `StreamMetrics` handler    | `internal/server/log_handlers.go` — `GET /api/v1/logs/stream/metrics` returns the snapshot; 503 when logging disabled |
| Route mount                | `internal/server/server.go` — admin-gated in multi-tenant, open in single-tenant (matches the rest of the log API) |
| Tests                      | `internal/server/log_broadcaster_test.go` — 3 new snapshot unit tests (empty, after drops across three subs, buffer-fill visibility) + 2 new HTTP handler tests (happy path + 503 path); existing nil-receiver test extended to also call `Snapshot()` |
| Verification               | **Live**: `go test ./internal/server/...` runs **83 tests** (was 78 → 83, +5); full `go test ./...` across 21 packages green |

Closes the **aggregate drop count across all subscribers**
follow-up explicitly deferred in §2.44. The per-subscription
SSE drop-count signal that slice added lets individual browser
tabs know they're falling behind; this slice adds the
operator-wide view so administrators can answer "is ANY tab
currently dropping?" without having to open every browser tab
themselves.

What landed:

- **`LogBroadcaster.Snapshot()`.** New method that acquires
  the broadcaster mutex once, iterates every subscription,
  reads the atomic drop counter + channel length/capacity,
  sorts the per-sub entries worst-offender-first, and
  returns a `BroadcasterSnapshot` value with aggregate
  counters (total + max) computed in the same pass. O(N)
  where N is subscriber count — and N is usually single-
  digit, so the sort is trivial.
- **`BroadcasterSnapshot` and `SubscriberSnapshot` types.**
  Public structs with JSON tags so the HTTP handler can
  encode them directly. The snapshot carries four numbers
  at the top level (`subscriber_count`, `total_dropped`,
  `max_dropped`, and the array itself) and three numbers
  per subscriber (`dropped`, `buffer_cap`, `buffer_len`).
  Subscribers are anonymous — the broadcaster has no
  identifying info to give, and operators who want to
  correlate a specific browser tab with a specific
  subscription can match by `(buffer_len, dropped)` pair
  during a short observation window.
- **`GET /api/v1/logs/stream/metrics` handler.** Thin
  wrapper: check `h.Broadcaster != nil`, call
  `Broadcaster.Snapshot()`, encode as JSON. Returns 503
  when the broadcaster is nil (logging disabled) rather
  than returning an empty snapshot that could be misread
  as "no drops anywhere".
- **Admin gate in multi-tenant mode.** The route is mounted
  behind `RequireRole(RoleAdmin, ...)` when
  `TenancyStore` is set so viewers can't fingerprint the
  operator's browser tabs via their buffer utilization
  patterns. Single-tenant deployments leave it open —
  matches the rest of the log management API.
- **Sorted worst-offender-first.** The per-subscription
  array is sorted descending by `Dropped` with a
  `BufferLen` tiebreaker. Operators skimming the JSON see
  the most-dropped subscription at index 0 without having
  to post-process.
- **Insertion sort for tiny N.** The sort helper is a
  hand-rolled insertion sort inside the broadcaster
  package — N is bounded by active subscriber count
  (single digits in practice), and an inline sort avoids
  a new `sort` package import inside the hot file.

Design notes:

- **Why not aggregate at Publish time?** A running
  "total drops across all subs" counter could be kept on
  the broadcaster struct and incremented in the Publish
  default branch. That would make Snapshot() O(1) instead
  of O(N). But: (a) Publish is the hottest path on the
  server, and adding an atomic increment there taxes every
  request even when no one is subscribed; (b) the
  aggregate counter wouldn't track per-sub drop counts,
  which are the actionable signal. The current "iterate at
  snapshot time" approach keeps the hot path free and
  computes aggregates only when someone explicitly asks.
- **Nil-safe Snapshot().** Matches the existing `Publish`
  and `SubscriberCount` nil-safety so `Broadcaster == nil`
  is uniformly OK. Defense-in-depth: the handler
  short-circuits before calling Snapshot, but a future
  caller that isn't as careful still won't crash.
- **Subscribers are anonymous in the snapshot.** The
  broadcaster doesn't know which HTTP client owns which
  channel — it just has `map[*LogSubscription]struct{}`.
  We could plumb through the request's remote address
  from the handler into the subscription object, but
  that adds coupling for a feature operators rarely
  need. If someone asks for per-tab identification we
  can wire it later.
- **Insertion sort, not `sort.Slice`.** The slice is
  usually length 1-10 (browser tabs + curl
  subscriptions). Insertion sort is faster than any
  general-purpose sort below ~30 elements and avoids the
  sort package's function-value indirection. Also keeps
  the sort predicate obvious at the call site.

Regression guards:

- **`TestLogBroadcaster_SnapshotEmpty`** — zero subs →
  zero counters, nil-safe iteration.
- **`TestLogBroadcaster_SnapshotAfterDrops`** — three
  subs with varying overflow states → aggregate totals
  match the sum of per-sub drops; max matches the
  largest; descending sort is respected.
- **`TestLogBroadcaster_SnapshotBufferFill`** — a
  subscriber with a 4-slot buffer that's 2/4 full
  reports `BufferCap: 4, BufferLen: 2, Dropped: 0`;
  after draining, `BufferLen` reports 0.
- **`TestStreamMetricsHappyPath`** — HTTP handler returns
  the snapshot as JSON with correct shape.
- **`TestStreamMetricsWithoutBroadcaster503`** — nil
  broadcaster → 503 with a clear error.
- **`TestLogBroadcaster_NilReceiverIsNoop`** (extended)
  — nil broadcaster's `Snapshot()` returns a zero value
  without panicking.

What stayed deferred:

- **Prometheus exporter.** A future slice could expose
  the same counters at `/api/v1/metrics` in Prometheus
  text format so they can be scraped alongside the rest
  of the server's metrics. The current JSON endpoint is
  sufficient for manual inspection and ad-hoc probing;
  Prometheus is a larger integration with its own
  exporter conventions.
- **Per-endpoint drop tracking.** The broadcaster only
  serves `/api/v1/logs/stream` today. If a future MCP
  SSE bus or pipeline-trace bus adopts the same
  broadcaster shape, the snapshot would need an
  endpoint-name dimension. Out of scope.
- **Time-windowed rates.** The current counters are
  cumulative since the subscription opened, not a
  rolling rate. A rate (drops/sec over the last minute)
  would need either an EWMA or a ring buffer of
  samples. Good fodder for a future slice.

### 2.47 Self-rotation /me/rotate endpoint + /account GUI  *(tenancy + GUI + Go SDK)*

| Item                       | Location                                                 |
| -------------------------- | -------------------------------------------------------- |
| `RotateMyAPIKey` handler   | `internal/server/tenancy_handlers.go` — reads Principal from context, calls `Store.RotateAPIKey(principal.KeyID)`, emits `api_key.rotated` audit with `self: true` detail |
| Route mount                | `internal/server/server.go` — `POST /api/v1/keys/me/rotate` behind `RequireRole(RoleViewer, ...)` so any authenticated role can self-rotate |
| GUI `rotateMyAPIKey`       | `gui/lib/api.ts` — `POST /api/v1/keys/me/rotate` helper |
| GUI `rotateSelf` action    | `gui/lib/auth.ts` — server action that calls the API, updates the `mockagents_api_key` cookie with the new plaintext atomically, returns a `{ok, plaintext, prefix}`/`{ok, error}` result shape |
| `/account` page            | `gui/app/account/page.tsx` — server component that shows current prefix/role, renders the once-only plaintext banner after rotation, and carries Rotate + Sign-out forms |
| Header auth pill           | `gui/app/layout.tsx` — prefix chip now links to `/account` so operators can discover self-rotation from every page |
| Go SDK helper              | `sdk/go/mockagents/client.go` `Client.RotateMyAPIKey(ctx)` — posts to `/api/v1/keys/me/rotate`, returns the JSON payload as a map |
| Tests                      | `internal/server/tenancy_handlers_test.go` — 2 new: `TestTenancyHandlers_RotateMyAPIKey` (full round-trip via injected `WithPrincipal` middleware; old plaintext dies, new one resolves to same key id + role), `TestTenancyHandlers_RotateMyAPIKey_Unauthenticated` (missing principal → 401) |
| Verification               | **Live**: `go test ./internal/server/...` runs **78 tests** (was 76 → 78, +2); `npx next build` clean with **16 routes** (was 15 — `/account` added); full `go test ./...` across 21 packages green |

Closes the **self-rotation `/me/rotate` endpoint** follow-up
explicitly deferred in §2.37. Operators no longer need an admin
credential to rotate their own key after a suspected compromise —
any authenticated role can hit the new endpoint (including
viewers) because the handler reads the caller's key id from the
request context instead of accepting it as a path parameter.

What landed:

- **`RotateMyAPIKey` handler.** New stateless method on
  `TenancyHandlers` that:
  1. Reads the `*tenancy.Principal` off the request context via
     `tenancy.PrincipalFrom(r.Context())`.
  2. Returns 401 if no principal is present — the auth
     middleware should have put one there in multi-tenant mode;
     missing it indicates either single-tenant mode (where
     self-rotation is meaningless) or a misconfigured proxy.
  3. Calls `Store.RotateAPIKey(principal.KeyID)` to do the
     actual atomic swap.
  4. Records an `api_key.rotated` audit event with a
     `self: true` detail so the audit log distinguishes
     admin-initiated rotations from self-rotations.
  5. Returns the full `NewAPIKeyResult` so the caller can
     capture the fresh plaintext exactly once.
- **`POST /api/v1/keys/me/rotate` route.** Mounted when
  multi-tenant mode is enabled. Gated behind
  `RequireRole(RoleViewer, ...)` — which really means "any
  authenticated caller" since viewer is the lowest role. There
  is intentionally no path parameter: the server never trusts
  a caller-supplied id for self-rotation; it always uses the
  key id that the auth middleware attached to the context.
  This makes privilege escalation through this endpoint
  impossible — a viewer can only touch its own credential.
- **Audit event with `self: true`.** The existing
  `EventAPIKeyRotated` event kind is reused, but with an extra
  `self` boolean in Details. Administrators can filter
  `?kind=api_key.rotated` on `/api/v1/audit` and sort between
  admin-driven rotations (operators rotating other users'
  compromised keys) and self-rotations (users responding to
  their own suspected exposure). No new audit kind means no
  schema migration.
- **GUI `rotateSelf` server action.** `gui/lib/auth.ts` gained
  a new server action that:
  1. Calls `rotateMyAPIKey` (which uses the current cookie to
     authenticate).
  2. On success, overwrites the HttpOnly `mockagents_api_key`
     cookie with the new plaintext so subsequent requests
     keep working — the old secret is already dead on the
     server side the moment the transaction commits.
  3. Returns `{ok: true, plaintext, prefix}` on success or
     `{ok: false, error}` on any transport/auth failure so
     the caller can render an inline banner.
- **`/account` page.** New server component at
  `gui/app/account/page.tsx` that shows the current session
  prefix + role, renders two forms (Rotate / Sign out), and
  displays the once-only plaintext banner after a successful
  rotation. Unauthenticated visits redirect to
  `/login?next=/account`. The page is the natural home for any
  future per-user preferences (e.g. "default page on sign-in",
  "show drop-badge warnings as modals") — today it's just
  rotation + sign-out, but the surface is ready.
- **Header auth pill becomes a link.** The existing
  "signed in as {prefix}" chip used to be a plain `<span>`;
  it's now a `<Link href="/account">` so operators discover
  the self-service page from every page in the app. Sign out
  still lives next to it.
- **Go SDK `Client.RotateMyAPIKey`.** New method on the
  existing `Client` struct. Thin wrapper over
  `c.do("POST", "/api/v1/keys/me/rotate", ...)` that returns
  the JSON payload as `map[string]any` — matches the existing
  `ReloadAgent` / `GetAgent` shape so SDK users don't learn a
  new return convention.

Design notes:

- **Why 401 (not 403) on missing Principal?** 403 implies the
  caller has a credential but it's insufficient for the
  route. The missing-principal case is "no credential at all"
  → 401 is correct. In practice the auth middleware rejects
  unauthenticated requests before they reach the handler, so
  this branch is only taken by tests and by a
  misconfiguration where someone mounted the route without
  wrapping it in `RequireRole`.
- **Why reuse `EventAPIKeyRotated` instead of a new kind?** A
  separate `EventAPIKeySelfRotated` would clutter the
  enum-of-kinds with no real benefit: the audit log query
  surface would need a new filter, the frontend would need a
  new badge color, etc. The `self: true` detail is cheaper
  and lets operators grep or SQL-filter on it if they really
  want to split the two cases.
- **Cookie overwrite inside the action, not the page.** The
  action is a server-side function that already has access
  to `cookies()` via `next/headers`; overwriting there means
  the next request (the redirect to `/account?plaintext=…`)
  carries the new cookie automatically. Doing the update on
  the page would race with the redirect — by the time the
  page reads `cookies()` the request is already past the
  `Set-Cookie` point. This is the standard "server-side
  atomic cookie refresh" pattern for Next.js 15.
- **Why put the plaintext in the query string?** The server
  action redirects to `/account?plaintext=…`; the query
  param lets the page render the banner without a separate
  server→client state handoff. The plaintext is already
  persisted in the HttpOnly cookie, so appearing once in the
  URL is not a meaningful leak (the URL is server-rendered
  only, never committed to browser history as an auth
  token). A paranoid deployment can swap this for a
  cookie-based "show once" flag — separate slice.
- **Go SDK returns `map[string]any`, not a typed struct.**
  The tenancy package is an internal dependency of the
  server, and pulling it into the public SDK would widen
  the import surface and leak implementation details. The
  existing SDK methods (`GetAgent`, `ReloadAgent`,
  `ListAgents`) already return maps or raw JSON for
  server-side shapes — this matches the convention.

Regression guards:

- **`TestTenancyHandlers_RotateMyAPIKey`** — full HTTP round-
  trip with a manually-injected Principal (via
  `tenancy.WithPrincipal` in an inline middleware):
  response returns fresh plaintext, key id unchanged, role
  unchanged, old plaintext stops resolving, new plaintext
  resolves to the same principal.
- **`TestTenancyHandlers_RotateMyAPIKey_Unauthenticated`** —
  no Principal on the context → 401 with a clear error
  message.

What stayed deferred:

- **Rotate-then-logout flow.** Some operators will want a
  "burn this session" button that rotates AND logs out in
  one step, invalidating their cookie entirely without
  exposing the new plaintext. Useful after a confirmed
  compromise of the current cookie. Small slice.
- **Per-action confirmation modal.** Self-rotation is an
  irreversible action — a confirmation dialog before the
  form submits would prevent accidental clicks. The current
  design trusts the operator (standard CLI / admin-tool UX).
- **Grace period / overlap rotation.** Still open from
  §2.37 — rotate with a 24h window where both secrets work.
  Needs a schema change (two hashes per row).
- **Bulk rotation.** Rotating every key in a tenant in one
  call after a suspected compromise is still §2.37's
  remaining deferred item.

### 2.46 Cross-document reference checking  *(config + CLI)*

| Item                        | Location                                                 |
| --------------------------- | -------------------------------------------------------- |
| Cross-doc validator         | `internal/config/cross_document_validator.go` `ValidateDocuments(*Documents)` |
| CLI second-pass wiring      | `cmd/mockagents/validate.go` runs `ValidateDocuments` after the per-document validators when pipelines or testsuites are present |
| Tests                       | `internal/config/cross_document_validator_test.go` — 9 cases: all valid, pipeline refs missing agent, testsuite agent target missing, testsuite pipeline target missing, assertion node_id missing in pipeline, node_id under agent-only target, empty docs no-op, nil no-op, unknown-pipeline suppresses node_id follow-ons |
| Verification                | **Live**: `go test ./internal/config/...` runs **127 tests** (was 118 → 127, +9); `go run ./cmd/mockagents validate examples/` still reports `Validated 11 file(s): all valid.` (the real examples wire their refs correctly); full `go test ./...` across 21 packages green |

Closes the **cross-document reference checking** follow-up
explicitly deferred in §§2.38, 2.43, and 2.45. The per-document
validators already catch everything you can check in one file at
a time; this slice adds the second pass that resolves references
across files in the same directory. Now a pipeline that points
at a non-existent agent, or a test suite whose target agent/
pipeline never loaded, is caught at `mockagents validate` time
instead of surfacing at runtime as a "not found" or silently
as a nil dereference.

What landed:

- **`ValidateDocuments(docs *Documents)`.** New entry point
  that takes a loaded `Documents` struct (the same one
  `config.LoadAllDocuments` returns) and runs four
  reference-resolution checks in one pass:
  1. Every Pipeline's `spec.agents[].ref` must name a loaded
     Agent. Per-node errors so operators can pinpoint the
     exact line in their YAML.
  2. Every TestSuite's `spec.target.agent` must name a
     loaded Agent.
  3. Every TestSuite's `spec.target.pipeline` must name a
     loaded Pipeline.
  4. Every TestSuite assertion with `node_id` set must
     target a pipeline (not an agent) AND the node_id must
     exist in that pipeline's `spec.agents[].id` list.
- **Error location on the referring document.** Errors are
  attached to the file path + yaml.Node of the document
  that holds the bad reference, not the missing target. So
  an operator sees `test-suite.yaml:10:5: test suite
  references unknown agent "support-agent"` — the exact
  line they need to fix — instead of a generic "unknown
  reference" with no anchor.
- **Noise suppression.** When a pipeline target is unknown,
  subsequent assertion `node_id` checks for that suite
  are skipped — there's nothing to look up, and piling
  "node_id X does not exist" on top of the root-cause
  "unknown pipeline" would just bury the actionable error.
  The `TestValidateDocuments_UnknownPipelineSuppressesNodeIDErrors`
  test pins this exact behavior: unknown pipeline + two
  assertions with node_id → exactly one error in the
  report.
- **Indexes built once per call.** The validator builds
  two maps up front — `agentNames` for quick agent
  membership checks and `pipelineNodeIDs` for per-pipeline
  node-id lookups — and reuses them across all documents
  in the collection. O(N + M) where N is the total number
  of documents and M is the total number of references;
  linear in the inputs.
- **CLI second-pass integration.** `cmd/mockagents/validate.go`
  now calls `ValidateDocuments` after the per-document
  validators finish, but only when the run loaded at
  least one pipeline or testsuite — pure agent
  directories (single-language projects, etc.) skip the
  pass entirely so the hot path isn't paying for work
  that can't produce errors.
- **Real examples still clean.** `mockagents validate
  examples/` reports `Validated 11 file(s): all valid.`
  after the cross-doc pass lands — proving that the real
  `research-pipeline.yaml` correctly references its
  `web-researcher` / `summary-writer` agents, and that
  `research-suite.yaml` correctly targets the
  `research-pipeline`. Everything resolves.

Design notes:

- **Why a separate validator instead of an extension of the
  per-document validators?** A per-document validator
  operates on a single yaml.Node tree and has no knowledge
  of what else has been loaded. Adding "check this agent
  name against the global set" to `ValidatePipeline` would
  require plumbing a `*Documents` parameter through every
  per-doc validator and would make unit testing each one in
  isolation awkward. A second pass that knows about every
  document is the cleanest split.
- **Why not in `ValidateBytes`?** `ValidateBytes` operates
  on a single YAML blob in memory — it has no context
  about other documents. The GUI editor at `/editor` uses
  `ValidateBytes` and intentionally cannot do cross-doc
  validation. A future slice could add a management API
  endpoint that takes multiple YAMLs and runs
  `ValidateDocuments` against them, but that's a separate
  feature that needs its own UX.
- **Index on `metadata.name`, not file path.** Pipelines
  and test suites reference agents by name (the
  `metadata.name` field), not by filename. The index uses
  names because that's what the refs use — so a
  misnamed file doesn't cause spurious "unknown agent"
  errors as long as `metadata.name` is correct.
- **Skip entries with empty refs.** When a Pipeline node
  has an empty `ref`, the per-document `ValidatePipeline`
  pass already flagged it. Re-flagging here would create
  duplicate errors for the same problem. The cross-doc
  pass silently skips those so the report stays focused.
- **Skip the pass when no pipelines or testsuites loaded.**
  Pure agent directories don't need cross-doc checks —
  every reference is trivially absent. The CLI skips the
  call entirely in that case so operators don't pay for
  work they don't need.
- **Test infrastructure uses plain struct literals.** The
  tests don't decode YAML — they build `LoadResult` /
  `PipelineLoadResult` / `TestSuiteLoadResult` structs
  directly with `newAgent` / `newPipeline` / `newSuite`
  helpers. This is fine because the cross-doc validator
  doesn't use line numbers for its errors (the reference
  IS the problem, not a specific line); the per-doc
  validators already cover line-number placement.

Regression guards:

- **`TestValidateDocuments_AllValid`** — happy path: two
  agents, a pipeline that references both, a suite that
  targets the pipeline with a valid node_id. Zero errors.
- **`TestValidateDocuments_PipelineRefsMissingAgent`** —
  a pipeline node refs an agent that doesn't exist;
  exactly one error on the right field.
- **`TestValidateDocuments_TestSuiteAgentTargetMissing`**
  — suite targets an unknown agent.
- **`TestValidateDocuments_TestSuitePipelineTargetMissing`**
  — suite targets an unknown pipeline.
- **`TestValidateDocuments_AssertionNodeIDMissingInPipeline`**
  — node_id references a node not in the target pipeline.
- **`TestValidateDocuments_AssertionNodeIDWithoutPipelineTarget`**
  — suite targets an agent but an assertion uses
  node_id; flagged as semantically incompatible.
- **`TestValidateDocuments_NoTestSuitesOrPipelinesIsANoOp`**
  — pure agent directory, zero errors.
- **`TestValidateDocuments_NilIsANoOp`** — nil Documents
  pointer, zero errors, no panic.
- **`TestValidateDocuments_UnknownPipelineSuppressesNodeIDErrors`**
  — unknown pipeline + two assertions with node_id →
  exactly one error (the unknown pipeline), no
  piled-on node_id errors.

What stayed deferred:

- **MCP server reference checking.** MCPServer documents
  don't reference other documents today, so there's
  nothing to cross-check. If a future slice adds an
  `mcp_ref` field or similar (e.g. an agent that uses a
  named MCPServer for tool resolution) the same pattern
  would apply.
- **Agent-to-agent references.** The engine's scenario
  matcher can include another agent's id in a
  cross-reference (rare but supported); the per-agent
  validator already covers this in `validateCrossReferences`,
  so the cross-doc pass doesn't duplicate it.
- **Runtime resolver consistency.** The pipeline engine's
  runtime agent resolver uses the exact same
  metadata.name index, so anything the cross-doc
  validator accepts will work at runtime, and anything
  it rejects would fail at runtime anyway. Keeping the
  two indexes in lockstep is maintained by convention
  (both look up via the same registry).

### 2.45 Pipeline edge polish — when_contains + duplicate edges  *(config)*

| Item                        | Location                                                 |
| --------------------------- | -------------------------------------------------------- |
| Whitespace `when_contains`  | `internal/config/pipeline_validator.go` — flags guards that are present but trimmed-empty (e.g. `"   "`) as almost-certainly-a-typo |
| Duplicate edge detection    | `internal/config/pipeline_validator.go` — tracks `(from, to, when_contains)` triples; second occurrence produces a back-ref to the first |
| Tests                       | `internal/config/pipeline_validator_test.go` — 5 new cases: whitespace-only guard, empty-tolerated (unconditional edge), duplicate unconditional, duplicate guarded, distinct guards valid |
| Verification                | **Live**: `go test ./internal/config/...` runs **118 tests** (was 113 → 118, +5); `go run ./cmd/mockagents validate examples/` still reports `Validated 11 file(s): all valid.`; full `go test ./...` across 21 packages green |

Closes the **edge `when_contains` substring validation**
follow-up explicitly deferred in §2.38 (and carried forward in
§2.43). Two edge-shape traps that used to slip through load-time
validation are now caught:

What landed:

- **Whitespace-only `when_contains` is flagged.** The rule
  distinguishes three cases: (1) field absent or `""` — a
  legal unconditional edge, no error; (2) field present and
  contains non-whitespace characters — a legal guarded edge,
  no error; (3) field present but only whitespace (e.g. `"  "`
  or `"\t"`) — flagged. The third case looks like a filter
  but matches everything the executor sees, which is almost
  always a typo: an operator meant to type `"foo"` but left
  the quotes empty.
- **Duplicate edge detection.** The validator now tracks a
  `(from, to, when_contains)` triple set as it walks the edge
  list. Second and later occurrences of the same triple
  produce a "duplicate edge X → Y (first seen at
  spec.edges[N])" error. When the guard is non-empty the
  message includes the guard for clarity: "duplicate edge
  X → Y guarded by \"foo\"". The suggestion explicitly
  clarifies: identical guards are redundant, but **different**
  guards on the same (from, to) pair are legal (parallel
  guarded paths) and are not flagged — the
  `TestValidatePipeline_DistinctGuardsAreValid` test pins
  that distinction.
- **Endpoint validation runs first.** The duplicate check
  skips edges whose endpoints are invalid (empty or unknown)
  so the dedup report stays clean — the earlier
  `.from`/`.to` errors already cover those cases and
  surfacing duplicate-edge noise on top would just clutter
  the report.

Design notes:

- **Why not just drop duplicates silently?** The pipeline
  executor processes every declared edge in order. Two
  identical edges means the downstream node gets fed twice,
  which at best duplicates work and at worst confuses the
  consumer. Surfacing it as an error forces the operator to
  choose: remove the duplicate or distinguish the guards.
- **Why include the guard in the error message?** Operators
  hit this when they accidentally paste an edge block twice
  after tweaking one field — showing the specific guard
  makes "oh right, those are both for `foo`" obvious
  immediately, instead of requiring a second read of the
  YAML.
- **Why error, not warn?** The validator's
  `ValidationError` type has no severity level — every entry
  is an error. Both of these rules are high-confidence "this
  is definitely a bug" signals: an empty-looking guard and a
  redundant edge. A warning tier could arrive in a future
  slice if other rules surface lower-confidence heuristics.
- **Track `edgeKey` as a local struct, not a composite
  string.** A `struct{from, to, when string}` keys a map
  with zero-allocation hashing and avoids the risk of
  collision that would come from joining strings with a
  separator that might legally appear inside a node id or a
  guard substring.

Regression guards:

- **`TestValidatePipeline_WhitespaceOnlyWhenContains`** —
  `when_contains: "   "` produces the whitespace-only error.
- **`TestValidatePipeline_EmptyWhenContainsTolerated`** — an
  edge with no `when_contains` field at all is a valid
  unconditional edge and produces zero errors.
- **`TestValidatePipeline_DuplicateUnconditionalEdge`** — two
  `a → b` edges with no guards produce a duplicate-edge
  error.
- **`TestValidatePipeline_DuplicateGuardedEdge`** — two
  `a → b` edges both guarded by `foo` produce a "guarded by
  \"foo\"" error (message includes the guard).
- **`TestValidatePipeline_DistinctGuardsAreValid`** — two
  `a → b` edges guarded by `foo` and `bar` are legal and
  produce zero errors — pins the "parallel guarded paths"
  use case.

What stayed deferred:

- **Cross-edge heuristic warnings.** E.g. "you have an edge
  `a → b` guarded by `foo` and an edge `a → b` guarded by
  `food`" — the second subsumes the first semantically.
  Detecting that needs substring-containment analysis and
  is out of scope.
- **Cross-document reference checking.** Still the next
  slice candidate — this one is purely intra-document.

### 2.44 SSE drop-count signal for slow subscribers  *(server + GUI)*

| Item                          | Location                                                 |
| ----------------------------- | -------------------------------------------------------- |
| `LogSubscription` struct      | `internal/server/log_broadcaster.go` — exposes `C()`, `Dropped() uint64` (atomic), replaces the raw channel return from `Subscribe` |
| `Publish` drop counter        | `internal/server/log_broadcaster.go` — `atomic.Uint64.Add(1)` on buffer-full path |
| `event: dropped` SSE frame    | `internal/server/log_handlers.go` `StreamLogs` — top-of-loop check against `lastDropped`, emits `event: dropped\ndata: {"count":N,"new":M}\n\n` whenever the subscription's counter has advanced |
| GUI dropped-event listener    | `gui/app/logs/AutoRefreshLogs.tsx` — `es.addEventListener("dropped", ...)`, sticky badge in the live bar |
| Drop badge style              | `gui/app/globals.css` `.drop-badge` (red-on-pink pill with warning glyph) |
| Broadcaster test update       | `internal/server/log_broadcaster_test.go` — `SlowSubscriberDrops` now asserts `sub.Dropped() == 4` |
| End-to-end test               | `internal/server/log_broadcaster_test.go` `TestStreamLogsEmitsDroppedFrame` — floods a backpressured subscriber, asserts the `event: dropped` frame arrives with positive `count` and `new` |
| Verification                  | **Live**: `go test ./internal/server/...` runs **76 tests** (was 75 → 76, +1); `npx next build` clean with 15 routes unchanged (`/logs` grew 1.27 kB → 1.4 kB for the new event listener) |

Closes the **drop-count header for slow SSE subscribers**
follow-up explicitly deferred in §2.34. Before this slice the
LogBroadcaster silently swallowed events when a subscriber's
channel was full — operators whose browser tab fell behind had
no way to know their view was incomplete. The v0.3 live feed
now surfaces backpressure as a dedicated SSE event the client
can listen for.

What landed:

- **`LogSubscription` struct.** The broadcaster's `Subscribe`
  now returns a `*LogSubscription` pointer instead of a raw
  receive-only channel. The struct exposes `C() <-chan
  *storage.InteractionLog` for the existing range-over-channel
  pattern and `Dropped() uint64` for the new counter read.
  The drop counter is `atomic.Uint64` so reads on the handler
  side don't contend with writes on the publisher's hot path.
- **Atomic drop counter.** `Publish` increments
  `sub.dropped.Add(1)` when the per-subscriber channel is
  full. The counter is cumulative and monotonically
  non-decreasing — the stream handler computes per-frame
  deltas from its own `lastDropped` high-water mark.
- **`event: dropped` SSE frame.** `StreamLogs` now checks
  `sub.Dropped()` against a local `lastDropped` at the top of
  every main-loop iteration — before the ticker/data/ctx
  select — so a drop that lands between a log write and the
  next heartbeat gets surfaced the moment the handler wakes.
  When the counter has advanced, the handler writes a
  standard SSE frame with a JSON payload of shape
  `{"count":<cumulative>,"new":<delta>}`, flushes, and bumps
  its own watermark.
- **No stealing the heartbeat.** The dropped check runs at the
  top of the loop and then falls through to the normal
  select. A heartbeat tick still writes `:heartbeat\n\n` right
  after the drop frame (if any), so the client never loses the
  keepalive signal — the two signals are additive, not
  exclusive.
- **GUI dropped event listener.** `AutoRefreshLogs.tsx` adds
  `es.addEventListener("dropped", ...)` that parses the JSON
  payload and updates a `droppedCount` state hook. The live
  bar renders a sticky badge `⚠ N dropped` whenever the
  counter is non-zero. The badge is sticky — once it lights
  up, operators see it until they reload the page — because
  "this tab is falling behind" is an actionable signal that
  should not auto-dismiss.
- **`.drop-badge` CSS.** A red-on-pink pill matching the
  existing error-banner palette, sized to fit next to the
  live pill without pushing the layout.
- **Broadcaster test refactor.** The existing
  `TestLogBroadcaster_SlowSubscriberDrops` test now asserts
  the exact drop count (4 — first publish fits, next 4
  overflow) instead of just "publisher didn't block".
- **End-to-end drop-frame test.** `TestStreamLogsEmitsDroppedFrame`
  wires a fresh broadcaster into a `LogHandlers` with a
  30 ms heartbeat, floods 500 publishes past the default
  64-slot subscriber buffer, and reads the HTTP body until it
  finds an `event: dropped` frame with positive `count` and
  `new` counts. Proves the whole chain — broadcaster atomic,
  handler poll, SSE frame encoding, client readability — is
  wired end-to-end.

Design notes:

- **Why an SSE event instead of an HTTP trailer?** HTTP
  trailers work for buffered responses but are unusual for
  SSE (the response body is long-lived, and middleboxes
  strip trailers aggressively). A dedicated `event: dropped`
  frame is in-protocol, browser-consumable via
  `addEventListener`, and doesn't require any special client
  handling — the EventSource API already ignores comment
  lines and routes custom events via their `event:` name.
- **Why top-of-loop, not post-write?** Checking the counter
  before the select gives us the quickest possible reporting
  path: a drop that lands 1 ns before the heartbeat tick is
  surfaced in the same iteration, not the next one. The
  alternative (emit after each log write) would starve
  clients that aren't receiving data at all, which is
  precisely the case where drops happen.
- **Why cumulative `count` + delta `new`?** Operators who
  connect mid-session want to know the absolute number of
  drops on their subscription so far; operators who watch
  the live bar want to know whether drops are still
  happening. Both are cheap to emit and the client picks
  whichever it wants — the GUI uses `count` for the badge
  (sticky total), tests use `new` (the delta was positive →
  drops just happened).
- **Atomic uint64, not a mutex-guarded int.** The publisher
  runs on every incoming interaction log — hot path.
  `atomic.Uint64.Add` is single-instruction on amd64/arm64
  and avoids any cache-line contention with the subscriber
  count mutex. The handler's `Dropped()` read is
  `atomic.Uint64.Load`, also single-instruction.
- **Subscription as a pointer, not a value.** The struct
  holds an `atomic.Uint64`, which is not safe to copy.
  Returning `*LogSubscription` (instead of `LogSubscription`)
  makes the no-copy contract explicit and matches Go's
  convention for types containing atomics.
- **Sticky badge.** The GUI does not reset the counter on
  reconnect — a reconnect with a fresh subscription starts
  from zero on the server side, but the browser keeps the
  last known count until a page reload. That's the right
  trade-off: operators who recover from a brief dropout
  still need to know that *at some point* during the session
  they missed events.

Regression guards:

- **`TestLogBroadcaster_SlowSubscriberDrops`** — buffer-1
  subscription sees exactly 4 drops after 5 publishes.
- **`TestStreamLogsEmitsDroppedFrame`** — 30 ms heartbeat +
  500-event flood → `event: dropped` frame on the wire with
  positive counts.

What stayed deferred:

- **Reset counter on explicit client ack.** A future slice
  could add a client-side "ack dropped" POST endpoint that
  zeros the counter so the badge can dismiss itself. Out of
  scope — operators who recovered from a dropout can reload.
- **Per-tab drop-rate metric.** The broadcaster tracks
  cumulative drops, not a rate. A rate (drops/sec) would
  need a rolling window or an EMA — unnecessary for the
  "is my tab falling behind?" signal.
- **Aggregate drop count across all subscribers.** Useful
  for server-side metrics but not for a specific tab's
  decision-making. Can land in an unrelated
  `/api/v1/stream/metrics` endpoint if someone wants it.

### 2.43 TestSuite + MCPServer rule-based validators  *(config + CLI)*

| Item                          | Location                                                 |
| ----------------------------- | -------------------------------------------------------- |
| TestSuite validator           | `internal/config/testsuite_validator.go` `ValidateTestSuite` — apiVersion, kind, metadata.name, target exactly-one-of, non-empty cases, unique case names, step role+content, assertion dispatch |
| MCPServer validator           | `internal/config/mcpserver_validator.go` `ValidateMCPServer` — apiVersion, kind, metadata.name, non-empty spec, unique tool/resource/prompt names, tool-response match-or-default, content block type + field guard |
| Shared `metadata.name` helper | `internal/config/testsuite_validator.go` `validateMetadataName` (used by both new validators) |
| ValidateBytes wiring          | `internal/config/validate_bytes.go` TestSuite + MCPServer branches now run the new validators instead of parse-only |
| CLI multi-kind coverage       | `cmd/mockagents/validate.go` collects `docs.TestSuites` + `docs.MCPServers` buckets and runs the matching validator against each; single-file mode falls back through `LoadTestSuiteFile` / `LoadMCPServerFile` before erroring |
| Tests                         | `internal/config/testsuite_validator_test.go` (9: valid, both targets, no target, no cases, duplicate case, no steps, missing role, assertion dispatch for every type, invalid kind), `internal/config/mcpserver_validator_test.go` (13: valid, empty server, duplicate tool, no responses, neither-match-nor-default, both-match-and-default, multiple defaults, empty text, unknown content type, resource missing URI, duplicate resource URI, duplicate prompt name, image missing fields) |
| Verification                  | **Live**: `go test ./internal/config/...` runs **113 tests** (was 91 → 113, +22); `go run ./cmd/mockagents validate examples/` reports `Validated 11 file(s): all valid.` (was 9 — now includes `research-suite.yaml` and `weather-mcp.yaml`); full `go test ./...` across 21 packages green |

Closes the **TestSuite / MCPServer rule-based validators** row
explicitly deferred in §§2.38 and 2.42. Every `kind:` that
MockAgents supports — Agent, Pipeline, TestSuite, MCPServer — now
flows through the same rule-based validator plumbing. Before this
slice the CLI only surfaced rule errors for agents and pipelines;
TestSuite and MCPServer documents were parse-only and silently
accepted any structural shape yaml.Decode could coerce.

What landed:

- **`ValidateTestSuite(def, path, node)`.** Seven rule groups:
  - `apiVersion`/`kind` must match `mockagents/v1` / `TestSuite`.
  - `metadata.name` is kebab-case, ≤63 chars.
  - `spec.target` must set exactly one of `agent` or `pipeline`.
  - `spec.cases` must be non-empty.
  - Every case needs a unique name and at least one step.
  - Every step needs a `role` and `content`.
  - Every assertion is dispatched through
    `validateTestAssertion`: `tool_call` needs `tool`,
    `response_contains`/`scenario_matched` need `value`,
    `latency_ms_lt` needs `max_ms > 0`, and anything else is
    flagged as an unknown type.
- **`ValidateMCPServer(def, path, node)`.** Eight rule groups:
  - Same `apiVersion`/`kind`/`metadata.name` trio as above.
  - `spec` must expose at least one of tools/resources/prompts —
    an MCP server that exposes nothing is almost always a typo.
  - Tool names unique; every tool has ≥1 response; every
    response has exactly one of `match:` or `default: true`;
    at most one default per tool.
  - Content blocks dispatch by type: `text` needs `text`,
    `image` needs `data` + `mimeType`, `resource` needs `uri`,
    anything else is an unknown type.
  - Resource URIs required and unique.
  - Prompt names unique; prompt arguments have names; prompt
    messages have roles; message content blocks run through
    the same content-block rule set as tool responses.
- **Shared `metadata.name` helper.** The Pipeline/TestSuite/
  MCPServer validators all use the same kebab-case rule, so the
  check is hoisted into a package-level
  `validateMetadataName(ctx, name, field, suggestionSuffix)`
  helper to avoid copy-paste. The Agent validator keeps its
  existing inline copy — zero-churn on the original surface.
- **`ValidateBytes` now runs both validators.** Before this
  slice, the TestSuite and MCPServer branches in `ValidateBytes`
  stopped after the typed decode. They now call the new
  validators and append any errors to the report. The GUI
  `/editor` page automatically picks up the new rules — no
  client changes needed.
- **CLI covers every bucket.** `cmd/mockagents/validate.go`
  previously collected only `docs.Agents` and `docs.Pipelines`
  from `config.LoadAllDocuments`. It now also collects
  `docs.TestSuites` and `docs.MCPServers` and runs the matching
  validator against each. Single-file mode's loader fallback
  chain gained two more tries
  (`LoadTestSuiteFile`, `LoadMCPServerFile`) so
  `mockagents validate my-suite.yaml` correctly dispatches on
  kind instead of hard-erroring with "not an agent".
- **Examples validated end-to-end.** `mockagents validate
  examples/` now reports **11 files valid** (was 9) — the two
  real-world examples `research-suite.yaml` (a TestSuite) and
  `weather-mcp.yaml` (an MCPServer) now flow through the new
  validators and come out clean. Proves the rule set matches
  the shape of documents operators actually write.

Design notes:

- **Both documents share the `validationContext` plumbing.**
  The existing `validationContext` + `addError` + `LineColOf`
  infrastructure from the agent validator is reused verbatim,
  so every new error carries the same file/line/column/
  suggestion shape the CLI's `FormatErrors` function already
  knows how to render. No changes to the reporter, no changes
  to the ErrorFormat text/JSON surface.
- **`ValidateMCPServer` accumulates errors into `ctx` then
  wraps at the end.** The function originally returned nil
  (missing return — caught by the compiler and fixed in
  place). The fix turns it into the standard "run every rule
  pass, then `if len(ctx.errors) == 0 return nil` else wrap"
  pattern that matches every other validator in the package.
- **Empty MCP server is an error, not a warning.** A server
  with no tools/resources/prompts technically still handles
  `initialize` and `ping`. Surfacing it as an error at load
  time prevents the "I shipped an empty server and my tests
  see nothing" bug class. Operators who legitimately want
  capabilities-only can add a single placeholder tool or
  resource — cheap and self-explanatory.
- **Tool response `match` XOR `default`.** A response with
  neither can never fire (the match logic would return the
  first match or the first `default: true`, and this entry
  would be skipped on both paths). A response with both is
  ambiguous — does `default: true` override a non-matching
  `match:` or does `match:` take precedence? The mock resolver
  never intended to support that combination, so we flag it at
  load time. At most one default per tool is the same
  invariant the resolver relies on at runtime.
- **Content block type dispatch.** The MCP content block is a
  union type (`text` | `image` | `resource`) squashed into a
  single struct with optional fields per variant. Validating
  per-variant at load time catches the common "I set
  `type: text` but forgot `text:`" typo and prevents it from
  showing up as a confusing empty response at runtime.
- **Per-kind single-file loader fallback chain.** The CLI's
  single-file mode used to error with the Agent loader's
  message when given a non-agent document. The new chain
  tries each typed loader in turn and only surfaces the
  original error if none match. Side effect: the error
  message for "this file is malformed regardless of kind" is
  now the Agent loader's message, which is still the most
  common case. Acceptable.

Regression guards:

- **`ValidateTestSuite` — 9 tests:** valid, both targets,
  no target, no cases, duplicate case name, case without
  steps, step missing role, assertion rule dispatch (5 types
  in one case), invalid kind.
- **`ValidateMCPServer` — 13 tests:** valid, empty server,
  duplicate tool name, tool without responses, neither
  match nor default, both match and default, multiple
  defaults, content block missing text, unknown content
  type, resource missing URI, duplicate resource URI,
  duplicate prompt name, image block missing fields.
- **Examples validated end-to-end.** `mockagents validate
  examples/` is a live smoke test for the new plumbing —
  the two real-world files (`research-suite.yaml`,
  `weather-mcp.yaml`) validate cleanly.

What stayed deferred:

- **Edge `when_contains` substring validation in pipelines.**
  Still accepts any string; warn-on-empty would be a small
  polish slice.
- **Cross-document reference checking.** Today the validator
  can't say "the agent `support-agent` referenced by
  research-suite.yaml's target isn't loaded". That requires
  loading every document together and running a cross-pass,
  which belongs in a future slice — maybe `mockagents
  validate --cross` or similar.
- **Chaos / streaming / tool JSON schema checks for agents.**
  The existing agent validator already covers these; no new
  gaps surfaced.

### 2.42 Pipeline graph checks — cycles + unreachable nodes  *(config)*

| Item                      | Location                                                 |
| ------------------------- | -------------------------------------------------------- |
| Graph validator pass      | `internal/config/pipeline_validator.go` `validatePipelineGraph` — graph-topology-only, runs after the existing spec-level rules |
| Cycle detection           | `internal/config/pipeline_validator.go` `detectPipelineCycle` (3-color DFS: white/gray/black, gray→gray = back edge = cycle) |
| Reachability check        | `internal/config/pipeline_validator.go` BFS from every in-degree-zero source; unreachable nodes produce a per-node error |
| ValidatePipeline wiring   | `internal/config/pipeline_validator.go` `ValidatePipeline` calls `validatePipelineGraph` as the fourth pass (after apiVersion/kind/metadata/spec) |
| Tests                     | `internal/config/pipeline_validator_test.go` (7 new: diamond valid, 2-node cycle, 3-node cycle, island trap, disconnected chains valid, fan-out valid, non-graph topology skip) |
| Verification              | **Live**: `go test ./internal/config/...` runs **91 tests** (was 84 → 91, +7); `go run ./cmd/mockagents validate examples/` still reports `all valid`; full `go test ./...` across 21 packages green |

Closes the **Cycle + orphan detection** follow-up explicitly
deferred in §2.38. Before this slice, a graph pipeline with a
cycle or an unreachable node would pass `mockagents validate` /
the GUI editor and only surface at runtime inside the executor —
which silently handles both cases (cycles via a visited-set guard,
unreachable nodes by never firing them). The validator now
catches both at load time with line-number-aware errors so
operators fix them in their editor instead of debugging at runtime.

What landed:

- **`validatePipelineGraph(ctx, def)`.** New pass in the pipeline
  validator that runs as the fourth and final validation step.
  No-op unless `topology == graph` and at least one agent is
  declared, so `sequential` and `parallel` pipelines are
  completely unaffected. Builds forward adjacency + in-degree
  maps, deliberately skipping any edge whose `from`/`to`
  references an unknown node or is a self-loop — the earlier
  spec pass already flagged those, and including them here
  would pollute both the cycle and reachability results with
  phantom arcs.
- **3-color DFS cycle detection.** `detectPipelineCycle` marks
  each node `white` (unvisited) → `gray` (on current recursion
  stack) → `black` (finished). Any `gray → gray` edge during the
  traversal is a back edge, which is a directed cycle. Iteration
  order matches YAML declaration order so cycle reports are
  deterministic for a given document — helps snapshot tests and
  diff-based code review. Exits on the first cycle found rather
  than exhaustively enumerating every cycle: a single
  well-located error is more actionable than drowning the
  operator in a list.
- **BFS reachability from sources.** After cycle detection, if
  no cycle was found, the pass runs a BFS from every in-degree-
  zero node (the set of "sources"). Any node not visited is
  unreachable and produces a per-node error with an indexed
  field path (`spec.agents[N].id`) so the GUI editor can point
  at the exact YAML line. A cycle-free graph with edges always
  has at least one source (topological sort proves this), and a
  graph with no edges has every node as a source — so the
  reachability loop handles both degenerate cases without
  extra branches.
- **Short-circuit on cycles.** If a cycle was found, the
  reachability check is skipped entirely. Rationale: the set of
  "sources" becomes unreliable in a graph with cycles (nodes in
  the cycle have non-zero in-degree from each other), so the
  reachability report would just pile noise on top of the cycle
  error. The user needs to fix the cycle first, then re-run.
- **Unknown-node edges excluded from the adjacency.** Edges
  pointing at nodes that don't exist in `spec.agents` are
  already flagged by the earlier spec pass. The graph builder
  skips them so the cycle and reachability checks operate on a
  clean subgraph of real nodes — prevents "phantom cycle" false
  positives caused by typo'd node ids.

Design notes:

- **Why not report every node in a cycle?** A cycle has N nodes
  and we have enough line-number context to pin each one, but
  all N errors say "this node is in a cycle" and the user only
  needs to read one to diagnose. Listing them all would bury
  other errors in the validator report. The current
  implementation stops at the first back edge and surfaces a
  single clear error: "graph contains a cycle involving node
  X" with a suggestion to remove the back edge.
- **Recursive DFS, not iterative.** Pipeline YAML files are
  small — even a hand-authored graph with a hundred nodes
  fits in the default Go stack. An iterative DFS using an
  explicit stack would be more defensive but adds complexity
  for no measurable benefit. If someone ships a pipeline with
  thousands of nodes we can revisit.
- **No warn/error distinction.** The validator's
  `ValidationError` type doesn't have severity levels — every
  entry is an error. Cycles and unreachable nodes are both
  treated as errors (`mockagents validate` will exit 1).
  Operators who want to allow cycles for a specific test can
  reorganize their pipeline to avoid them; there is no
  "allow-cycle" escape hatch because the executor's visited-set
  guard is a bug trap, not a feature.
- **Reachability uses `append([]string(nil), sources...)`**.
  This is the idiomatic Go way to copy a slice so the BFS
  queue mutations don't alias the sources list. Cheap enough
  that it doesn't need a pool.

Regression guards:

- **`TestValidatePipeline_GraphDiamondIsValid`** — classic
  diamond (a→b, a→c, b→d, c→d); no cycle, all reachable, zero
  errors.
- **`TestValidatePipeline_GraphTwoNodeCycle`** — a→b, b→a
  produces a "cycle involving" error.
- **`TestValidatePipeline_GraphThreeNodeCycle`** — a→b→c→a
  produces the same shape.
- **`TestValidatePipeline_GraphUnreachableNode`** — exercises
  the cycle-short-circuit path: a graph with an explicit cycle
  (c↔d) and a separate chain (a→b) produces a cycle error and
  no unreachable error.
- **`TestValidatePipeline_GraphIsolatedChain`** — two
  disconnected chains (a→b, c→d) where both a and c are
  in-degree-zero; every node is reachable from a source. Zero
  errors.
- **`TestValidatePipeline_GraphUnreachableSink`** — fan-out
  (root → left, root → right); all three reachable. Zero
  errors. Included to pin the happy counterpart of the
  unreachable check.
- **`TestValidatePipeline_GraphChecksSkipNonGraphTopology`** —
  a sequential pipeline with a→b, b→a edges (which would form a
  cycle if it were a graph) produces the existing "edges only
  under graph" error but NOT a cycle error. Ensures the graph
  pass is a strict no-op under non-graph topology.

What stayed deferred:

- **Genuine unreachability without cycles.** In a cycle-free
  graph, every node has a predecessor chain that eventually
  hits an in-degree-zero source, so "unreachable without a
  cycle" is structurally impossible in a purely-forward DAG.
  The reachability check exists to catch the cycle short-
  circuit edge cases and as a safety net if a future validator
  change introduces a new failure mode.
- **Edge-case reporting for disconnected subgraphs.** Two
  disconnected chains are currently both "valid" because each
  has its own source. A future slice could warn that the
  pipeline has multiple independent components — sometimes
  intentional (parallel branches that don't need to meet),
  sometimes a typo. Out of scope for now.
- **TestSuite / MCPServer validators.** Still parse-only. Same
  plumbing (`validationContext` + rule functions) will apply
  when someone hits a surprising decode-time failure. Next
  session candidate.

### 2.41 Go SDK MCP bidirectional helper  *(SDK — Go)*

| Item                      | Location                                                 |
| ------------------------- | -------------------------------------------------------- |
| `McpClient` facade        | `sdk/go/mockagents/mcp.go` `NewMcpClient`, `Connect`, `SendResponse`, `DispatchRequest` |
| `McpEventStream` iterator | `sdk/go/mockagents/mcp.go` (scanner-style `Next`/`Value`/`Err`/`Close`; reuses `parseSSEFrame` + `splitSSEFrames` from `streaming.go`) |
| `McpEvent` + helpers      | `sdk/go/mockagents/mcp.go` `IsRequest`, `IsNotification`, `Params` |
| JSON-RPC types            | `sdk/go/mockagents/mcp.go` `JSONRPCEnvelope`, `JSONRPCError`, `McpRequestHandler` |
| Tests                     | `sdk/go/mockagents/mcp_test.go` (11: request parse, heartbeat+malformed skip, send-response result/error/exactly-one-of, dispatch happy path, unknown method, handler error, non-request reject, IsRequest requires id, Params always returns map) |
| Verification              | **Live**: `go test ./sdk/go/mockagents/...` runs **44 tests** (was 33 → 44, +11); full `go test ./...` across 21 packages green |

Closes the **Go SDK equivalent** follow-up explicitly deferred in
§§2.39 and 2.40. All three language SDKs now ship the same MCP
bidirectional surface: `McpClient` with `Connect` / `SendResponse`
/ `DispatchRequest`, a scanner-or-iterator-style `McpEventStream`,
and typed `McpEvent` accessors (`IsRequest`, `IsNotification`,
`Params`). Example code translates one-for-one across Python,
TypeScript, and Go — the only language-idiomatic difference is
scanner-style iteration in Go (`for stream.Next() { ev := stream.Value() ... }`)
versus generator-style in Python (`for event in stream`) and
async-iterator in TypeScript (`for await (const event of stream)`).

What landed:

- **`NewMcpClient(McpClientOptions)`.** Stateless constructor that
  owns an `*http.Client`. Nil or empty options defaults to
  `http://localhost:8080` with the stdlib default HTTP client
  (no timeout, so long-lived SSE subscriptions work correctly).
  Callers that want a custom client pass it via
  `McpClientOptions.HTTPClient`.
- **`Connect(ctx) (*McpEventStream, error)`.** Opens
  `GET /mcp/events` and returns a scanner-style iterator. The
  passed context bounds the entire subscription — cancelling it
  aborts the request and the next `Next` call returns false with
  `ctx.Err()`. The client deliberately clones the caller's
  `*http.Client` with `Timeout: 0` for the SSE fetch so users who
  set a timeout on their shared client don't accidentally break
  long-lived subscriptions. Matches the `requestSSE` pattern from
  §2.31.
- **`SendResponse(ctx, id, result, error) error`.** POSTs a
  JSON-RPC reply. Exactly one of `result` (a map) or `error` (a
  `*JSONRPCError`) must be non-nil; passing both or neither
  returns a validation error without touching the network. The
  server routes the reply by id — an unknown id comes back as
  404 wrapped in `*HTTPError`. The id is `json.RawMessage` so
  numeric, string, and null ids round-trip faithfully without
  the caller needing to know which form the server chose.
- **`DispatchRequest(ctx, event, handlers) (map[string]any, error)`.**
  The glue most test harnesses will use. Takes a
  `map[string]McpRequestHandler`; the handler gets the parsed
  params map and returns a result map. Unknown method ⇒ client
  posts `-32601` and returns a non-nil error. Handler returns
  an error ⇒ client posts `-32603` carrying the error's `.Error()`
  string and the *original* error is returned so the caller
  still sees the cause. Non-request events (notifications) are
  rejected up-front with a clear error.
- **`McpEventStream.Next / Value / Err / Close`.** Idiomatic Go
  scanner surface that matches `RawEventStream` from §2.31. The
  iterator drops heartbeat comments and malformed JSON frames
  silently so it only surfaces valid events. `Close` is
  idempotent and releases the HTTP body + cancels the request
  context, so `defer stream.Close()` is safe even after an
  explicit close.
- **`McpEvent` accessors.** `IsRequest` is true only when the
  payload has a non-empty non-null id (defensive — matches the
  Python and TS helpers). `IsNotification` is the inverse.
  `Params` always returns a non-nil map so callers can do
  `params["x"]` without a nil-check.
- **Helper reuse.** `parseSSEFrame` and `splitSSEFrames` from
  `streaming.go` (§2.31) are reused directly — the MCP helper
  adds exactly the JSON-decode step on top of the existing
  frame parser. Keeps the two clients on a single battle-tested
  SSE code path.

Design notes:

- **Scanner iterator over channels.** Go SDKs have two natural
  idioms for iterating a stream: a channel-based receiver (`for
  ev := range ch`) or a scanner struct (`for s.Next() { ... }`).
  The existing `RawEventStream` in §2.31 picked scanner because
  errors are cleaner (`stream.Err()` post-loop vs. a separate
  error channel) and closing the iterator doesn't race with the
  producer goroutine. The MCP helper follows the same pattern
  for consistency — a codebase with both styles would be
  confusing.
- **Zero new dependencies.** `net/http`, `encoding/json`,
  `bufio`, `bytes`, `context`, `io` — everything stdlib. The
  slice honored the project convention (`CLAUDE.md`) of not
  adding deps without a measured benefit. The TypeScript and
  Python helpers also ship stdlib-only, so all three language
  bindings stay on zero-dep footings.
- **JSON-RPC id is `json.RawMessage`.** The MCP spec allows
  numeric, string, and null ids. Forcing a typed alias (int
  or string) would truncate one of those forms and break
  round-trips through the server's `DeliverResponse` map
  (which is keyed by the stringified id bytes). `json.RawMessage`
  preserves the original bytes verbatim so anything the server
  emits round-trips cleanly back.
- **DispatchRequest returns the handler's map, not `interface{}`.**
  Handlers always return `map[string]any` per the
  `McpRequestHandler` signature, so the return shape is
  predictable. Callers that need a typed result can decode the
  map themselves. Keeping the return typed (vs. `any`) avoids
  the naked-type-assertion pattern in every call site.
- **`Close` is idempotent.** The Go SDK style is "defer close on
  open," matching how `bufio.Scanner` / `http.Response.Body`
  work. Multiple explicit closes are no-ops so callers never
  have to branch on "did I already close?". Matches
  `RawEventStream.Close` from §2.31 for symmetry.

Regression guards:

- **`TestMcpClient_ParsesRequestFrame`** — happy path: httptest
  server emits one request frame; Connect → Next → Value returns
  an event with the expected method + params.
- **`TestMcpClient_SkipsHeartbeatsAndMalformed`** — mixes
  `:heartbeat`, a malformed-data request, and a valid request;
  only the valid one is surfaced.
- **`TestMcpClient_SendResponseResult`** — POST /mcp/response
  with a result map carries the expected JSON body shape.
- **`TestMcpClient_SendResponseError`** — same for the error
  path; the `-32603` code + message round-trip.
- **`TestMcpClient_SendResponseRequiresExactlyOneOf`** — passing
  neither or both of result/error returns a validation error
  without hitting the network.
- **`TestMcpClient_DispatchRequestRoutesToHandler`** — full
  dispatch loop: handler sees params, return value becomes the
  posted result, method return value matches.
- **`TestMcpClient_DispatchRequestUnknownMethod`** — unknown
  method posts `-32601` and returns a non-nil error.
- **`TestMcpClient_DispatchRequestHandlerErrorPostsInternal`** —
  handler returns `errors.New("kaboom")`; client posts `-32603`
  with the message; the original error is returned so tests
  see it.
- **`TestMcpClient_DispatchRequestRejectsNotification`** —
  dispatching a notification event returns a clear error.
- **`TestMcpEvent_IsRequestRequiresID`** — IsRequest is true
  only when the payload has a non-empty non-null id.
- **`TestMcpEvent_ParamsAlwaysReturnsMap`** — `Params()` is
  non-nil even when the server omits the field.

What stayed deferred (shared across all three SDKs):

- **Async handler variant.** Handlers are sync today — an async
  variant would need either a context-aware `chan` dispatcher
  or a dedicated goroutine pool. Most Go tests don't need it.
- **Bidirectional cancellation testing under chaos.** We test
  the happy path + individual error branches but don't
  fuzz-close the stream mid-dispatch. Real-world usage covers
  this; no known bug.

### 2.40 TypeScript SDK MCP bidirectional helper  *(SDK — TypeScript)*

| Item                  | Location                                                     |
| --------------------- | ------------------------------------------------------------ |
| `McpClient` facade    | `sdk/typescript/src/mcp.ts` `McpClient.connect`, `sendResponse`, `dispatchRequest` |
| `McpEventStream`      | `sdk/typescript/src/mcp.ts` (fetch + `ReadableStream` parser, reuses `parseSSEFrame` from `client.ts`) |
| `McpEvent` + helpers  | `sdk/typescript/src/mcp.ts` `isRequest`, `paramsOf`, `parseMcpFrame` (exported for tests) |
| Package re-exports    | `sdk/typescript/src/index.ts` (`McpClient`, types, helpers) |
| Tests                 | `sdk/typescript/tests/mcp.test.ts` (15: 4 parse, 2 accessor, 9 HTTP end-to-end against a node:http server) |
| Verification          | **Live**: `npm run build` clean under tsc strict; `npm test` runs **53 tests** (was 38 → 53, +15 new) all green |

Closes the **TypeScript mirror** follow-up from §2.39. The TS SDK
now has full parity with Python on the MCP bidirectional surface:
the same three classes (`McpClient`, `McpEvent`, `McpEventStream`),
the same helper functions (`isRequest`, `paramsOf`,
`parseMcpFrame`), and the same method shapes so example code
translates one-for-one across the two SDKs.

What landed:

- **`McpClient`.** Stateless wrapper over `fetch` + `AbortController`.
  `connect()` opens a long-lived subscription to `/mcp/events` and
  returns an `McpEventStream` (see below). `sendResponse(id, {result}
  | {error})` POSTs a JSON-RPC reply to `/mcp/response` with the
  standard bearer-less shape MCP uses. `dispatchRequest(event,
  handlers)` routes a single event through a `method -> handler`
  map, auto-posts the reply, and re-throws on handler failure so
  tests still see the failure.
- **`McpEventStream`.** Async iterable backed by the fetch response
  body's `ReadableStream` reader. Each chunk is decoded with
  `TextDecoder`, split on the SSE blank-line terminator, and
  handed to a frame parser that reuses the existing
  `parseSSEFrame` from `client.ts` for line-level parsing. Adds
  JSON.parse + defensive drop-on-error so a malformed frame never
  kills the subscription. The trailing partial frame is drained
  on EOF so servers that omit the terminating blank line still
  work. `close()` aborts the underlying fetch via the controller
  so the test cleanup never leaks a hung reader.
- **`McpEvent` + helpers.** Type-only interface matching the
  Python dataclass shape one-for-one. The helper functions
  `isRequest(event)` (true only when the JSON-RPC id is present),
  `paramsOf(event)` (always returns a dict, even for
  missing/non-dict params), and `parseMcpFrame(frame)` (for unit
  tests that don't need a real HTTP stream) are exported so test
  harnesses can compose them without going through the full
  client.
- **`dispatchRequest` ergonomics.** Async handler signatures are
  supported natively (`async (params) => { ... }`). Unknown
  methods post `-32601` and throw `Error(method)`; handler
  throws post `-32603` carrying the error message and re-throw
  the original `Error`. Matches Python semantics exactly so a
  test suite can move between the two languages without
  surprises.
- **Package re-exports.** `import { McpClient, McpEvent,
  isRequest, paramsOf, parseMcpFrame } from "mockagents"` works
  alongside the existing `MockAgentClient` / `Scenario` /
  `expect` surface. Type-only exports (`McpClientOptions`,
  `JsonRpcEnvelope`, `JsonRpcError`, `McpRequestHandler`) are
  split through `export type` so the build tree-shakes cleanly.

Design notes:

- **Reuse `parseSSEFrame` from `client.ts`.** The existing
  streaming helper already has a battle-tested SSE frame parser
  (handles multi-line `data:` joining, heartbeat comments, and
  leading-space stripping). Duplicating it for the MCP module
  would risk the two parsers drifting. The new `parseMcpFrame`
  is a thin wrapper that calls `parseSSEFrame` + `JSON.parse` +
  a defensive null check.
- **No timeout on `/mcp/events`.** SSE subscriptions are
  long-lived by design — slapping `timeoutMs` on the fetch would
  break tests that expect to hold the stream open for multiple
  round-trips. The per-request timeout on `sendResponse` still
  uses the shared `timeoutMs` option because those are short-
  lived POSTs. Matches the Go SDK's `requestSSE` pattern from
  §2.31.
- **AbortController-based close.** Calling `stream.close()`
  aborts the fetch via the shared controller; the iterator
  catches the `AbortError` and exits cleanly so `for await`
  loops don't throw on cleanup. Idempotent — multiple close
  calls are safe.
- **Type-only sensitive exports.** `McpClientOptions`,
  `JsonRpcEnvelope`, `JsonRpcError`, and `McpRequestHandler`
  are exported via `export type` so the built JS bundle does
  not carry them. Only the runtime-necessary `McpClient` class
  and helper functions survive the tree-shake.
- **Consistent with `client.ts` patterns.** The new module
  follows the same error-handling conventions as `requestSSE`
  in `client.ts`: check `resp.ok`, throw `HTTPError` with the
  body on non-2xx, release the reader lock in a `finally`
  block. Reviewers don't need to learn a new pattern to audit
  the MCP module.

Regression guards:

- **`parseMcpFrame` unit tests** — decodes request + notification
  frames, drops malformed-data frames, drops no-data frames.
- **`isRequest` / `paramsOf` accessor tests** — is-request
  requires an id; params-of always returns a dict even for
  missing or non-object params.
- **`McpClient against node:http server` — 9 end-to-end tests:**
  - parses a request frame from a real SSE stream
  - skips heartbeats + malformed frames interleaved with valid
    ones
  - `sendResponse` posts the expected JSON body shape
  - `sendResponse` rejects when neither `result` nor `error`
    is supplied
  - `sendResponse` throws `HTTPError` on non-2xx
  - `dispatchRequest` routes to the handler, posts the result,
    returns the handler's value, and passes the params through
  - `dispatchRequest` unknown method → `-32601` posted, throws
  - `dispatchRequest` handler throws → `-32603` posted, original
    error rethrown
  - `dispatchRequest` rejects non-request events with a clear
    error

What stayed deferred (unchanged from §2.39):

- **Go SDK equivalent.** The Go SDK can already talk to
  `/mcp/events` / `/mcp/response` via the raw `http.Client`.
  Wrapping that in a dedicated `McpClient` type is still a
  future slice.
- **Async iterator cancellation propagation.** Calling
  `stream.close()` mid-iteration aborts the fetch; the `for
  await` loop then exits on the next turn. A very rapid
  close-then-iterate sequence could still see one buffered
  frame before the abort lands — acceptable for the v0.3
  surface, can be tightened later if someone hits it.

### 2.39 Python SDK MCP bidirectional helper  *(SDK — Python)*

| Item                      | Location                                                 |
| ------------------------- | -------------------------------------------------------- |
| `McpClient` facade        | `sdk/python/mockagents/mcp.py` `McpClient.connect`, `send_response`, `dispatch_request` |
| `McpEventStream` iterator | `sdk/python/mockagents/mcp.py` (context-managed SSE parser: heartbeat skip, multi-line data joins, malformed-frame tolerance) |
| `McpEvent` dataclass      | `sdk/python/mockagents/mcp.py` (typed accessors: `is_request`, `is_notification`, `request_id`, `method`, `params`) |
| Package re-exports        | `sdk/python/mockagents/__init__.py` (`McpClient`, `McpEvent`, `McpEventStream` + `__all__` rows) |
| Tests                     | `sdk/python/tests/test_mcp.py` (14: request/notification parsing, heartbeat skip, multi-line data, context-manager close, send_response result/error, exactly-one-of guard, dispatch happy path, -32601 unknown method, -32603 handler raise, non-request reject, params dict guard, missing-id guard) |
| Verification              | **Live**: `python -m pytest tests/` runs **104 tests** (was 90 → 104, +14 new) all green |

Closes the **Client SDK helpers (Python / TS) that wrap the SSE
parse loop + dispatcher** follow-up explicitly deferred in §2.33.
The Python half ships now; the TypeScript half stays on the
follow-up list until someone needs it (the Python flow is the
pattern that will translate over).

What landed:

- **`McpEventStream`.** Context-managed iterator over
  `GET /mcp/events`. Consumes a `requests.Response` with
  `stream=True` under the hood, parses SSE frames according to
  the spec (blank-line terminator, `event:` + multi-line
  `data:`, leading-space stripping after the colon), and yields
  typed `McpEvent` objects. Heartbeat comments (`:heartbeat`)
  and malformed `data:` frames are dropped silently so a
  misbehaving server cannot break a test harness mid-stream.
  Supports `with ... as stream:` for deterministic close.
- **`McpEvent` dataclass.** Holds the SSE `kind` string
  (`"request"` or `"notification"`) and the decoded JSON-RPC
  payload. Typed accessors — `is_request`, `is_notification`,
  `request_id`, `method`, `params` — keep call sites short and
  defensive: `params` always returns a dict even when the
  server omits the field or sends a non-object value, so
  handler code can use `params.get(...)` without a None check.
- **`McpClient.send_response(request_id, *, result=…, error=…)`.**
  POSTs a JSON-RPC reply to `/mcp/response`. Enforces
  exactly-one-of `result`/`error` via a `ValueError` at call
  time so mis-wired replies never reach the server. Raises
  `requests.HTTPError` on 404 (unknown id — the server enforces
  this too).
- **`McpClient.dispatch_request(event, handlers)`.** The
  convenience that most test harnesses will use. Takes a
  `method -> handler` map; the handler is called with the
  parsed `params` dict and must return a `result` dict. On an
  unknown method the helper POSTs a JSON-RPC `-32601` error
  and raises `KeyError(method)`. If the handler itself raises,
  the helper POSTs a `-32603` error carrying the exception
  string and re-raises so the test can still see the failure.
- **`McpClient` as a context manager.** `with McpClient(...) as
  c:` closes the underlying `requests.Session` on exit, matching
  the existing `MockAgentServer` / `MockAgentClient` pattern
  for cleanup symmetry.
- **Package re-exports.** `from mockagents import McpClient,
  McpEvent, McpEventStream` works alongside the existing
  `MockAgentClient` / `Scenario` / `expect` surface, and
  `__all__` is updated so `from mockagents import *` includes
  the new names.

Design notes:

- **Why a separate module instead of methods on `MockAgentClient`?**
  `MockAgentClient` is the OpenAI / Anthropic client — its wire
  format is chat completions + messages. MCP speaks JSON-RPC over
  a different set of endpoints (`/mcp`, `/mcp/events`,
  `/mcp/response`) and has zero overlap with chat payloads.
  Keeping the two surfaces separate means each class stays
  tight and users import only what they need.
- **Handler return shape stays untyped.** MCP's
  `sampling/createMessage` result is a provider-specific blob
  that looks roughly like an Anthropic `Message`; `roots/list`
  returns a list of roots. We don't pin a dataclass because
  there's no single spec for what the result "should" look
  like — handlers return whatever dict the server expects, and
  the helper just forwards it. Users who want typing can wrap
  the helper with their own dataclasses.
- **`params` normalization.** The JSON-RPC spec allows `params`
  to be absent, a dict, or an array. MockAgents always emits a
  dict for sampling/roots, but the accessor returns an empty
  dict on both "missing" and "non-dict" so handlers never crash
  on `params.get(...)` when talking to a lenient server.
- **Defensive JSON parsing.** `_try_json` returns `None` on
  `JSONDecodeError` and the iterator drops that frame. This
  matches the existing `message_stream` behavior and is the
  safest default — a malformed frame in the middle of a
  legitimate stream should not abort the whole subscription.
- **SSE multi-line data joining.** Per the SSE spec, consecutive
  `data:` lines in a frame are joined with a literal newline
  before JSON decoding. The `message_stream` client (§2.25)
  ignores the multi-line case because Anthropic never emits it;
  MCP v0.3 intentionally supports it, so the new parser
  handles it correctly. One test (`test_event_stream_joins_multiline_data`)
  pins that behavior.

Regression guards:

- **`test_event_stream_parses_request_frame`** — happy path:
  `event: request\ndata: {...}` → `McpEvent(kind="request",
  payload=...)` with all typed accessors populated.
- **`test_event_stream_parses_notification_frame`** — same for
  notifications; `request_id` is `None`.
- **`test_event_stream_skips_heartbeat_and_malformed_data`** —
  `:heartbeat` and non-JSON `data:` lines are dropped silently;
  the next valid frame still comes through.
- **`test_event_stream_joins_multiline_data`** — two `data:`
  lines joined with newline before JSON decode.
- **`test_event_stream_context_manager_closes`** — `with
  McpEventStream(resp):` closes the underlying response on exit.
- **`test_send_response_result_payload`** / **`_error_payload`**
  — both reply shapes reach `/mcp/response` with the expected
  JSON body.
- **`test_send_response_requires_exactly_one_of_result_or_error`**
  — neither supplied or both supplied raises `ValueError`.
- **`test_dispatch_request_routes_to_handler`** — happy path:
  handler receives the params dict, return value is posted as
  the response result, method return matches.
- **`test_dispatch_request_unknown_method_posts_32601`** —
  unknown method posts the JSON-RPC "method not found" code
  and raises `KeyError`.
- **`test_dispatch_request_handler_raises_posts_32603_and_reraises`**
  — handler exceptions post `-32603` AND the original exception
  is re-raised so tests see it.
- **`test_dispatch_request_rejects_notification_event`** —
  dispatching a non-request event raises `ValueError`.
- **`test_event_params_always_returns_dict`** — missing and
  non-dict `params` both produce `{}`.
- **`test_event_is_request_requires_id`** — defensive check that
  a payload without an id is treated as a notification.

What stayed deferred:

- **TypeScript mirror.** The TS SDK needs the same three
  classes with its fetch-based transport. Same design, ~150
  lines. Not shipping here to keep the slice focused; will
  land when the first TS test harness asks for it.
- **Async / streaming handlers.** Handlers are sync dicts today.
  An async variant (`async def handler(params)` +
  `aiohttp`-backed `McpClient`) is a future slice — most Python
  test suites don't need it and the current surface is usable
  under `pytest-asyncio` via `asyncio.to_thread`.
- **Go SDK equivalent.** The Go SDK can already talk to
  `/mcp/events` / `/mcp/response` via the raw `http.Client`.
  Wrapping that in an `McpClient` type is a future slice — the
  Go engine can also be exercised in-process via `internal/mcp`,
  so the external client sugar is lower-priority.

### 2.38 Pipeline validator + CLI multi-kind validate  *(config + CLI)*

| Item                   | Location                                                 |
| ---------------------- | -------------------------------------------------------- |
| Pipeline rule set      | `internal/config/pipeline_validator.go` `ValidatePipeline` (apiVersion, kind, metadata.name, topology enum, non-empty agents, unique ids, edge references, no self-loops, edges-only-in-graph warning) |
| ValidateBytes wiring   | `internal/config/validate_bytes.go` Pipeline branch now runs the full validator instead of parse-only |
| CLI multi-kind support | `cmd/mockagents/validate.go` switched to `config.LoadAllDocuments`; agent validator runs on Agents, pipeline validator runs on Pipelines, both collected into one summary |
| Unit tests             | `internal/config/pipeline_validator_test.go` (9: valid, missing name, invalid topology, empty agents, duplicate node id, edge → unknown node, self-loop, edges under non-graph, missing kind) |
| Verification           | **Live**: `go test ./...` across 21 packages green; `go run ./cmd/mockagents validate examples/` reports "Validated 9 file(s): all valid." (was "all agents valid" — now covers pipelines too) |

Promotes the GUI config editor (§2.35) and the CLI `validate`
subcommand from "Agents-only" to "Agents + Pipelines". Before this
slice, a malformed pipeline document — a dangling edge, a duplicate
node id, or an unknown topology — would only surface at server
start time or at runtime inside the pipeline executor. Now
`mockagents validate` and the GUI editor catch it at load time
with a line-number-aware error message.

What landed:

- **`ValidatePipeline(def, filePath, node)`.** New entry point in
  `internal/config` that mirrors the shape of the existing
  `Validator.Validate` for agents: accumulates errors in a
  `validationContext`, stamps each error with line + column from
  the yaml.Node tree, and returns nil when the definition is
  clean. Nine rules:
  - `apiVersion` required and equal to `mockagents/v1`.
  - `kind` required and equal to `Pipeline`.
  - `metadata.name` required, lowercase kebab-case, ≤63 chars.
  - `spec.topology` required and one of
    `sequential`/`parallel`/`graph`.
  - `spec.agents` non-empty.
  - Every node has an `id` and a `ref`.
  - Every node id is unique within the pipeline.
  - Every edge's `from` and `to` reference a declared node id.
  - No self-loops (`from == to`).
  - Edges under `sequential` or `parallel` topology produce a
    clear error: "edges are only honored under topology
    \"graph\"".
- **`ValidateBytes` Pipeline branch upgraded.** Previously
  parse-only; now runs `ValidatePipeline` on every successful
  decode. The GUI `/editor` page (§2.35) automatically picks up
  the new rules — no changes needed on the client side, the wire
  shape (`ValidateResult { ok, kind, errors[] }`) was already
  structured to carry rule-based errors.
- **CLI `validate` covers every kind.** `cmd/mockagents/validate.go`
  used `config.LoadDir` / `config.LoadFile`, which only surface
  `kind: Agent` documents. It now uses `config.LoadAllDocuments`
  so pipelines register alongside agents, and runs the
  appropriate validator against each document kind. The total-
  files count + summary include both buckets. TestSuite and
  MCPServer documents are still loaded but not rule-validated
  (they rely on the typed decode for structural correctness
  until a dedicated validator lands).
- **Single-file path dispatch.** `mockagents validate
  my-pipeline.yaml` now tries `LoadFile` first (Agent), falls
  back to `LoadPipelineFile` on kind mismatch, and surfaces the
  original parse error only when neither path accepts the file.

Design notes:

- **Why a separate validator instead of extending the existing
  `Validator`?** The existing `Validator` is typed against
  `*types.AgentDefinition`. Generalizing it to handle every kind
  would either widen the signature (`any` + type switch) or force
  a sum type. A sibling package-level function keeps the Agent
  validator tight and reuses `validationContext` /
  `addError` / `LineColOf` for zero-duplication of the error-
  surfacing plumbing.
- **"edges under non-graph" is an error, not a warning.** The
  pipeline executor silently ignores `spec.edges` under
  sequential/parallel topology — operators who hand-wrote edges
  there expect them to fire. Surfacing that as an error at load
  time forces the fix (either change the topology or delete the
  edges) instead of letting the misconfiguration lurk.
- **No cycle detection.** Cycles in a graph topology are
  technically allowed by the current executor — it visits each
  reachable node once. A dedicated cycle-detection pass could
  warn on unreachable-after-a-loop nodes, but would require
  tarjan-style SCC analysis and is out of scope here. Orphan
  node detection (a node with no incoming edges under graph
  topology) is deliberately deferred for the same reason.
- **Parse-only fallback for TestSuite/MCPServer.** Those kinds
  don't have rule-based validators yet; they rely on
  `yaml.Decode` catching structural problems. A follow-up slice
  can add them when someone hits a surprising decode-time
  failure.

Regression guards:

- **`TestValidatePipeline_Valid`** — happy path: every rule
  passes on a minimal two-node sequential pipeline.
- **`TestValidatePipeline_MissingName`** — metadata.name absent
  → `metadata.name` error.
- **`TestValidatePipeline_InvalidTopology`** — `topology: star`
  → `spec.topology` error.
- **`TestValidatePipeline_EmptyAgents`** — `agents: []` →
  `spec.agents` error.
- **`TestValidatePipeline_DuplicateNodeID`** — two nodes with
  `id: w` → "duplicate" error.
- **`TestValidatePipeline_EdgeReferencesUnknownNode`** — edge
  pointing at `to: nope` → "unknown node" error.
- **`TestValidatePipeline_SelfLoop`** — `from: a, to: a` →
  "self-loop" error.
- **`TestValidatePipeline_EdgesUnderNonGraphTopology`** —
  sequential pipeline with edges → "only honored under topology
  graph" error.
- **`TestValidatePipeline_MissingKind`** — missing `kind:` →
  `kind` error.

What stayed deferred:

- **Cycle + orphan detection** in graph topology — needs a real
  graph traversal pass.
- **TestSuite / MCPServer rule-based validators** — parse-only
  for now. Same plumbing will apply when someone wants them.
- **Edge `when_contains` substring validation** — today we
  accept any string; a future slice could warn on empty
  substrings or duplicate edge conditions.

### 2.37 Multi-tenancy — API key rotation  *(tenancy + GUI)*

| Item                   | Location                                                 |
| ---------------------- | -------------------------------------------------------- |
| Audit event kind       | `internal/audit/types.go` `EventAPIKeyRotated` + `Valid()` switch |
| Store interface        | `internal/tenancy/store.go` `Store.RotateAPIKey(ctx, id) (*NewAPIKeyResult, oldPrefix string, err error)` |
| SQLite implementation  | `internal/tenancy/store.go` transactional read-old / generate-new / write-new; resets `last_used`; flushes auth cache |
| HTTP handler           | `internal/server/tenancy_handlers.go` `RotateAPIKey` (emits `EventAPIKeyRotated` with old/new prefix in Details) |
| Route mount            | `internal/server/server.go` `POST /api/v1/keys/{id}/rotate` (admin-gated) |
| API client helper      | `gui/lib/api.ts` `rotateAPIKey(id)` |
| Rotate button          | `gui/app/admin/tenants/[id]/page.tsx` `rotateKeyAction` + `btn-xsmall` button next to Delete; reuses the existing once-only plaintext banner |
| Store tests            | `internal/tenancy/rotate_test.go` (3: happy path round-trip, unknown id, auth-cache flush) |
| Handler tests          | `internal/server/tenancy_handlers_test.go` (2: happy path HTTP flow, unknown id → 404) |
| Verification           | **Live**: full `go test ./...` across 21 packages green; `npx next build` clean with 15 routes unchanged |

Opens a partial close on the **Multi-tenancy | key rotation** row
from §6 (the SaaS SSO/OAuth/billing items are still deferred).
Before this slice operators had to create a new key + delete the
old one as two separate steps, with a window where neither worked.
Now a single click on Rotate swaps the secret in place.

What landed:

- **`Store.RotateAPIKey(ctx, id)`.** New interface method that
  atomically swaps the secret behind an existing key while
  preserving every immutable field. The SQLite implementation
  wraps the read-old / generate-new / write-new sequence in a
  `BeginTx` so a crash or cancellation cannot leave the row with
  a broken hash/prefix pair. `last_used` is cleared because a
  rotated key has no prior usage of its new plaintext — keeping
  the old timestamp would confuse "when did this credential last
  work?" investigations. The auth cache is flushed on commit so
  a cached Principal cannot linger past the rotation.
- **Old-prefix surfaced to the caller.** `RotateAPIKey` returns
  the prefix the old secret was using alongside the new
  `NewAPIKeyResult`. The HTTP handler logs both in the audit
  event's Details so operators can correlate a rotation with the
  specific compromised credential they were responding to (the
  prefix is the only public identifier that travels with a key
  in logs / request headers).
- **New audit event kind.** `EventAPIKeyRotated` joins the seven
  existing kinds. The `Valid()` switch was extended so the
  `/api/v1/audit?kind=api_key.rotated` filter works.
- **`POST /api/v1/keys/{id}/rotate` handler.** Admin-gated via
  `RequireRole(RoleAdmin, ...)`. Returns the full
  `NewAPIKeyResult` — the GUI surfaces the plaintext once and
  then drops it, matching the create-key flow from §2.32.
- **GUI Rotate button.** Per-row `btn-xsmall` button next to
  Delete in `/admin/tenants/[id]`. Clicking posts through a
  `rotateKeyAction` server action (which injects the auth cookie
  as Bearer automatically), then redirects with the plaintext +
  key name in the query string so the existing once-only banner
  renders it exactly the same way minting a new key does. No
  new UI components — the banner and the auth-cookie plumbing
  from §2.32 both already exist.

Design notes:

- **Why rotate in place instead of create-then-delete?** The
  caller's CI scripts, audit trails, and dashboards all
  reference the key by its id (`key_xxxxxxxx`). A create-then-
  delete flow would force every one of those consumers to
  update their id references — and would leave a window where
  two keys are simultaneously valid. Rotation in place keeps the
  id stable and guarantees the old secret stops working the
  moment the new one takes effect.
- **Transactional swap, not a simple UPDATE.** The transaction
  matters because we need to *read* the existing row (to get
  the old prefix for the audit trail and the immutable fields
  for the response) and then *write* the new secret. Doing
  those in two separate statements without a transaction opens
  a race where another goroutine could delete the key between
  our read and our write. The `BeginTx` closes that window for
  free.
- **Reset `last_used` on rotation.** The old timestamp reflects
  the *old* secret's activity. A rotated key's first use under
  its new secret is semantically a fresh event; leaving the old
  timestamp in place would confuse later investigations.
- **Old plaintext is never returned.** The audit trail carries
  the old *prefix* (public), never the old plaintext. That's
  enough for correlation without widening the blast radius of
  a compromised audit log.
- **Auth cache flush is invalidate-all, not key-specific.** The
  `auth_cache` from §2.21 is hash-keyed, not id-keyed, so we
  can't cleanly evict just the rotated entry. `Invalidate()` is
  cheap (resets the map) and rotation is a rare admin action,
  so the blunt flush is fine — it's the same pattern
  `UpdateAPIKeyRole` uses.

Regression guards:

- **`TestRotateAPIKey`** — full round-trip: pre-rotation Resolve
  works, rotation swaps the secret, post-rotation Resolve with
  the old plaintext returns `ErrInvalidKey`, post-rotation
  Resolve with the new plaintext returns the correct Principal
  (same id, tenant, role). Also asserts id/name/role/tenant are
  stable across the rotation.
- **`TestRotateAPIKey_UnknownID`** — bogus id returns
  `ErrNotFound` so the handler can 404 cleanly.
- **`TestRotateAPIKey_FlushesAuthCache`** — cached Principal for
  the old plaintext cannot linger. Warms the cache, rotates,
  then re-Resolves the old plaintext and asserts it returns
  `ErrInvalidKey` (not a cached success).
- **`TestTenancyHandlers_RotateAPIKey`** — full HTTP flow against
  a real store: the returned `NewAPIKeyResult` has a different
  plaintext than the original, the id is preserved, and the old
  plaintext stops resolving while the new one starts.
- **`TestTenancyHandlers_RotateAPIKey_NotFound`** — unknown key
  id → 404.

What stayed deferred:

- **Grace period / overlap rotation.** A "rotate with 24-hour
  overlap" mode would let both old and new secrets work for a
  window so CI can pick up the new secret before the old one
  dies. Needs a schema change (two hashes per row) and clearer
  threat modeling around "who can invalidate the old hash
  early?" — out of scope here.
- **Bulk rotation.** Rotating every key in a tenant in one call
  is useful after a suspected compromise but adds audit-event
  volume and needs a "which keys were touched?" response shape.
  Separate slice.
- **Self-rotation.** The caller rotates *their own* key by
  passing their auth cookie and hitting a `/me/rotate` path.
  Nice-to-have but requires the auth middleware to surface the
  caller's key id, which it doesn't today. Separate slice.

### 2.36 GUI v0.3 — pipeline DAG viewer + mgmt API  *(GUI + server)*

| Item                          | Location                                                 |
| ----------------------------- | -------------------------------------------------------- |
| Start-time pipeline loading   | `cmd/mockagents/start.go` (now uses `config.LoadAllDocuments`; registers every `kind: Pipeline` in a new `engine.PipelineRegistry`) |
| Server config wiring          | `internal/server/server.go` `Config.Pipelines` + route mount (only when the registry is non-nil) |
| `PipelineHandlers`            | `internal/server/pipeline_handlers.go` `ListPipelines`, `GetPipeline`, `PipelineSummary` |
| API helpers                   | `gui/lib/api.ts` `listPipelines`, `getPipeline`, `PipelineSummary`, `PipelineDefinition`, `PipelineAgent`, `PipelineEdge` |
| List page                     | `gui/app/pipelines/page.tsx` (card grid, topology badge, agent/edge counts) |
| Detail page                   | `gui/app/pipelines/[name]/page.tsx` (breadcrumb + meta row + DAG viewer + per-node table linking to agent detail) |
| DAG viewer component          | `gui/app/pipelines/[name]/DAGViewer.tsx` (pure SVG, server-compatible, longest-path layered layout for sequential / parallel / graph) |
| Nav link                      | `gui/app/layout.tsx` (`Pipelines` between `Agents` and `Logs`) |
| Styles                        | `gui/app/globals.css` (`.pipeline-meta`, `.dag-wrap`, `.dag-empty`, `.dag-edges`, `.dag-node-rect`, `.dag-node-id`, `.dag-node-ref`, `.dag-when`) |
| Tests                         | `internal/server/pipeline_handlers_test.go` (5: list with two pipelines, empty registry, get by name, 404, nil registry) |
| Verification                  | **Live**: full `go test ./...` across 21 packages green; `npm run build` clean with **15 routes** (was 13) — `/pipelines` and `/pipelines/[name]` both added, shared JS unchanged at 102 kB |

Closes a substantial chunk of the **GUI | workflow editor** row
from §6: the read-only viewer half. A full drag-to-rewire editor
is still open (it needs a DAG widget like React Flow and a
YAML-sync layer), but operators can now browse every `kind: Pipeline`
document the server loaded from the browser.

What landed:

- **Start-time pipeline loading.** Before this slice,
  `mockagents start` only registered agents — pipelines were
  invisible to the running server (they were only touched by
  `mockagents test`). Start now calls `config.LoadAllDocuments`,
  which already buckets every document kind, and registers each
  pipeline in a new `engine.PipelineRegistry` alongside the agent
  registry. Individual pipeline parse failures are logged but
  non-fatal — the server still boots as long as at least one
  agent loaded cleanly.
- **`PipelineRegistry` wired into `server.Config`.** A new
  `Config.Pipelines` field carries the registry into the server.
  When it is nil (test suites that don't populate it, for
  example) the `/api/v1/pipelines` routes are simply not mounted
  — no 500s, no empty-registry confusion.
- **`GET /api/v1/pipelines`.** Lists every registered pipeline as
  a `PipelineSummary { name, description, topology, agent_count,
  edge_count }`. Rows are sorted by name ascending (the registry
  owns that). An empty list is a well-formed 200, not a 404 — so
  the GUI renders "no pipelines loaded" instead of a crash
  banner.
- **`GET /api/v1/pipelines/{name}`.** Returns the full
  `PipelineDefinition` struct (apiVersion + kind + metadata +
  spec). 404 when the name isn't registered, 400 when the name
  path segment is missing. The GUI's DAG viewer consumes nodes
  and edges directly from this shape, no remapping.
- **GUI list page.** `/pipelines` renders one card per pipeline
  using the same `.card-grid` / `.card` styles as the agent
  catalog so the two feel uniform. Topology is a badge; the stats
  block shows agent + edge counts.
- **GUI detail page.** `/pipelines/[name]` is a server component
  that fetches the definition, normalizes edges (sequential →
  synthesized linear chain, parallel → empty, graph → declared),
  and hands the agents + edges to the `DAGViewer` client-safe
  component. Below the viewer is a table mapping each node id to
  its agent ref, with each ref linking to `/agents/{ref}` so
  operators can jump from "this node in the pipeline" to "the
  agent definition that powers it" in one click.
- **DAG viewer.** Pure SVG, no React Flow dep. The layout
  algorithm is a longest-path layered assignment: compute
  `inDegree` + `forward` adjacency; BFS from every source
  (inDegree == 0) and assign each visited node a layer equal to
  `max(layer of predecessors) + 1`; group by layer for x-axis
  placement and preserve YAML order within each layer for y-axis
  stacking. Parallel topology takes a short-circuit branch and
  stacks all nodes in layer 0. Any agent the BFS misses (orphan
  or cycle) falls through to layer 0 so it still renders. The
  viewer emits one `<rect>` per node, a cubic-bezier `<path>` per
  edge with an `#arrow` marker, and an optional `<text>` element
  carrying the `when_contains` guard for graph edges. Everything
  is a pure function of the inputs so the component stays
  server-renderable (no `useState` / `useEffect`).
- **Nav + styles.** `Pipelines` nav link slotted between Agents
  and Logs. `.dag-*` CSS classes added to `globals.css`; the SVG
  inherits colors via `currentColor` and CSS variables so the
  viewer follows the existing light/dark theme automatically.

Design notes:

- **Why SVG instead of React Flow?** React Flow is ~300 kB gzipped
  and pulls in its own state machine, drag handlers, zoom/pan
  controls, and a custom renderer. The read-only viewer uses
  none of that — static coordinates, one render, done. Shipping
  SVG keeps the v0.3 bundle flat at 102 kB shared JS and avoids
  a "pinned major version" dep for a feature that doesn't need
  the library. The full editor slice can adopt React Flow when
  it actually needs drag handles.
- **Longest-path layered layout beats simple BFS.** Plain BFS
  ("layer = shortest path from source") collapses diamond
  shapes — if node C is reachable via both A→C and A→B→C, BFS
  puts C at layer 1 but the B→C edge then travels backwards. The
  longest-path variant assigns C to layer 2, keeping every edge
  strictly left-to-right. Still O(V+E) because each node is
  processed once per predecessor.
- **Normalize edges on the server component side.** The Go handler
  returns the raw `PipelineDefinition`. The client component
  synthesizes sequential/parallel edges because a future
  "edit the YAML" slice will want to round-trip the same shape.
  Splitting the normalization into a helper in the detail page
  keeps the viewer component single-purpose.
- **Route guarding on registry presence, not on document count.**
  The stream endpoint pattern from §2.34 sets the precedent:
  features are enabled or disabled by whether the backing state
  exists, not by runtime counts. A server started with a valid
  agents dir but no pipeline documents simply returns `[]` from
  `/api/v1/pipelines`; only a server with no pipeline *registry*
  at all (unusual, test-suite-only code path) returns 404.

Regression guards:

- **`TestPipelineHandlers_List`** — two pipelines → sorted
  summaries with topology + agent count.
- **`TestPipelineHandlers_ListEmptyRegistry`** — empty registry
  returns a well-formed `[]`.
- **`TestPipelineHandlers_GetByName`** — detail fetch round-trips
  the full `PipelineDefinition`.
- **`TestPipelineHandlers_NotFound`** — unknown name returns 404.
- **`TestPipelineHandlers_NilRegistryListReturnsEmpty`** — nil
  registry still serves a 200 on list (matches the broadcaster
  nil-receiver pattern).

What stayed deferred:

- **Drag-to-rewire editor.** Needs React Flow or a comparable DAG
  widget plus a YAML round-trip layer. Separate slice.
- **Schema-aware node property editor.** Clicking a node would
  open an inline form populated from `schema/mockagents-v1-pipeline.json`.
  Out of scope here.
- **Validation of the inbound pipeline YAML.** The existing
  `ValidateBytes` (§2.35) only runs the Agent validator — a
  pipeline-specific rule set (edges reference declared nodes,
  topology matches edge shape, etc.) is a follow-up to that
  slice, not this one.
- **Live pipeline runs.** Executing a pipeline from the browser
  and streaming node outputs back would be a "pipeline console"
  slice — depends on both this viewer and the existing runner.

### 2.35 GUI v0.3 — schema-aware config editor  *(GUI + server)*

| Item                      | Location                                                 |
| ------------------------- | -------------------------------------------------------- |
| `config.ValidateBytes`    | `internal/config/validate_bytes.go` (new) — parse + kind dispatch + Agent validator, single `ValidateReport` shape for Agent / Pipeline / TestSuite / MCPServer |
| `POST /api/v1/config/validate` | `internal/server/validate_handler.go` `ValidateHandler` (raw YAML body or `{"yaml":"..."}` JSON wrapper; always 200 with `ok` flag) |
| Route mount               | `internal/server/server.go` (admin-gated as `RoleEditor` in multi-tenant, open in single-tenant) |
| API client helper         | `gui/lib/api.ts` `validateYAML`, `ValidateResult`, `ValidationError` |
| Editor page               | `gui/app/editor/page.tsx` (server component with a `validateAction` server action) |
| Client editor widget      | `gui/app/editor/YamlEditor.tsx` (textarea + line gutter + validate button + error panel) |
| Header nav link           | `gui/app/layout.tsx` (`Editor` added between Audit and Admin) |
| Styles                    | `gui/app/globals.css` (`.editor-layout`, `.editor-toolbar`, `.editor-grid`, `.editor-gutter`, `.editor-textarea`, `.editor-errors`) |
| Unit tests                | `internal/config/validate_bytes_test.go` (7: valid agent, invalid protocol, yaml parse error, empty, unknown kind, pipeline kind, JSON input) |
| HTTP tests                | `internal/server/validate_handler_test.go` (4: raw YAML valid, raw YAML invalid → 200 + ok=false, JSON wrapper, method not allowed) |
| Verification              | **Live**: full `go test ./...` across 21 packages green; `npm run build` clean with **13 routes** (was 12); `/editor` at 1.15 kB, shared JS unchanged at 102 kB |

Closes the **GUI | schema-aware config editor** sub-item from §6.
Operators can now paste an agent YAML into the browser, click
**Validate**, and see server-side errors inline — the same errors
`mockagents validate` prints on the CLI.

What landed:

- **`ValidateBytes`.** New package-level entry point in
  `internal/config` that accepts a byte slice, runs the same
  two-pass decode + Agent validator that `LoadFile` does, but
  skips the filesystem entirely. The result is a typed
  `ValidateReport { Kind, Errors }` so callers can render a
  single response shape regardless of parse-time vs schema-time
  failures. Empty documents, malformed YAML (line number extracted
  from `yaml.v3`'s error string), unknown kinds, and the
  non-Agent kinds (`Pipeline`, `TestSuite`, `MCPServer`) all flow
  through the same path — non-Agent kinds get parse-only
  validation since those types have no rule-based validator yet.
- **`POST /api/v1/config/validate`.** New stateless server handler
  in `internal/server/validate_handler.go` that reads the request
  body (raw YAML is the default; a JSON-wrapped
  `{"yaml": "..."}` shape is supported via Content-Type detection
  for curl ergonomics), calls `ValidateBytes`, and always returns
  200 with an `ok` boolean. Validation failures are not HTTP
  failures — the GUI renders them inline, the CLI can exit 1 on
  `ok=false`. In multi-tenant mode the route is gated behind the
  `editor` role so viewers can't spray YAML at the parser as a
  cheap fingerprinting vector.
- **Editor page.** `/editor` is a server component that defines a
  `validateAction` server action inline. The action calls
  `validateYAML` in `api.ts`, which injects the auth cookie as
  Bearer automatically — so the editor works identically in
  single-tenant and multi-tenant deployments. The server
  component imports the client widget and passes the action
  down as a prop.
- **Client editor widget.** `YamlEditor.tsx` is a client component
  rendering a textarea (not Monaco) with a sibling line-number
  gutter, a Validate button wired through `useTransition` so the
  button shows "Validating…" without blocking the page, a Reset
  button, and an error panel below. The textarea is deliberately
  plain: dropping in a full Monaco widget would add ~3 MB of JS
  for features (autocomplete, folding, minimap) most operators
  won't use. The gutter uses a `<pre>` filled with line numbers
  so its line-height exactly matches the textarea's, keeping the
  two columns aligned during edit.
- **Seed document.** The editor starts with a minimal valid
  `hello-world` agent so first-time visitors see something they
  can mutate. Reset restores the seed.
- **Header link.** `Editor` added to the main nav between Audit
  and Admin so the feature is one click from every page.

Design notes:

- **Server-side validation is the source of truth.** A browser-
  side JSON-schema validator (ajv, @stoplight/spectral, etc.)
  would mean two copies of the rules — one in Go, one in JS —
  and they would inevitably drift. Delegating to the Go validator
  via a small HTTP endpoint keeps the CLI and the GUI in
  lockstep forever, at the cost of one 4 ms round-trip per
  Validate click.
- **Why not auto-validate on keystroke?** A round-trip per
  keystroke would hammer the server with malformed intermediate
  YAML and churn the GUI's state. A Validate button is a
  deliberate, explicit action — matches the
  compile-edit-compile rhythm operators already use on the CLI.
- **Validation is not persistence.** The editor is a validation
  playground — the page lede says so explicitly. A future slice
  could add a "save back to disk" flow behind an admin toggle,
  but that would open a path to unsandboxed filesystem writes
  from the browser and needs its own security design. Out of
  scope here.
- **Forgiving content-type handling.** The handler accepts both
  raw YAML (what the GUI posts, with `Content-Type:
  application/x-yaml`) and a `{"yaml": "..."}` JSON wrapper (what
  curl scripts naturally produce, with `Content-Type:
  application/json`). If the Content-Type is JSON but the body
  can't be unmarshaled into the wrapper, the handler falls
  through to raw-YAML parsing — the interface never refuses a
  valid document because the caller picked the wrong framing.

Regression guards:

- **`TestValidateBytes_ValidAgent`** — happy path: full agent
  definition with a default scenario returns zero errors and
  `Kind: "Agent"`.
- **`TestValidateBytes_InvalidProtocol`** — protocol typo
  surfaces a schema error mentioning "protocol".
- **`TestValidateBytes_YAMLParseError`** — mis-indented document
  returns a parse error with `field: "document"`.
- **`TestValidateBytes_Empty`** — whitespace-only document
  reports "empty".
- **`TestValidateBytes_UnknownKind`** — `kind: Weasel` surfaces
  an "unknown kind" error while still reporting `Kind:
  "Weasel"` so the GUI can display what the user typed.
- **`TestValidateBytes_PipelineKind`** — a minimal pipeline
  parses to `Kind: "Pipeline"` with zero errors (parse-only
  mode).
- **`TestValidateBytes_JSONInput`** — a valid JSON document
  (JSON is a YAML subset) flows through the same validator with
  zero errors.
- **`TestValidateHandler_RawYAMLValid`** — POST a raw YAML body
  → 200 + `ok: true`.
- **`TestValidateHandler_RawYAMLInvalidReports200`** — validation
  failures surface as 200 + `ok: false` (not 400/422) so the
  GUI can render errors inline.
- **`TestValidateHandler_JSONWrapper`** — POST a JSON wrapper
  with `yaml` field works identically.
- **`TestValidateHandler_MethodNotAllowed`** — GET returns 405.

What stayed deferred:

- **Client-side autocomplete.** The schema file at
  `schema/mockagents-v1-agent.json` could power Monaco's IntelliSense
  if someone wants to do that slice, but the bundle-size tradeoff
  pushes it out of v0.3.
- **Save-back-to-disk.** Would need an admin-gated write path
  with directory sandboxing + filename validation + conflict
  detection. Out of scope.
- **Multi-document validation.** The endpoint validates a single
  document per request. Validating a directory's worth of YAML
  in one shot (like `make validate`) would be a nice follow-up
  for CI-in-the-browser workflows.

### 2.34 GUI v0.3 — real live feed via SSE  *(GUI + server)*

| Item                      | Location                                                 |
| ------------------------- | -------------------------------------------------------- |
| LogBroadcaster pub/sub    | `internal/server/log_broadcaster.go` `Subscribe`, `Publish`, `SubscriberCount` |
| LogWorker publish hook    | `internal/server/log_worker.go` (broadcaster wired via `LogWorkerConfig.Broadcaster`; fires after successful SQLite write) |
| `SQLiteStore.Log` ID capture | `internal/storage/sqlite.go` (populates `entry.ID` from `LastInsertId` so stream frames carry a clickable row id) |
| `GET /api/v1/logs/stream` | `internal/server/log_handlers.go` `LogHandlers.StreamLogs` (SSE handler with `event: log` frames + 15s heartbeat + cost annotation) |
| Route mount               | `internal/server/server.go` (broadcaster constructed alongside the log worker; route mounted only when a log store is configured) |
| Next.js SSE proxy         | `gui/app/api/logs/stream/route.ts` (same-origin passthrough that injects the auth cookie as a Bearer header + pipes upstream body) |
| EventSource client        | `gui/app/logs/AutoRefreshLogs.tsx` (full rewrite; capped-exponential reconnect, client-side agent filter, cap at `limit` rows) |
| Disconnected indicator    | `gui/app/globals.css` `.dot-down` (grey pill when the stream is down) |
| Tests                     | `internal/server/log_broadcaster_test.go` (7: single sub, fan-out, slow-subscriber drop, cancel-closes, nil-receiver no-op, end-to-end SSE, 503 without broadcaster) |
| Verification              | **Live**: `go test ./...` across 21 packages green; `npm run build` clean with **12 routes** (was 11) — `/api/logs/stream` added, `/logs` now 1.27 kB (was 1.16 kB) |

Closes the **GUI | real WS feed** sub-item from §6 — but the
implementation uses **server-sent events** (SSE) instead of
WebSockets. SSE is a better fit for the one-way server→client push
shape, reuses the stdlib-only transport the MCP v0.3 slice already
established (§2.33), and needs zero new dependencies on either side
of the wire.

What landed:

- **LogBroadcaster pub/sub.** New small module that owns a
  `map[*subscription]struct{}` and fans published interaction logs
  out to every subscribed channel. Each subscriber has its own
  buffered channel (default 64); a slow subscriber drops events on
  a full buffer rather than blocking `Publish`. Drop counts are
  tracked per subscriber for future metrics surfacing. Nil
  receiver is safe so `LogWorker.broadcaster.Publish(entry)` works
  whether broadcasting is enabled or not.
- **LogWorker publish hook.** After `store.Log(entry)` returns
  success, the worker calls `broadcaster.Publish(entry)`. The
  broadcaster field is nil when no live feed is configured, so the
  call is a zero-cost no-op in environments that don't need it.
  Publish happens **after** durability (matches the invariant that
  a UI row is only shown once the backend commits it) and **only**
  on success (failed writes never leak into the live feed).
- **`SQLiteStore.Log` ID capture.** The insert now pulls
  `res.LastInsertId()` into `entry.ID` so stream subscribers
  receive rows with a usable primary key — the GUI links each live
  row to `/logs/{id}` for the detail view. `LastInsertId` failures
  are non-fatal; the row is already on disk and callers still get
  a nil error.
- **`GET /api/v1/logs/stream` handler.** New SSE endpoint on
  `LogHandlers` that flushes headers, subscribes to the broadcaster,
  and writes one `event: log\ndata: <json>\n\n` frame per incoming
  entry. `:heartbeat\n\n` comments fire on a configurable ticker
  (default 15s) so idle proxies and load balancers don't reap the
  connection. When the client disconnects, `r.Context().Done()`
  fires, the subscription's cancel hook removes it from the
  broadcaster, and the channel is closed. Each frame is
  `annotate()`-wrapped so cost + usage fields are identical to the
  static `GET /api/v1/logs` list — the GUI reuses its row
  component for free.
- **Route mounting guard.** The stream route is only mounted when a
  log store is configured (and therefore a broadcaster exists).
  Environments without SQLite logging return 404 for the path,
  matching the existing `/api/v1/logs` behavior.
- **Next.js SSE proxy (`/api/logs/stream`).** Browser `EventSource`
  cannot set custom headers, so the Go backend's
  `Authorization: Bearer` requirement (in multi-tenant mode) would
  break a direct `new EventSource("http://localhost:8080/…")`
  connection. The same-origin Next.js route reads the auth cookie
  server-side via `getAuthKey()`, forwards it as a Bearer header
  on the upstream fetch, and pipes the upstream `ReadableStream`
  straight into the browser response with the correct SSE
  headers. The `req.signal → fetch` wiring propagates browser
  disconnects back to the Go server so stale subscriptions don't
  accumulate.
- **EventSource client (`AutoRefreshLogs`).** Full rewrite. The
  component now opens an `EventSource("/api/logs/stream")`,
  listens on the custom `"log"` event, JSON-parses each frame,
  and prepends the row onto the in-memory table state (capped at
  `limit` so a long-running live session can never balloon the
  page's memory). Client-side agent filter is retained — the
  stream is one shared firehose for every open tab, so filtering
  happens on the browser side. Reconnect uses capped exponential
  backoff (1s → 30s cap) with a retry counter that survives
  React StrictMode's double-mount. Last-event timestamp surfaces
  in the live bar so operators can tell "connected but idle"
  from "disconnected".
- **Disconnected indicator.** `.dot-down` joins the existing
  `.dot-live` pulse — grey when the EventSource is closed or
  retrying, green with a pulse when live. One-line CSS addition.

Design notes:

- **Why SSE and not WebSockets?** The live feed is one-way
  (server → browser), no binary payloads, no message ordering
  guarantees beyond "per-connection FIFO". SSE gives all of that
  with stdlib only — no `gorilla/websocket` or
  `coder/websocket` dependency. The original §2.26 note said a
  future WS swap would "touch about 30 lines"; the SSE swap
  touched ~40 lines of backend + ~80 lines of client (the
  reconnect logic is new regardless of transport).
- **Publish-after-durability ordering.** The worker publishes only
  after a successful SQLite commit. That means the UI cannot show
  a row that later turns out to have failed — which avoids a
  whole class of "phantom row" UX bugs. The tradeoff is that a
  row does not appear in the live feed until SQLite flushes (sub-
  millisecond on WAL mode), which is still orders of magnitude
  better than the 3-second poll it replaced.
- **Per-subscriber drop semantics.** A slow browser tab cannot
  block the hot path. We drop events for that subscriber
  specifically and track the count so operators know their
  client is falling behind. The alternative — blocking `Publish`
  on a backpressured subscriber — would convert "one slow tab"
  into "whole server slows down", which is unacceptable for a
  mock that load tests depend on.
- **Client-side filter, not server-side.** The stream is a single
  firehose for every subscriber. Filtering per-subscriber on the
  server would require either a parameterized subscription
  (more state, more surface) or an API where each subscription
  declares its filter at subscribe time (wire churn). Filtering
  on the browser is one line and keeps the server shape trivial.
- **Proxy pipes the upstream `ReadableStream` as-is.** The Next.js
  route does not re-parse or re-encode SSE frames — it just sets
  the response body to the upstream body. That keeps the proxy
  latency-free and avoids re-tokenizing SSE quirks that the Go
  handler already got right.

Regression guards:

- **`TestLogBroadcaster_SingleSubscriber`** — one publish, one
  subscribe, one deliver.
- **`TestLogBroadcaster_FanOutToMany`** — three subscribers each
  receive the same publish; `SubscriberCount` tracks the count.
- **`TestLogBroadcaster_SlowSubscriberDrops`** — a buffer-1
  subscriber cannot block the publisher; five publishes never
  deadlock.
- **`TestLogBroadcaster_CancelClosesChannel`** — cancel closes the
  returned channel and removes the subscription; post-cancel
  publish is a no-op.
- **`TestLogBroadcaster_NilReceiverIsNoop`** — nil broadcaster
  doesn't panic on `Publish` or `SubscriberCount`.
- **`TestStreamLogsEndToEnd`** — full HTTP flow: httptest server
  mounts `StreamLogs`, client connects, the LogWorker writes a
  row, the handler emits an `event: log` frame, the test decodes
  it into `LogWithCost` and asserts `ID != 0` (proving
  `LastInsertId` wiring) plus the agent name round-trips.
- **`TestStreamLogsWithoutBroadcaster503`** — nil broadcaster
  returns 503 instead of hanging or crashing.

What stayed deferred:

- **Last-Event-ID replay cursor** — a reconnecting client starts
  from "now", not from the last event id it saw. For the "watch
  what's happening" use case this is fine; for a true audit
  replay a cursor-backed history endpoint would be better.
- **Per-subscriber filter push-down** — a server-side filter
  would save wire bytes when the operator's filter is narrow
  (one agent in a thousand), but requires a parameterized
  subscribe API and isn't worth the design churn for the current
  single-process workloads.
- **`X-MCP-Pending-Notifications`-style bundled drop count** —
  slow subscribers drop rows silently; surfacing the per-sub
  drop count in an HTTP header would be a nice follow-up once
  someone hits it in anger.

### 2.33 MCP v0.3 — bidirectional transport (sampling + roots)  *(MCP)*

| Item                          | Location                                                 |
| ----------------------------- | -------------------------------------------------------- |
| Bidirectional state machine   | `internal/mcp/bidirectional.go` `bidirectional`, `OutboundMessage`, `OutboundKind` |
| `SendRequest` / correlate     | `internal/mcp/bidirectional.go` `Server.SendRequest`, `Server.DeliverResponse` |
| `Sample` / `ListRoots` sugar  | `internal/mcp/bidirectional.go` `Server.Sample`, `Server.ListRoots` |
| Subscriber lifecycle          | `internal/mcp/bidirectional.go` `Subscribe` (replay + steal + detach) |
| SSE event stream              | `internal/mcp/sse.go` `EventStreamHandler` (GET /mcp/events, 15s heartbeat) |
| Client response endpoint      | `internal/mcp/sse.go` `ResponseHandler` (POST /mcp/response) |
| Admin trigger                 | `internal/mcp/sse.go` `SendRequestHandler` (POST /mcp/sample, /mcp/roots) |
| EmitNotification bridge       | `internal/mcp/server.go` now fans notifications to both the legacy `pending` queue and the SSE subscriber |
| Tests                         | `internal/mcp/bidirectional_test.go` (9: round-trip, timeout, unknown-id, replay, steal, HTTP end-to-end, HTTP timeout, HTTP unknown id, client-disconnect cleanup) |
| Verification                  | **Live**: `go test ./internal/mcp/... -v` runs **38 tests** (was 29 → 38, +9 new); full `go test ./...` across 21 packages green |

Closes the **MCP | `sampling/createMessage` + `roots/list` (need
bidirectional transport)** row from §6. Before this slice those two
methods returned a clear "server-initiated, not supported" error —
the mock had no channel for pushing requests out to the client. v0.3
adds the push channel.

What landed:

- **SSE server→client stream.** `GET /mcp/events` opens a long-lived
  `text/event-stream` that carries server-initiated JSON-RPC
  requests and notifications. Each frame is one of
  `event: request` (with an id the client must echo in its reply)
  or `event: notification` (fire-and-forget, no id). A
  `:heartbeat\n\n` comment is written every 15 seconds so load
  balancers don't reap idle connections; tests shorten the
  interval via `EventStreamHandler.HeartbeatInterval`.
- **Client→server response channel.** `POST /mcp/response` accepts
  a JSON-RPC response body and routes it through
  `Server.DeliverResponse`, unblocking whichever `SendRequest`
  caller registered the matching id. Duplicate or unknown ids come
  back as 404 with a clear error so misbehaving clients fail fast.
- **`Server.SendRequest(ctx, method, params)`.** The new primitive
  that server code (tool handlers, admin triggers, etc.) calls to
  issue a server-initiated JSON-RPC request. It allocates a
  monotonic numeric id, enqueues the request on the bidirectional
  buffer, registers a pending-response channel, and blocks on the
  channel until either `DeliverResponse` wakes it or the context
  deadline fires. Context cancellation cleans up the pending entry
  so late replies never leak memory.
- **`Server.Sample` and `Server.ListRoots`.** Thin wrappers over
  `SendRequest` for the two specific methods that motivated the
  slice. Callers pass params as a `map[string]any` so the mock
  doesn't force a typed struct.
- **Admin trigger endpoint.** `POST /mcp/sample` and `POST
  /mcp/roots` are `SendRequestHandler`s bound to the two method
  names. Tests and black-box harnesses can drive a server-initiated
  request from outside the Go process: POST the params, the handler
  blocks on `SendRequest`, the SSE client processes the request and
  POSTs back to `/mcp/response`, and the admin endpoint returns
  with the decoded `Response`. Timeouts are tunable per-request via
  an `X-MCP-Timeout-Ms` header.
- **Subscriber lifecycle that doesn't drop work.** `Subscribe`
  replays any messages buffered before subscription, so a client
  that reconnects after a brief disconnect still sees the queued
  requests. Subscribing a second time steals the first subscriber
  (matching the "new tab wins" pattern real MCP proxies use). The
  cancel hook drains any channel-buffered messages back into the
  outbound slice so a detach-and-reconnect never loses frames.
- **Notifications bridge.** `EmitNotification` now pushes through
  both paths: the legacy `pending` slice (so existing
  POST-then-drain HTTP consumers still see
  `X-MCP-Pending-Notifications` bundles) **and** the bidirectional
  queue (so SSE subscribers receive notifications interleaved with
  server-initiated requests in the same ordered stream).

Design notes:

- **One subscriber at a time on purpose.** Fan-out to multiple
  subscribers would require either (a) a spec decision on how
  sampling responses pick a winner or (b) full pub/sub with
  per-subscriber delivery tracking. v0.3 punts on both — real MCP
  proxies "one browser tab owns the session" is the established
  pattern and we mirror it. The steal semantics are tested
  explicitly in `TestSubscribeStealsPreviousSubscriber`.
- **Numeric ids with an atomic counter.** Using `atomic.Int64` for
  id allocation means `SendRequest` is lock-free on the hot path
  except for the pending-map insert. The id is rendered as a raw
  JSON number via `strconv.FormatInt`, which is faster and
  smaller than going through `json.Marshal` for a single int.
- **No bounded timeout built into `SendRequest`.** The caller owns
  the deadline via the `ctx` argument — that matches idiomatic Go
  and lets tests use 50 ms timeouts without changing the API.
- **Late delivery never blocks.** The pending-response channels
  are buffered with capacity 1 so `DeliverResponse` never blocks,
  even if the `SendRequest` caller has already bailed on
  context cancellation. The cleanup hook removes the entry
  idempotently.

Regression guards:

- **`TestSendRequestRoundTrip`** — happy path with in-process
  subscription: request is enqueued, drained from the channel,
  response is delivered, caller unblocks with the right result.
- **`TestSendRequestTimeout`** — 50 ms deadline fires with
  `DeadlineExceeded`; a late `DeliverResponse` for the same id
  returns `no pending request` (no leak).
- **`TestDeliverResponseUnknownID`** — bogus ids return an error.
- **`TestSubscribeReplaysBufferedMessages`** — two notifications
  emitted before subscription are replayed on attach.
- **`TestSubscribeStealsPreviousSubscriber`** — second subscriber
  closes the first channel and starts receiving new messages.
- **`TestHTTPBidirectionalSampleRoundTrip`** — full HTTP flow: SSE
  subscribe → admin POST /mcp/sample → SSE client reads the frame
  → POST /mcp/response → admin call returns with the decoded
  reply. Uses a custom SSE frame reader that skips `:heartbeat`
  lines so the heartbeat cadence doesn't desync the test.
- **`TestHTTPSendRequestTimeout`** — admin POST with
  `X-MCP-Timeout-Ms: 100` and no responder returns 504.
- **`TestHTTPResponseHandlerUnknownID`** — POST /mcp/response with
  an unknown id returns 404.
- **`TestEventStreamCancelsOnClientDisconnect`** — client closes
  the stream mid-flight; server cleans up `bi.sub` so the slot is
  available for the next subscriber.

What stayed deferred:

- Multi-subscriber fan-out and per-subscriber delivery tracking
  (v0.4 or later).
- Session resumption across disconnects with a Last-Event-ID
  replay cursor — today a reconnecting client gets whatever was
  buffered at reconnect time, not a durable log.
- Client SDK helpers (Python / TS) that wrap the SSE parse loop +
  dispatcher. Users drive the endpoint directly with raw HTTP for
  now.

### 2.32 GUI v0.3 — admin auth (login + tenants + keys)  *(GUI)*

| Item                  | Location                                                     |
| --------------------- | ------------------------------------------------------------ |
| Cookie-backed auth    | `gui/lib/api.ts` `AUTH_COOKIE`, `getAuthKey`, `fetchJSON` Authorization injection |
| Server actions        | `gui/lib/auth.ts` `login`, `logout`, `getAuthStatus`         |
| Login page            | `gui/app/login/page.tsx` (form + inline error banner)        |
| Tenants admin         | `gui/app/admin/tenants/page.tsx` (list + create + delete)    |
| API keys admin        | `gui/app/admin/tenants/[id]/page.tsx` (mint + role change + delete + once-only plaintext reveal) |
| New api.ts methods    | `listTenants`, `createTenant`, `deleteTenant`, `listAPIKeys`, `createAPIKey`, `updateAPIKeyRole`, `deleteAPIKey`, `probeTenants` |
| Header auth pill      | `gui/app/layout.tsx` `AuthPill` (sign-in link or prefix + sign-out form) |
| Styles                | `gui/app/globals.css` (btn variants, data-table, inline-form, login-wrap, banner-ok, plaintext-box, pill-muted) |
| Verification          | **Live**: `npm run build` runs `tsc --strict` and produces a clean Next.js 15 build with **11 routes** (was 8 in v0.2) — 102 KB shared, 3 new admin routes at 106 KB each |

Closes the **GUI v0.3 | admin auth UI** sub-item from §6. Operators
running MockAgents in multi-tenant mode
(`MOCKAGENTS_MULTI_TENANT=1`) can now sign in with a pasted API key
and manage tenants + API keys from the browser — no more hand-
crafted curl calls against `/api/v1/tenants`.

What landed:

- **Cookie-backed session.** `/login` is a server component with a
  form that posts to a `login()` server action in `gui/lib/auth.ts`.
  The action probes `/api/v1/tenants` (admin-gated at the middleware
  level, so a 200 proves both validity AND admin role) and persists
  the key in an HttpOnly `mockagents_api_key` cookie on success. A
  parallel `mockagents_role` cookie stores the inferred role so the
  header pill can render it. On auth failure the action redirects
  back to `/login?error=…` and the page surfaces the reason inline.
- **Transparent header injection.** `fetchJSON` in `api.ts` reads
  the cookie via `await cookies()` (Next 15's async API) and
  forwards it as `Authorization: Bearer <key>` on every management
  API request. Single-tenant deployments set no cookie and every
  helper passes through anonymously, so the GUI keeps working
  without login.
- **Tenants list.** `/admin/tenants` renders one row per tenant with
  inline create (form action → `createTenant` → `revalidatePath`)
  and delete. Error messages from the API come back through
  `APIError` and are surfaced in a red banner at the top of the
  page. Unauthenticated visits redirect to
  `/login?next=/admin/tenants`.
- **API keys page.** `/admin/tenants/[id]` renders one row per key
  with an inline role dropdown that patches to
  `/api/v1/keys/{id}`, a Delete button, and a Mint-key form. Newly
  minted keys show their plaintext **exactly once** in a prominent
  "copy now — never shown again" banner (matches the CLI
  `NewAPIKeyResult.Plaintext` semantics). After the first render
  the plaintext drops out of the URL and is not recoverable.
- **Header UX.** The right side of the header now holds two pills:
  the existing health pill and a new `AuthPill`. Unauthenticated
  shows a "sign in" link; authenticated shows the first 8
  characters of the token plus an inline "sign out" form that
  posts to a `logout` server action and redirects to `/login`.
- **New API helpers.** `api.ts` gained `listTenants`,
  `createTenant`, `deleteTenant`, `listAPIKeys`, `createAPIKey`,
  `updateAPIKeyRole`, `deleteAPIKey`, plus a dedicated `probeTenants`
  for the login flow (the only helper that accepts an explicit
  `authKey` parameter — it has to validate a key *before* the
  cookie exists).
- **Typed tenancy shapes.** `Role`, `Tenant`, `APIKey`, and
  `NewAPIKeyResult` match the server-side JSON one-for-one so the
  tables don't `any`-cast.
- **Styling.** `globals.css` grew a small design system slice:
  `.btn` / `.btn-primary` / `.btn-danger` / `.btn-xsmall`,
  `.data-table`, `.inline-form`, `.login-wrap`, `.banner-ok`,
  `.plaintext-box`, `.pill-muted`. No new CSS-in-JS libraries — the
  GUI still ships pure CSS.

Design notes:

- **Why probe /api/v1/tenants instead of /api/v1/health?** Health is
  always open (load balancer path) so a 200 proves nothing. Tenants
  is admin-gated, so a 200 proves both "valid key" AND "admin
  role". Failing early with a clear message is better than letting
  the operator log in with a viewer key and then discover the admin
  pages are broken.
- **HttpOnly cookie, not localStorage.** A future slice might embed
  user-generated content (agent descriptions, scenario names)
  directly in the GUI, which opens a path to XSS. HttpOnly means
  even a successful XSS cannot exfiltrate the token. The cost is
  that client components can't read it — but the entire v0.3 admin
  surface is server components, so the constraint doesn't bite.
- **Server actions everywhere.** Every mutation (create tenant,
  delete key, change role, logout) is a Next.js server action, not
  a client-side fetch. That keeps the token flow 100% server-side
  and means the form submits work without JavaScript enabled.
- **`revalidatePath` after every mutation** so the tenant/key list
  reflects the change immediately — no stale SSR snapshots.

What stayed deferred to future GUI v0.3 slices:

- Workflow editor for `kind: Pipeline` documents.
- Schema-aware YAML editor for agent configs.
- Real WebSocket live feed (the `AutoRefreshLogs` polling island is
  still the current shape).
- Per-user login (no user table on the server; the key *is* the
  identity).

### 2.31 Go SDK streaming + in-process engine mode  *(SDK — Go)*

| Item                  | Location                                                     |
| --------------------- | ------------------------------------------------------------ |
| `StreamChunk` type    | `sdk/go/mockagents/streaming.go`                             |
| SSE frame parser      | `sdk/go/mockagents/streaming.go` `parseSSEFrame`, `splitSSEFrames` |
| `ChatStream`          | `sdk/go/mockagents/streaming.go` (OpenAI raw events)         |
| `MessageStream`       | `sdk/go/mockagents/streaming.go` (Anthropic raw events)      |
| `IterStream`          | `sdk/go/mockagents/streaming.go` (protocol-agnostic chunks)  |
| Normalizers           | `sdk/go/mockagents/streaming.go` `normalizeOpenAIStream`, `normalizeAnthropicStream` |
| `RawEventStream` iter | `sdk/go/mockagents/streaming.go` (bufio.Scanner-style Next/Value/Err/Close) |
| `ChunkStream` iter    | `sdk/go/mockagents/streaming.go` (same surface, yields `StreamChunk`) |
| In-process client     | `sdk/go/mockagents/inprocess.go` `NewInProcessClient`, `InProcessOptions`, `InProcessClient` |
| Tests                 | `sdk/go/mockagents/streaming_test.go` (12), `inprocess_test.go` (5) |
| Verification          | **Live**: `go test ./sdk/go/mockagents/...` runs **33 tests** (was 16 → 33, +17 new) all green; full `go test ./...` across 21 packages green |

Closes the **Go SDK | Streaming helpers, in-process engine mode**
row from §6. Mirrors the Python §2.25 and TypeScript §2.28 streaming
surfaces exactly so example code translates one-to-one across all
three SDKs.

New surface area:

- **`ChatStream(ctx, messages, opts)`** — opens an OpenAI
  `/v1/chat/completions` SSE stream and returns a `*RawEventStream`.
  Consumers use the `bufio.Scanner` idiom: `for s.Next() { ev := s.Value(); … }`
  then `s.Err()` / `s.Close()`. Terminates on the `[DONE]` sentinel.
- **`MessageStream(ctx, messages, opts)`** — mirrors ChatStream for
  the Anthropic Messages wire format. Yields `message_start` /
  `content_block_*` / `message_delta` / `message_stop` events and
  stops cleanly after `message_stop`.
- **`IterStream(ctx, messages, opts)`** — higher-level entry point
  that picks the wire format via `opts.Protocol` ("openai" default,
  or "anthropic") and returns a `*ChunkStream` yielding
  protocol-agnostic `StreamChunk` values. User code becomes:

      stream, err := client.IterStream(ctx, messages, mockagents.IterStreamOptions{Protocol: "anthropic"})
      for stream.Next() {
          chunk := stream.Value()
          fmt.Print(chunk.Text)
          if chunk.Finished { break }
      }

- **`StreamChunk`** — same shape as the Python dataclass and TS
  interface: `Text`, `ToolCallDelta` (index+name+fragment), `FinishReason`,
  `Finished`, `Raw`. Padding chunks (empty text, no tool delta, not
  finished) are dropped by `normalizeOpenAIStream` so consumers never
  have to filter noise.
- **In-process mode (`NewInProcessClient`)** — loads YAML agents from
  a directory, builds an `engine.Engine` + registry + in-memory state
  store in the current process, wires them into an `httptest.Server`
  mounting the existing `adapter.OpenAIHandler` / `adapter.AnthropicHandler`
  / health route, and returns an `*InProcessClient` that embeds the
  standard `*Client`. The subprocess binary is never spawned, no free
  port is negotiated, and startup is sub-millisecond — downstream Go
  users can spin up thousands of client instances in a test run.
  `Close()` tears down the test server; `BaseURL()` exposes the URL
  so callers can point a third-party SDK (`openai-go`, etc.) at the
  same mock without touching the embedded client.

Design notes:

- **Scanner-based SSE pump.** `splitSSEFrames` is a `bufio.SplitFunc`
  that yields one complete frame (up to `\n\n` or `\r\n\r\n`) per
  `Scan()`, with an EOF tail-drain so servers that omit the trailing
  blank line still work. The scanner buffer ceiling is raised to
  1 MiB — pathological chunks that exceed the 64 KiB default would
  otherwise silently truncate.
- **Per-request cancellation, no HTTP client timeout.** SSE clients
  must not apply a read timeout (the stream is long-lived by design),
  so `requestSSE` clones the underlying `http.Client` with `Timeout: 0`
  and uses a per-request `context.WithCancel` that `Close()` fires.
- **State-carrying Anthropic normalizer.** `normalizeAnthropicStream`
  remembers the most recent tool-use index + name on a
  `normalizeState` value so `input_json_delta` fragments from later
  `content_block_delta` events line up with the right tool call.
  `message_delta` buffers `stop_reason` so the terminal
  `message_stop` chunk carries it; falls back to `"end_turn"` when
  the server omits the reason.
- **No new dependencies.** Uses stdlib only (`bufio`, `bytes`,
  `net/http`, `net/http/httptest`, `encoding/json`). The in-process
  mode reuses the existing `adapter` handlers, so any future
  handler-level change (auth, chaos, etc.) is automatically picked
  up by tests that use `InProcessClient`.

Regression guards:

- **`TestParseSSEFrame`** — pins the SSE spec quirks: comments,
  multi-line data joining, leading-space stripping, CRLF tolerance.
- **`TestNormalizeOpenAIStream*`** — padding suppression, text,
  tool-call delta (index/name/fragments), and finish-reason chunks.
- **`TestNormalizeAnthropicStream*`** — tool_use open, input_json_delta
  fragment routing, text_delta, message_delta stop-reason buffering,
  and the default `end_turn` fallback.
- **`TestChatStreamEndToEnd`** — fake `httptest` SSE server feeds
  three frames + `[DONE]`; the client returns exactly three events.
- **`TestIterStreamOpenAIAssemblesText`** — verifies that chunks
  assembled across the stream boundary reassemble to the expected
  text and that the terminal chunk is marked `Finished`.
- **`TestMessageStreamStopsOnMessageStop`** — verifies a stray event
  after `message_stop` is never surfaced.
- **`TestIterStreamAnthropicEndToEnd`** — full Anthropic path through
  `IterStream` with `Protocol: "anthropic"`.
- **`TestIterStreamUnknownProtocol`** — error path for a bogus
  protocol value.
- **`TestChatStreamHTTPErrorSurfacesAsHTTPError`** — 400 response
  body comes back wrapped in `*HTTPError` so callers can type-assert.
- **`TestInProcessClient*`** — exercises Chat, Health, and the three
  error paths (missing dir, empty `AgentsDir`, empty directory).

### 2.30 Tenant-scoped agent isolation  *(multi-tenancy)*

| Item                        | Location                                                 |
| --------------------------- | -------------------------------------------------------- |
| Metadata.TenantID           | `internal/types/agent.go`                                |
| Tenant context helpers      | `internal/engine/reqmeta.go` `WithTenantID`, `TenantIDFromContext` |
| Registry visibility methods | `internal/engine/agent_registry.go` `GetForTenant`, `GetByModelForTenant`, `ListForTenant`, `ListNamesForTenant`, `visibleTo` |
| Engine resolve              | `internal/engine/engine.go` `resolveAgentForTenant`      |
| Adapter wiring              | `internal/adapter/{openai,anthropic}.go` honor `X-Mockagents-Tenant` header and call `ProcessRequestContext` |
| Server handlers             | `internal/server/handlers.go` `callerTenantID`, `ListAgents`, `GetAgent`, `ReloadAgent` |
| Tests                       | `internal/engine/tenant_test.go` (8)                     |
| Verification                | **Live**: registry visibility tests cover global + scoped + cross-tenant cases; engine integration tests cover successful tenant lookup, cross-tenant 404, and anonymous fallback; full Go suite (21 packages) green and **fully backward compatible** with single-tenant deployments |

Closes the **Multi-tenancy | Tenant-scoped agent data isolation** row
from §6 with a non-breaking visibility filter on top of the v0.1
single-tenant registry.

Design:

- **`Metadata.TenantID` is the ownership marker.** Empty string =
  "global" agent visible to every caller (the v0.1 default behavior).
  A non-empty value declares the agent as private to that tenant.
- **Visibility-only model.** The registry still keys agents by name
  (not by `(tenant, name)`), so name collisions across tenants are
  not supported within a single MockAgents process. This is the
  right tradeoff for a single-process mock — real multi-tenancy at
  scale uses one process per tenant or moves to the deferred
  Postgres slice.
- **Two lookup layers.** Control-plane handlers
  (`/api/v1/agents`, `/api/v1/agents/{name}`,
  `/api/v1/agents/{name}/reload`) read the authenticated principal
  via `tenancy.PrincipalFrom(r.Context())` and pass `TenantID` into
  the new `*ForTenant` registry methods. LLM endpoints
  (`/v1/chat/completions`, `/v1/messages`) honor an opt-in
  `X-Mockagents-Tenant: <id>` header so v0.1 clients that don't
  send the header keep their global-only behavior — no breaking
  change to the OpenAI/Anthropic drop-in promise.
- **Engine context plumbing.** A new
  `engine.WithTenantID(ctx, id)` / `engine.TenantIDFromContext(ctx)`
  pair carries the tenant id from the HTTP layer down to the
  registry without importing the `tenancy` package (which would
  cycle: `tenancy → engine → tenancy`). The engine package stays
  cycle-free of every other package.
- **Conservative resolve fallback.** The "if only one agent is
  visible, use it as default" convenience still fires for anonymous
  callers (preserves single-agent demos) but is disabled for
  tenant-bound callers — better to fail with a clear "not found"
  than silently land on the wrong agent.
- **404 not 403.** Cross-tenant `GetAgent` returns 404 to avoid
  leaking the existence of foreign agent names through a
  permissions error.

Tests cover: registry global-vs-scoped visibility (`GetForTenant`,
`GetByModelForTenant` with model-name collision, `ListForTenant`),
engine integration (successful own-tenant lookup, cross-tenant
denial, anonymous fallback to global), and the
`WithTenantID`/`TenantIDFromContext` round trip.

### 2.29 MCP v0.2 — completion, logging, notifications  *(MCP)*

| Item                       | Location                                                 |
| -------------------------- | -------------------------------------------------------- |
| `MCPCompletion` config     | `internal/types/mcp.go` `MCPCompletion`, `MCPServerSpec.Completions` |
| Server state + queue       | `internal/mcp/server.go` `Server.logLvl`, `Server.pending`, `Notification` |
| New API                    | `internal/mcp/server.go` `EmitNotification`, `DrainNotifications`, `LogLevel` |
| `completion/complete`      | `internal/mcp/server.go` `handleCompletionComplete` (catalog lookup + prefix filter + 100-item cap) |
| `logging/setLevel`         | `internal/mcp/server.go` `handleLoggingSetLevel` (validates against syslog levels) |
| `sampling`/`roots` errors  | `internal/mcp/server.go` `Handle` (clear "server-initiated, not supported" message) |
| HTTP transport bundling    | `internal/mcp/http.go` `HTTPHandler.ServeHTTP` (response + notifications envelope, `X-MCP-Pending-Notifications` header) |
| Admin notify endpoint      | `internal/mcp/http.go` `NotifyHandler` (`POST /mcp/notify`) |
| Tests                      | `internal/mcp/v02_test.go` (14)                          |
| Verification               | **Live**: 5 completion tests + 2 logging tests + 2 sampling/roots tests + 5 notification queue & transport tests; total 29 MCP tests in the package now |

Closes the **MCP | Streaming notifications, completion/complete,
sampling, roots** row from §6. Three of the four §6 items land
fully; the two server→client methods (`sampling/createMessage` and
`roots/list`) return a clear "requires bidirectional transport"
error with a hint pointing at `EmitNotification` / `POST /mcp/notify`
as the workaround for tests that need to drive the client side.

What landed:

- **`completion/complete`** — config-driven autocomplete catalog.
  Each `MCPCompletion` entry binds an `(refType, refName, argName)`
  triple to a static value list; empty fields wildcard-match so a
  single entry can serve every prompt that uses the same argument.
  Non-empty `argument.value` filters results by case-insensitive
  prefix, matching the "narrow as you type" UX of a real
  autocomplete server. Spec-mandated 100-result cap with `hasMore`
  flag.
- **`logging/setLevel`** — validates against the standard syslog
  level set (`debug` / `info` / `notice` / `warning` / `error` /
  `critical` / `alert` / `emergency`), records the value on the
  server, and exposes it via `Server.LogLevel()` for tests. The
  mock has no internal log output to filter — the level is observed
  but not acted on.
- **Server-initiated notifications** — `Server.EmitNotification`
  enqueues a JSON-RPC notification (no id) for the next transport
  drain, and `Server.DrainNotifications` clears + returns the queue
  atomically. The HTTP transport drains after every request and
  bundles the notifications into an envelope alongside the response
  body, signaling the count via `X-MCP-Pending-Notifications`.
  Notification-only requests (no id, no response) emit a JSON
  array body when notifications are pending instead of the usual
  204 No Content.
- **Admin notify endpoint** — `NotifyHandler` exposes a tiny
  `POST /mcp/notify` admin route that test harnesses can hit from
  outside the process to drive the queue. Useful for verifying
  client conformance against `notifications/tools/list_changed` and
  similar list-change events without instrumenting the server.
- **`sampling/createMessage` and `roots/list`** — both server→client
  methods in real MCP. The mock returns
  `ErrMethodNotFound` with the message `"method ... is
  server-initiated and not supported by the mock without a
  bidirectional transport"` and a `hint` pointing at
  `EmitNotification` / `POST /mcp/notify`. Documented as a
  known limitation in the slice notes; full bidirectional support
  would need a streaming transport (WebSocket or SSE), which is
  its own slice.

### 2.28 TypeScript SDK streaming  *(SDK — Anthropic + unified)*

| Item                  | Location                                              |
| --------------------- | ----------------------------------------------------- |
| `StreamChunk` type    | `sdk/typescript/src/types.ts`                         |
| `chatStream`          | `sdk/typescript/src/client.ts` (OpenAI raw events)    |
| `messageStream`       | `sdk/typescript/src/client.ts` (Anthropic raw events) |
| `iterStream`          | `sdk/typescript/src/client.ts` (protocol-agnostic StreamChunks) |
| SSE parser            | `sdk/typescript/src/client.ts` `requestSSE`, `parseSSEFrame` |
| Normalizers           | `sdk/typescript/src/client.ts` `normalizeOpenAIStream`, `normalizeAnthropicStream` |
| Index re-export       | `sdk/typescript/src/index.ts`                         |
| Tests                 | `sdk/typescript/tests/streaming.test.ts` (13)         |
| Verification          | **Live**: `npm run build` clean under tsc strict; `npm test` runs **38 tests** (was 25 → 38, +13) all green |

Closes the **TypeScript SDK | Streaming helper, WebSocket/SSE
integration** row from §6. Mirrors the Python SDK §2.25 surface
exactly so the two SDKs stay in lockstep.

The SSE parser is a small standalone helper (`parseSSEFrame` —
exported for tests) that respects the spec quirks: comment lines
starting with `:` are skipped, multiple `data:` lines in one frame
are joined with `\n`, and the leading space after the colon is
stripped. The transport layer (`requestSSE`) reads the `fetch`
response stream into a chunked decoder, splits on the SSE blank-
line frame boundary, drains the trailing partial frame on EOF, and
releases the reader lock in a finally block so a slow client never
leaks the underlying socket.

`StreamChunk` matches the Python `StreamChunk` shape one-for-one
(text delta, optional `[index, name, fragment]` tool-call triple,
finish reason, finished flag, raw event). The `iterStream` entry
point picks the wire format via `protocol: "openai" | "anthropic"`
so user code can write the same loop for both providers and switch
by changing one keyword.

### 2.27 Helm chart v0.2 — HPA, PDB, NetworkPolicy, ServiceMonitor  *(Helm)*

| Item                  | Location                                                  |
| --------------------- | --------------------------------------------------------- |
| Chart version bump    | `deploy/helm/mockagents/Chart.yaml` (0.1.0 → 0.2.0)       |
| Values blocks         | `deploy/helm/mockagents/values.yaml` (`autoscaling`, `podDisruptionBudget`, `networkPolicy`, `serviceMonitor`) |
| HPA template          | `deploy/helm/mockagents/templates/hpa.yaml`               |
| PDB template          | `deploy/helm/mockagents/templates/pdb.yaml`               |
| NetworkPolicy template| `deploy/helm/mockagents/templates/networkpolicy.yaml`     |
| ServiceMonitor        | `deploy/helm/mockagents/templates/servicemonitor.yaml`    |
| Verification          | **Live**: `helm lint` clean; `helm template ... --set autoscaling.enabled=true,podDisruptionBudget.enabled=true,networkPolicy.enabled=true,serviceMonitor.enabled=true` renders 10 resources; defaults still render the same 6 resources as v0.1 |

Closes the **Helm | HPA, NetworkPolicy, Prometheus ServiceMonitor**
row from §6. All four new templates are **off by default** so
upgrading from chart 0.1.0 → 0.2.0 is a no-op for existing users
— the new resources only render when their `enabled` flag is
flipped, preserving backward compatibility with every running
deployment.

What landed:

- **HorizontalPodAutoscaler** (`autoscaling.*`) on the
  `autoscaling/v2` API. Targets CPU utilization by default, with
  optional memory utilization, configurable min/max replicas, and
  a passthrough `behavior` block for stabilization windows. Skips
  metrics with empty target values so a CPU-only HPA renders
  cleanly.
- **PodDisruptionBudget** (`podDisruptionBudget.*`) on
  `policy/v1`. Off by default because the chart's default
  `replicaCount` is 1 — a PDB with `minAvailable: 1` would block
  drains entirely. Operators flip it on once they raise replicas
  or enable autoscaling.
- **NetworkPolicy** (`networkPolicy.*`) locking down ingress + egress
  via standard `networking.k8s.io/v1` rules. Three knobs:
  `allowSameNamespace` (default true), `allowExternalIngress` for
  cluster-wide Ingress controllers in another namespace, and
  user-supplied `ingressFrom` / `egressRules` arrays. DNS egress
  is allowed by default via `allowDNS`.
- **ServiceMonitor** (`serviceMonitor.*`) for Prometheus Operator
  scraping. Uses the named `http` port from the existing Service,
  defaults to a 30s interval / 10s timeout, and forwards
  user-supplied `relabelings` / `metricRelabelings` /
  extra `labels`.

Defaults render the same 6 resources as v0.1 (ServiceAccount,
ConfigMap, Service, Deployment, Ingress when enabled, helm-test
Pod). Flipping all four v0.2 knobs on adds HPA, PDB, NetworkPolicy,
ServiceMonitor for a 10-resource render.

### 2.26 GUI v0.2 — costs, audit, log detail, live feed  *(GUI)*

| Item                  | Location                                                     |
| --------------------- | ------------------------------------------------------------ |
| API client extensions | `gui/lib/api.ts` — `getCosts`, `listAudit`, `getLog`, log filters |
| Same-origin proxy     | `gui/app/api/logs/route.ts` (forwards to `/api/v1/logs` for the live-feed client) |
| Costs page            | `gui/app/costs/page.tsx` (totals + by-model + by-agent tables) |
| Audit page            | `gui/app/audit/page.tsx` (events table with categorized badges, friendly 401/403) |
| Log detail page       | `gui/app/logs/[id]/page.tsx` (request/response bodies, latency, cost) |
| Logs filter form      | `gui/app/logs/page.tsx` (agent select, since input, limit, live toggle) |
| Live-feed client      | `gui/app/logs/AutoRefreshLogs.tsx` (3 s polling, no overlapping fetches, error pill) |
| Static logs table     | `gui/app/logs/LogsTable.tsx` (extracted from page.tsx — Next.js page files cannot export non-default symbols) |
| Header navigation     | `gui/app/layout.tsx` (Costs + Audit links, version bumped to v0.2) |
| Styles                | `gui/app/globals.css` (filters, live-bar pulse, cost tiles, meta-grid, JSON block, audit badges) |
| README                | `gui/README.md` rewritten for v0.2 with explicit deferred-items list |
| Verification          | **Live**: `npm run build` produces a clean Next.js 15 build with all 8 routes (was 4 in v0.1) under tsc strict mode; first-load JS 102 KB shared, +5 KB only on `/logs` (the live-feed island) |

Closes the **GUI v0.2 partial** row from §6 with a focused
"observability" slice. The GUI is still a single-process dev/ops
tool — no auth flows, no global stores, no build-time data.

What landed:

- **Cost surface.** `/costs` aggregates `/api/v1/costs` into total
  requests / prompt + completion tokens / USD plus by-model and
  by-agent tables sorted by cost descending. Returns null on 503
  (logging disabled) so the page can show a friendly empty state
  instead of crashing.
- **Audit surface.** `/audit` renders the 200 most recent events
  from `/api/v1/audit` with categorized badges (`auth.*` warns,
  `tenant.*` / `api_key.*` info, `agent.reloaded` ok). Details
  blobs are pretty-printed inline as `key=value` pairs. When
  multi-tenant mode is on the endpoint requires the admin role —
  the page handles 401/403 with a "needs admin token" notice
  instead of throwing.
- **Log detail.** `/logs/[id]` shows the full request/response
  bodies (pretty-printed when the body is JSON, raw otherwise),
  method/path, status, latency, scenario match, model, token
  counts, and per-row cost. Hands you a back link to the filtered
  list view.
- **Filters on /logs.** Server-component form with agent select,
  RFC3339 `since` input, limit dropdown (25–250), and a "Live"
  checkbox. State lives in URL query params so a filtered view is
  shareable and survives a refresh.
- **Live feed via polling.** `?live=1` swaps the static SSR table
  for `AutoRefreshLogs`, a client island that polls the same-origin
  `/api/logs` proxy every 3 seconds. The proxy
  (`gui/app/api/logs/route.ts`) forwards to the upstream
  MockAgents API server-side, so the browser never needs to know
  the upstream URL or worry about CORS. Overlapping fetches are
  prevented via an `inFlight` ref so a slow upstream cannot stack
  pollers. Errors surface in the live bar without dropping the
  table.
- **Header navigation.** Two new links (Costs, Audit) and the
  version label moved to v0.2.

Why polling instead of a real WebSocket: the MockAgents server has
no WS endpoint today and adding one is its own slice. A 3-second
poll over the existing REST surface is responsive enough for the
"watch what's happening" use case, and the
`AutoRefreshLogs`/`/api/logs` boundary is small enough that a
future swap to WS would touch about 30 lines.

What stayed deferred (now the explicit GUI v0.3 backlog):

- Workflow editor for `kind: Pipeline` documents.
- Schema-aware YAML editor for agent configs.
- Authentication UI (admin login, mint API keys from the browser).
- WebSocket live feed (real subscription, not polling).
- Component-level unit tests (today the TS strict pass at build
  time covers the type surface end-to-end).

### 2.25 Python SDK streaming helper parity  *(SDK — Anthropic + unified)*

| Item                  | Location                                              |
| --------------------- | ----------------------------------------------------- |
| `StreamChunk` type    | `sdk/python/mockagents/types.py`                      |
| `message_stream`      | `sdk/python/mockagents/client.py` `MockAgentClient.message_stream` |
| `iter_stream`         | `sdk/python/mockagents/client.py` `MockAgentClient.iter_stream` |
| Normalizers           | `sdk/python/mockagents/client.py` `_normalize_openai_stream`, `_normalize_anthropic_stream` |
| Tests                 | `sdk/python/tests/test_streaming.py` (14)             |
| Verification          | **Live**: `python -m pytest tests/` runs **90 tests** (76 → 90, +14 new) all green |

Closes the **Python SDK | Streaming helper parity with chat_stream**
row from §6. Before this slice the Python SDK exposed
`chat_stream` for OpenAI but had no Anthropic equivalent — users
had to call `message(stream=True)` and wait for the parser to
collapse the entire stream into a single `ChatResponse`, losing
the incremental delivery that streaming exists for.

New surface area:

- **`message_stream()`** — mirrors `chat_stream()` for the Anthropic
  Messages wire format. Yields parsed event dicts
  (`message_start`, `content_block_start`, `content_block_delta`,
  `content_block_stop`, `message_delta`, `message_stop`) and stops
  cleanly on `message_stop`. Robust against malformed `data:` lines
  (skipped) and stray events past terminal (ignored).
- **`StreamChunk` dataclass** — protocol-agnostic chunk shape with
  `text`, `tool_call_delta` (an `(index, name, arguments_fragment)`
  triple), `finish_reason`, `finished`, and `raw`. Equality and
  default values are exercised by tests.
- **`iter_stream(messages, protocol=...)`** — the higher-level entry
  point that picks the wire format and yields `StreamChunk`s. User
  code can write::

      for chunk in client.iter_stream(messages, protocol="anthropic"):
          print(chunk.text, end="", flush=True)

  …and switch protocols by changing one keyword argument.
- **Module-level normalizers** (`_normalize_openai_stream`,
  `_normalize_anthropic_stream`) — kept private but exercised
  directly in tests so the wire-format-to-`StreamChunk` conversion
  is decoupled from the HTTP layer. Also: padding chunks (no text,
  no tool delta, no finish) are dropped from OpenAI streams so
  consumers don't have to filter empty deltas themselves.

Tests cover: dataclass defaults + equality, OpenAI text-only
streams, OpenAI padding-chunk filtering, OpenAI tool-call delta
accumulation, Anthropic text-only streams, Anthropic
`tool_use` + `input_json_delta` accumulation, post-`message_stop`
event suppression, the `message_stream` HTTP path with mocked
`requests.Session` (text + early-stop + malformed-data), the
end-to-end `iter_stream` for both protocols, and the unknown-
protocol error path.

`StreamChunk` is also re-exported from the package root so
``from mockagents import StreamChunk`` works alongside
``ChatResponse``.

### 2.24 Adapter JSON decode buffer pool  *(perf — request decode)*

| Item                  | Location                                                       |
| --------------------- | -------------------------------------------------------------- |
| Pool + helper         | `internal/adapter/decode.go` `decodeBufPool`, `decodeJSONBody` |
| OpenAI integration    | `internal/adapter/openai.go` `HandleChatCompletions`           |
| Anthropic integration | `internal/adapter/anthropic.go` `HandleMessages`               |
| Tests                 | `internal/adapter/decode_test.go` (3 unit tests + 2 benchmarks)|
| Verification          | **Live**: side-by-side benchmark on identical hardware shows the pooled helper at **1257 ns/op / 1177 B/op / 18 allocs/op** vs the previous `json.NewDecoder(r.Body).Decode` at **1386 ns/op / 1920 B/op / 22 allocs/op** — **9.3% faster, 39% less memory, 18% fewer allocations** per request decode |

Closes recommendation **#6** from the performance review. The
adapter handlers previously decoded incoming requests with
`json.NewDecoder(r.Body).Decode(&req)`, which streams from the
request body into an internal scratch buffer that is allocated
fresh on every call. The new `decodeJSONBody` helper drains the
body once into a pooled `bytes.Buffer` and hands the resulting
slice to `json.Unmarshal`, eliminating both the per-request
decoder struct and its hidden scratch allocation.

Design:

- **`sync.Pool` of `*bytes.Buffer`.** Each adapter call pulls a
  buffer, calls `Reset` (preserving the backing array), reads the
  whole body via `ReadFrom`, and unmarshals the resulting slice.
  The buffer goes back to the pool on defer.
- **Oversized-buffer guard.** Any buffer whose `Cap()` grew beyond
  `maxPooledBodyBufBytes` (1 MiB) is dropped instead of returned
  to the pool, so a single attacker probing limits cannot turn
  the pool into a permanent memory high-water mark. Steady-state
  workloads (OpenAI/Anthropic SDK requests run a few hundred bytes
  to a few KB) reuse the same backing array indefinitely.
- **Behavior parity with NewDecoder.** Both call sites previously
  used the default Decoder configuration (no `UseNumber`, no
  `DisallowUnknownFields`), and `json.Unmarshal` produces
  identical results for those settings. The
  `TestDecodeJSONBody_RoundTrip` test pins this parity for the
  shapes the adapters actually decode.

Regression guards:

- **`TestDecodeJSONBody_RoundTrip`** — sub-tests for minimal and
  streaming-flag bodies, asserting field-level equality with the
  expected struct.
- **`TestDecodeJSONBody_PoolReuseIsSafe`** — fires 100 sequential
  decodes with variable-length model names (e.g. `mxxxx...`) and
  asserts every iteration parses its own model exactly. Catches
  the classic sync.Pool failure mode of "previous request's bytes
  bleed into this one".
- **`TestDecodeJSONBody_MalformedReturnsError`** — invalid JSON
  must surface as an error, not silently produce a zero struct.
- **`BenchmarkDecodeJSONBody_Pooled`** + **`_StreamingDecoder`** —
  side-by-side benchmarks ship in the test file so any future
  regression (or a re-evaluation against `sonic`/`jsoniter`) can
  rerun `go test -bench` and read the delta directly.

### 2.23 captureWriter pool  *(perf — GC pressure)*

| Item                  | Location                                                  |
| --------------------- | --------------------------------------------------------- |
| Pool + helpers        | `internal/server/log_handlers.go` `captureWriterPool`, `acquireCaptureWriter`, `releaseCaptureWriter` |
| Middleware integration| `internal/server/log_handlers.go` `InteractionCapture` — `defer releaseCaptureWriter(cw)` |
| Test                  | `internal/server/interaction_capture_test.go` `TestCaptureWriterPool_ReusedCleanly` |
| Verification          | **Live**: 50 rapid-fire requests through a single middleware chain produced 50 distinct, non-overlapping rows — proof the pool does not bleed body state between requests |

Closes recommendation **#5** from the performance review, the last
open item in the perf-review backlog. Before this slice the
interaction-capture middleware allocated a fresh `captureWriter`
struct plus a zero-length `body` slice on every request, and the
body grew via `append` as the handler wrote chunks. At sustained
load the per-request struct + slice became noticeable GC churn
alongside the async log worker (§2.20) that drained the snapshots.

Design:

- **`sync.Pool` of `*captureWriter`** — each request goes through
  `acquireCaptureWriter(w)` which resets `statusCode`, `capture`,
  `truncated`, and crucially `body = body[:0]` (zero length, same
  backing array). When the middleware returns, `defer
  releaseCaptureWriter(cw)` clears the embedded `ResponseWriter`
  pointer (so a stale `http.ResponseWriter` cannot be pinned in
  the pool for the lifetime of the process) and returns the struct
  to the pool.
- **Body backing-array reuse.** The common case (small sub-KB
  responses) keeps the slice so the next request simply writes
  into the already-sized array. This is the headline win: the
  `append(body[:0], ...)` pattern grows only as far as needed
  without reallocating on every request.
- **Pathological-response cap.** If any request's body cap grows
  beyond `maxCaptureBodyBytes/4` (256 KiB) the slice is dropped
  before returning to the pool, so a single large-response
  outlier cannot turn the pool into a per-process memory high-
  water mark.
- **Safe against `snapshot()` lifetime.** The async log worker
  only ever reads `cw.body` via `snapshot()`, which already
  produces a defensive copy the worker owns. By the time
  `releaseCaptureWriter` runs, the snapshot is already safely
  handed off to `worker.Submit(entry)` and the `cw.body` slice
  has no remaining external references.

Regression guard:

- **`TestCaptureWriterPool_ReusedCleanly`** — fires 50 sequential
  requests through the middleware, each handler writing
  variable-length JSON with a distinct `model` marker. Every
  persisted row must (a) be valid JSON, (b) contain exactly its
  own request's model string, and (c) produce 50 distinct models
  with no duplicates. If a pool cleanup bug ever forgets to reset
  `body`, this test will see tail bleed from the previous request
  and fail.

### 2.22 SQLite multi-conn pool + synchronous=NORMAL  *(perf — concurrency)*

| Item                     | Location                                              |
| ------------------------ | ----------------------------------------------------- |
| Log store pool + pragmas | `internal/storage/sqlite.go` `NewSQLiteStore`         |
| Audit store pool         | `internal/audit/store.go` `NewSQLiteStore` (pool raised, redundant mutex dropped) |
| Log concurrency test     | `internal/storage/concurrent_test.go` (2)             |
| Audit concurrency test   | `internal/audit/audit_test.go` `TestConcurrentAppend` (1) |
| Verification             | **Live**: `TestSQLiteStore_ConcurrentWritersAndReaders` confirms readers interleave with 4 concurrent writers and every write is readable post-run; `TestSQLiteStore_PragmasApplied` verifies `journal_mode=wal`, `synchronous=NORMAL`, `MaxOpenConnections=8`; `TestConcurrentAppend` persists 200 events from 8 goroutines with unique IDs |

Closes recommendation **#7** from the performance review. The v0.1
interaction log and audit stores both opened SQLite with
`MaxOpenConns=1`, which serialized every reader behind every
writer even though WAL mode was already enabled in the DSN. At
scale this meant the async log worker pool (§2.20) could saturate
a single connection while `GET /api/v1/logs` and `GET /api/v1/costs`
piled up behind it.

Changes:

- **Pool sizing** — `MaxOpenConns=8`, `MaxIdleConns=8` on both
  stores. Eight is a conservative ceiling: big enough for the
  typical dashboard + SDK + worker mix, small enough that file
  handle and goroutine pressure stay bounded on constrained hosts.
- **`synchronous=NORMAL`** added to both DSNs. Standard WAL-mode
  pairing for log-class workloads: durable against process crashes,
  can lose ~1 ms of writes on a hard power-off — acceptable
  tradeoff for interaction logs and audit events, neither of which
  is a system of record.
- **Redundant mutex removed** from `audit.SQLiteStore.Append`. The
  mutex was inherited from the v0.1 single-conn design; with WAL
  serialization at the database layer and `database/sql` handling
  goroutine safety at the pool layer, holding a Go-level mutex
  across `ExecContext` just re-serialized everything the new pool
  was meant to unblock.
- **Tenancy store is deliberately left at `MaxOpenConns=1`** and
  this is now documented inline in both storage files. The tenancy
  `Resolve` path holds a `*sql.Rows` iterator open while issuing an
  `UPDATE` on the same connection (see the inline comment in
  `internal/tenancy/store.go`); raising the pool would not cause a
  deadlock any more, but the auth cache slice (§2.21) already
  removed bcrypt from the hot path so the tenancy single-conn
  bottleneck is no longer load-bearing.

Regression guards:

- **`TestSQLiteStore_ConcurrentWritersAndReaders`** — 4 writers ×
  50 appends run alongside 4 readers polling `Count` and `Query`.
  Asserts no deadlock, every write is eventually readable, and
  `interleaveObserved` is set (proof that a read completed while
  writes were still in flight — the headline property the multi-
  conn pool provides). If a future refactor drops back to
  `MaxOpenConns=1` or disables WAL, this test will flag it.
- **`TestSQLiteStore_PragmasApplied`** — direct `PRAGMA` queries
  assert `journal_mode=wal` and `synchronous=1` (NORMAL) are
  actually applied. A DSN typo would silently fall back to default
  `journal_mode=delete`, which is the failure mode this test
  catches.
- **`TestConcurrentAppend`** — 8 goroutines × 25 appends produce
  200 audit events with unique IDs and no errors. Also exercises
  the no-mutex Append path under contention.

### 2.21 Auth cache for tenancy Resolve  *(perf — multi-tenant)*

| Item                  | Location                                                 |
| --------------------- | -------------------------------------------------------- |
| Cache type            | `internal/tenancy/auth_cache.go`                         |
| Store integration     | `internal/tenancy/store.go` `EnableAuthCache`, `Resolve`, `Delete*`, `UpdateAPIKeyRole` |
| CLI wiring            | `cmd/mockagents/start.go` — on by default in multi-tenant mode |
| Tests                 | `internal/tenancy/auth_cache_test.go` (10)               |
| Verification          | **Live**: `TestResolveUsesAuthCache` measured cold=36.3 ms (one bcrypt compare at cost=10) vs hot=sub-µs — **~∞× speedup** on repeat authentications; invalidate tests verify cached Principal cannot outlive a `DeleteAPIKey`, `UpdateAPIKeyRole`, or `DeleteTenant` call |

Closes the **#3 auth LRU cache** recommendation from the
performance review. The multi-tenant hot path previously ran
`bcrypt.CompareHashAndPassword` on every authenticated request,
which is intentionally slow (~36 ms at cost=10 on a modern laptop)
and executed under `MaxOpenConns=1` — meaning a single slow
comparison would serialize every subsequent auth attempt behind it.

Design:

- **Bounded, TTL-gated, opt-in.** The cache is only installed after
  `store.EnableAuthCache(ttl, maxSize)` is called. Existing tests
  continue to exercise the uncached path unchanged. `cmd/mockagents
  start` enables the cache with 5 min TTL / 1024 entries whenever
  multi-tenant mode is on.
- **Hash-keyed, never stores plaintext.** Keys are
  `sha256(plaintext)[:16]` (32 hex chars). Raw API keys never sit
  in memory, so a heap dump or a crash log cannot leak them.
- **Principal stored by value.** `Get` returns a fresh pointer so a
  caller mutating the returned struct cannot poison subsequent
  cache hits.
- **Coarse invalidation.** `DeleteAPIKey`, `UpdateAPIKeyRole`, and
  `DeleteTenant` (which cascades key deletes) all call
  `cache.Invalidate()` — which rebuilds the map rather than
  iterate-and-delete for GC-friendliness. Mutations are orders of
  magnitude less frequent than reads, so flushing the whole cache
  on every mutation is both correct and cheap.
- **Random eviction at capacity.** When `len(entries) >= maxSize` a
  new Set evicts one existing entry by exploiting Go's randomized
  map range. O(1), probabilistically fair, no linked list or
  timestamp bookkeeping required. Hot keys re-populate on their
  next Get.
- **last_used is NOT bumped on cache hits.** The tradeoff is that
  an always-hot key will look slightly stale in the admin console,
  but auth latency is the dominant concern and the counter
  self-corrects each time the entry expires and is re-resolved.

Tests cover: hit/miss, TTL expiry, explicit Invalidate, capacity
eviction, nil-cache no-op, concurrent access (16 goroutines × 100
iterations each — safe under `go test -race` when cgo is
available), end-to-end Resolve cold-vs-hot timing, and the three
mutation paths (`DeleteAPIKey`, `UpdateAPIKeyRole`, `DeleteTenant`)
that must flush the cache.

### 2.20 Bounded log worker pool  *(perf — scalability)*

| Item                   | Location                                              |
| ---------------------- | ----------------------------------------------------- |
| Worker type + metrics  | `internal/server/log_worker.go`                       |
| Middleware rewire      | `internal/server/log_handlers.go` `InteractionCapture` now takes `*LogWorker` |
| Server wiring + drain  | `internal/server/server.go` (`s.logWorker`, `Shutdown` drains) |
| Tests                  | `internal/server/log_worker_test.go` (7), `interaction_capture_test.go` updated (2) |
| Verification           | **Live**: concurrent-submit stress test (16 goroutines × 50 entries) passes; overflow test (burst=200 into queue=2) consistently drops; Shutdown is idempotent; full test suite (21 packages) green |

Closes the "unbounded goroutine-per-request" pattern flagged in
`docs/benchmarks/README.md` baseline pprof notes. Previously
`InteractionCapture` launched `go func() { store.Log(...) }()` for
every request — at the benchmark's 1.7M ops/sec that is 1.7M
goroutines/sec, and the 54 % GC cumulative cost in the baseline
profile was driven entirely by this fan-out.

New design:

- **Bounded queue** (default 1024 entries) fronts a fixed pool of
  workers (default 4). Sized so a 1024-entry backlog is ~1 MiB
  worst-case resident, bounded vs. the old unbounded fan-out.
- **Non-blocking Submit.** Full queue → increment `Dropped` counter
  and return false. User-facing request latency is never held up
  waiting for a log write, which is the right trade-off for a mock
  server where log completeness is a nice-to-have.
- **Atomic counters** — `Submitted`, `Written`, `Dropped`, `Failed`
  — plus `QueueLen`/`QueueCap`. Reachable via `worker.Metrics()`
  from expvar/Prometheus wiring. Invariants: every Submit call
  counts exactly once (`attempts = Submitted + Dropped`), and
  `Written + Failed <= Submitted` at all times (the pre-select
  increment is rolled back on the overflow branch so workers
  reading the channel can never see Written > Submitted).
- **Graceful drain on Shutdown.** After `httpServer.Shutdown`
  returns, the worker closes its queue and waits up to
  `DefaultLogDrainTimeout` (2 s) for in-flight writes. Metrics are
  logged at info level so operators can tell whether any drops
  happened during the last run.
- **Nil-safe.** A nil `*LogWorker` is a valid no-op; middleware
  code stays simple when logging is disabled.

Tests cover: happy-path drain, deterministic overflow
(burst=200 → queue=2), post-shutdown Submit drops cleanly, double
Shutdown is safe, nil-worker path, and concurrent Submit from 16
goroutines.

### 2.19 Zero-risk micro-optimization slice  *(perf follow-up)*

| Item                          | Location                                            |
| ----------------------------- | --------------------------------------------------- |
| Session pre-size              | `internal/engine/state/session.go` `initialMessageCap` |
| Tracer NoOp bypass            | `internal/observability/tracing.go` `IsEnabled()`, `internal/engine/engine.go` `traceOn` guard |
| Lazy captures map             | `internal/engine/scenario_matcher.go` `matchedSentinel` |
| Template buffer pool          | `internal/engine/response_generator.go` `renderBufPool` |
| `byModel` index               | `internal/engine/agent_registry.go`                 |
| Before/after measurements     | `docs/benchmarks/README.md` "Release 2026-04-14 micro-optimization slice" |
| Refreshed baseline            | `docs/benchmarks/latest.{json,md}`                  |
| Verification                  | **Live**: `make bench-report` re-ran on identical hardware; `ProcessRequest_StaticResponse` 557.7 → **500.8 ns/op** (-10.2 %); `ProcessRequest_DefaultFallback` -24.3 %; `ScenarioMatcher_Default` -63.6 %; every ProcessRequest benchmark down 10-24 %, allocations down proportionally (12→9, 15→9, 30→22); all 21 Go test packages still pass |

Follow-up to the §2.18 benchmark slice. No public API change, no
new dependency, no reordering of existing middleware — just five
mechanical hot-path fixes the baseline pprof profile had flagged:

- Pre-sized session history kills `growslice` on the common 3-8 turn path.
- The tracer bypass eliminates the variadic `[]attribute.KeyValue`
  slice allocation per call to `span.SetAttributes` whenever OTEL
  is not actively exporting (the default).
- Lazy captures allocation is the biggest win: ContentContains-only
  scenarios — the hottest matcher configuration — no longer allocate
  a map at all. `ScenarioMatcher_ContentContains` dropped from
  75.6 to 28.8 ns/op (-62 %) and from 2 allocs/op to 1.
- The template buffer pool shaves ~100 ns off every template render
  without touching the static path.
- The `byModel` index turns the adapter's model lookup from O(n)
  agent-scan to O(1) map lookup; today's benchmark uses a single
  agent so the headline number is flat, but deployments with hundreds
  of agents will feel it on every request.

### 2.18 Benchmark report + profiling workflow  *(MVP carry-over US-12.2)*

| Item                 | Location                                              |
| -------------------- | ----------------------------------------------------- |
| Benchmark tool       | `tools/benchreport/main.go`                           |
| Makefile targets     | `bench`, `bench-report`                               |
| Published artifacts  | `docs/benchmarks/latest.json`, `docs/benchmarks/latest.md` |
| Workflow + baseline  | `docs/benchmarks/README.md` (includes 2026-04-14 pprof snapshot) |
| Source benchmarks    | `internal/engine/benchmark_test.go` (12)              |
| Verification         | **Live**: `make bench-report` on Go 1.26.1 / windows/amd64 emitted 12 results into `docs/benchmarks/latest.{json,md}`; baseline static-response pipeline p50 ≈ 594.8 ns/op with GC-bound allocation profile documented in README |

Closes the last Phase 1 P1 carry-over (**US-12.2 Performance
Benchmarking**). The deliverable has three pieces:

- **Reproducible emitter.** `tools/benchreport` shells out to
  `go test -run=^$ -bench=. -benchmem` against a configurable package
  pattern, parses the standard output, strips the GOMAXPROCS suffix
  so laptop and CI results compare cleanly, and writes both a
  schema-versioned JSON document (`schema_version: "1"`) and a
  human-readable Markdown table with an auto-computed ops/sec column.
- **Published snapshot.** `docs/benchmarks/latest.{json,md}` is
  checked in so reviewers can see the current numbers without
  running anything locally. A target envelope (registry < 100 ns/op,
  matcher < 1 µs, static ProcessRequest < 2 µs, tool-call
  ProcessRequest < 5 µs) lives alongside the table so a regression
  is obvious at a glance.
- **Profiling workflow.** The README captures the exact `pprof`
  commands to use when a benchmark regresses, and a
  "Release 2026-04-14 profile notes" section documents the baseline
  hotspot distribution — GC scan/mark ~54 % cumulative,
  `Session.AppendUserMessage` ~10 %, `runtime.mallocgc` ~15 %. No
  optimization was required for this release (every benchmark sits
  inside the target envelope); the profile is archived as a
  reference point for the next release cycle.

### 2.17 Cost estimation engine + log cost annotation  *(Phase 4)*

| Item                 | Location                                              |
| -------------------- | ----------------------------------------------------- |
| Price table + math   | `internal/pricing/pricing.go`                         |
| Usage extractor      | `internal/pricing/extract.go`                         |
| YAML override loader | `internal/pricing/pricing.go` `LoadYAML` / `FromEnv`  |
| Costs aggregate API  | `internal/server/costs_handler.go`                    |
| Log annotation       | `internal/server/log_handlers.go` `LogWithCost`       |
| Request meta carrier | `internal/engine/reqmeta.go`                          |
| Adapter stamp        | `internal/adapter/{openai,anthropic}.go`              |
| CLI wiring           | `cmd/mockagents/start.go` (always-on, defaults + env) |
| Tests                | `internal/pricing/pricing_test.go` (12), `internal/server/interaction_capture_test.go` (2), `internal/engine/reqmeta_test.go` (2) |
| Verification         | **Live**: built-in price table seeded for 10 OpenAI/Anthropic models; `GET /api/v1/costs?since=…` returns zero-sorted `by_model` + `by_agent` groups; `GET /api/v1/logs` rows carry `prompt_tokens`, `completion_tokens`, `cost_usd` fields |

Closes the Phase 4 **cost estimation** row. Usage blocks are parsed
out of stored response bodies by `pricing.ExtractUsage`, which
handles both the OpenAI (`usage.prompt_tokens` /
`usage.completion_tokens`) and Anthropic (`usage.input_tokens` /
`usage.output_tokens`) shapes from a single JSON probe. The
`pricing.Table` is a thread-safe case-insensitive map with a
configurable `Fallback` price (zero by default) so unknown models
never drop out of a cost total silently.

Operators override defaults with `MOCKAGENTS_PRICING=/path/to.yaml`:

```yaml
prices:
  - model: gpt-4o
    prompt_per_1k_usd: 0.002
    completion_per_1k_usd: 0.008
fallback:
  prompt_per_1k_usd: 0.001
  completion_per_1k_usd: 0.003
```

The `start.go` bootstrap always constructs a default table, merges
overrides on top when the env var is set, and logs a warning (without
failing startup) on unreadable override files.

Two consumers share the table:

- `GET /api/v1/costs` — aggregates from the interaction-log store
  with `since`/`until`/`agent`/`limit` filters, returns total
  requests/tokens/USD plus `by_model` and `by_agent` arrays sorted
  descending by cost.
- `GET /api/v1/logs` — every row is decorated with a `LogWithCost`
  wrapper carrying per-row `prompt_tokens`, `completion_tokens`,
  `model`, and computed `cost_usd`.

**Request-meta plumbing** (`internal/engine/reqmeta.go`,
`WithRequestMeta` / `RequestMetaFromContext`) is the infrastructure
that makes `by_agent` accurate: `InteractionCapture` middleware
attaches a mutable `RequestMeta` pointer to the request context, the
adapter handlers stamp `resp.AgentName` + the requested model onto
it after `ProcessRequest` resolves, and the async log writer reads
the field on the way out. A body-probe fallback is preserved for
the error path where `ProcessRequest` never ran (validation errors,
chaos 429 before resolve). No import-cycle: the helpers live in
`engine`, which both `server` and `adapter` already import.

### 2.16 Audit logging  *(Phase 4)*

| Item                | Location                                              |
| ------------------- | ----------------------------------------------------- |
| Types + store       | `internal/audit/{types,store,recorder}.go`            |
| Server handler      | `internal/server/audit_handlers.go`                   |
| Route + actor bridge| `internal/server/server.go` (`principalToActor`)      |
| Recorder hook-ups   | `internal/server/{handlers,tenancy_handlers}.go`      |
| CLI wiring          | `cmd/mockagents/start.go` (always-on, `.mockagents-audit.db`) |
| Tests               | `internal/audit/audit_test.go` (14), `internal/tenancy/tenancy_test.go` new denial-hook + role-update cases (3) |
| Verification        | **Live**: single-tenant agent reload produced `{actor: anonymous, kind: agent.reloaded, target: echo-agent}`; multi-tenant `api_key.created` captured the admin actor (tenant_id + key_id + role + remote_ip) and anonymous reads of `/api/v1/audit` correctly returned 401. |

Closes the Phase 4 **audit logging** row. Seven event kinds —
`tenant.created`, `tenant.deleted`, `api_key.created`,
`api_key.deleted`, `api_key.role_changed`, `agent.reloaded`, and
`auth.denied` — are recorded to a dedicated SQLite file
(`.mockagents-audit.db`, independent of the interaction log and
tenancy stores).

**Auth-denial audit trail**: every 401 and 403 at the control
plane routes through `tenancy.SetDenialHook`, which the server
wires to `recorder.RecordHTTP(EventAuthDenied, ...)` during `New`.
Missing credentials, invalid API keys, and insufficient-role
rejections each produce an `auth.denied` row stamping the HTTP
method + path as the target and `{status_code, reason}` in the
details blob. Anonymous denials still persist — failed-auth spikes
are visible to operators even when no principal is present. The
tenancy package stays import-cycle-free of audit via the
package-level hook variable.

**API key role changes** land via a new `PATCH /api/v1/keys/{id}`
handler (`tenancy.UpdateAPIKeyRole`) that atomically promotes or
demotes an existing key and returns the `{from, to}` transition.
Every successful change emits an `api_key.role_changed` event so
privilege escalations leave a trail alongside the original
`api_key.created` row. The append API is lock-serialized; reads
support filters `?kind=`, `?actor=`, `?since=` (RFC3339), `?limit=`
(default 100, clamped at 1000). An unknown `kind` returns 400 with a
hint listing the valid values.

The recorder is import-cycle-safe by design: the server package owns
`principalToActor` and passes it into `audit.NewRecorder`, so the
audit package has zero knowledge of tenancy internals. In
single-tenant mode every event records `actor: anonymous`; in
multi-tenant mode the authenticated principal's key id, tenant id,
and role are stamped on every event.

The `/api/v1/audit` read endpoint is gated by the auth middleware
like every other `/api/v1/*` route; when multi-tenant mode is on, it
additionally requires the admin role (explicit `RequireRole` wrap)
so the who-did-what surface stays private to operators.

Remote IP capture prefers `X-Forwarded-For` (common behind
Kubernetes ingresses) and falls back to `RemoteAddr`. Plaintext API
keys are **never** written to the audit log — the actor field stores
the bcrypt-hashed key's opaque id and the details blob for
`api_key.created` carries only the public prefix, name, and role.

### 2.15 CI/CD integration: GitHub Actions + GitLab CI  *(Phase 2)*

| Item                                   | Location                                                            |
| -------------------------------------- | ------------------------------------------------------------------- |
| GitHub Actions composite action        | `deploy/actions/mockagents-test/action.yml`                         |
| Composite action README with usage     | `deploy/actions/mockagents-test/README.md`                          |
| GitLab CI job template                 | `deploy/ci/gitlab-ci.yml`                                           |
| Verification                           | Both YAML files parse cleanly via `yaml.safe_load`; action has 6 inputs, 1 output, 4 composite steps; GitLab job declares `artifacts.reports.junit`. |

Closes the Phase 2 **CI/CD integration** row from the implementation
plan. Users now get a single-step drop-in for the two most common CI
hosts instead of hand-rolling `go install` + validate + test +
artifact-upload boilerplate.

**GitHub Actions composite action** inputs: `version` (default `latest`,
pinnable to any tag), `agents-dir`, `suites`, `junit-output`,
`go-version` (default `1.26` to match `go.mod`), `skip-validate`.
Exposes `junit-report` as a step output so downstream reporter
actions (e.g. `mikepenz/action-junit-report@v5`) can consume the XML
without hardcoding the path. Composite rather than Docker so it
shares cache layers with other Go steps and starts in seconds.

**GitLab CI template** is a single `mockagents:test` job usable
either via `include:` or copy-paste. Runs on `golang:1.26-alpine`,
installs via `go install`, and attaches the JUnit XML as a
`artifacts.reports.junit` so GitLab's built-in MR-test-summary UI
picks it up — no plugin required.

### 2.14 CI readiness: fsnotify hot reload + JUnit XML  *(MVP carry-over + Phase 2)*

| Item                          | Location                                              |
| ----------------------------- | ----------------------------------------------------- |
| fsnotify watcher              | `internal/server/watcher.go`                          |
| `--watch` / `-w` flag         | `cmd/mockagents/start.go`                             |
| JUnit reporter                | `internal/runner/junit.go`                            |
| `--format junit` flag         | `cmd/mockagents/test.go`                              |
| Tests                         | `internal/server/watcher_test.go` (5), `internal/runner/junit_test.go` (4) |
| Verification                  | **Live**: `mockagents test … --format junit` prints valid JUnit XML; `mockagents start --watch --help` documents the flag |

Closes two roadmap items in one slice:

- **US-2.3 hot reload (MVP P1 carry-over).** `AgentDirWatcher` debounces
  rapid Create/Write/Rename events (150 ms default), parses the file
  through `config.LoadFile`, skips non-Agent kinds (Pipeline, TestSuite,
  MCPServer), validates, and registers on success. Validation failures
  are logged and the previous definition is kept — a bad save never
  wipes a known-good agent. Enabled via `mockagents start --watch`.
- **Phase 2 JUnit XML reporter.** `internal/runner.WriteJUnit` produces
  Jenkins-compatible XML: `<testsuites>` → `<testsuite>` → `<testcase>`
  with optional `<failure>`. Aggregate `tests`, `failures`, and `time`
  counts roll up to the top level. Every case's ErrMessage or first
  Failures entry becomes the `<failure message="...">` one-liner; the
  chardata holds the full joined failure list. Drops directly into
  GitHub Actions, GitLab, CircleCI, and Jenkins test reporters.

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

**Scope limit**: this slice originally protected only the control plane
(`/api/v1/*`). The 2026-05-30 hardening pass (§2.52) keeps LLM
endpoints compatible with anonymous local clients while honoring valid
MockAgents API keys for tenant-scoped model listing and request
resolution.

### 2.52 Architecture review hardening  *(server + tenancy + storage + engine)*

| Item                         | Location |
| ---------------------------- | -------- |
| Localhost default bind        | `internal/server/server.go`, `cmd/mockagents/start.go` — `Config.Host`, `DefaultHost=127.0.0.1`, `--host` and `MOCKAGENTS_HOST` |
| Container bind opt-in         | `Dockerfile`, `docker-compose.yml`, `deploy/helm/mockagents/*` — container paths explicitly pass or set `0.0.0.0` |
| Principal-derived tenant      | `internal/tenancy/middleware.go`, `internal/server/middleware.go`, `internal/adapter/{openai,anthropic}.go` — valid API keys attach the tenant; `X-Mockagents-Tenant` no longer controls scope |
| Scoped model listing          | `internal/adapter/openai.go` — `/v1/models` lists global-only for anonymous callers, global + tenant agents for authenticated callers |
| Tenant-scoped observability   | `internal/storage/{models,sqlite}.go`, `internal/server/{log_handlers,costs_handler}.go` — `tenant_id` persisted, migrated, queried, deleted, streamed, and cost-aggregated by tenant |
| Same-session turn atomicity    | `internal/engine/engine.go`, `internal/engine/state/{session,store}.go` — per-session lock wraps user append, scenario match, response generation, tool processing, and assistant append |
| Tests                         | `internal/server/server_test.go`, `internal/server/log_hardening_test.go`, `internal/storage/sqlite_test.go`, `internal/engine/session_concurrency_test.go` |
| Verification                  | `go test ./internal/storage ./internal/server ./internal/adapter ./internal/tenancy -count=1` |

Closes the P0 architecture review hardening lane (AHR-01, AHR-02,
AHR-03) plus AHR-04a from `docs/sprint-backlogs.md`.

What landed:

- The local binary now binds to loopback by default. Operators must
  explicitly choose `--host 0.0.0.0` (or `MOCKAGENTS_HOST=0.0.0.0`)
  to expose the server outside the machine.
- Multi-tenant LLM endpoints remain SDK-compatible: anonymous calls
  still work for global agents, while valid MockAgents API keys are
  resolved opportunistically and copied into the engine tenant context.
  Invalid placeholder provider keys on skipped LLM routes are ignored
  so existing local OpenAI clients using `api_key="mock"` keep working.
- The spoofable `X-Mockagents-Tenant` request header is no longer read
  by OpenAI or Anthropic adapters. Tenant scope now comes only from a
  resolved API-key principal.
- `/v1/models` uses the same tenant-visible registry view as request
  resolution, preventing anonymous callers from enumerating tenant
  models.
- Interaction logs gained `tenant_id` with a migration for existing
  SQLite databases. Log list/detail/delete, cost aggregation, and SSE
  live feed delivery all filter by the caller's tenant id when present.
- Same-session engine turns now run inside `Session.ApplyTurn`, so
  concurrent requests for one session cannot interleave the user append,
  turn-number match, template variable read, and assistant append.

### 2.53 Multi-pass security review hardening  *(internal/server + internal/tenancy)*

| Item                         | Location |
| ---------------------------- | -------- |
| Review reports               | `review/pkg-internal-server/`, `review/pkg-internal-tenancy/` (00-SUMMARY / 01-PER-FILE / 02-INTEGRATION / 03-ACTION-PLAN each) |
| Cross-tenant key IDOR (P0)    | `internal/tenancy/store.go` — `DeleteAPIKey`/`RotateAPIKey`/`UpdateAPIKeyRole` take `tenantID` + `AND tenant_id = ?`; handlers pass `principal.TenantID` + `ensureOwnTenant` |
| Platform/super-admin role     | `internal/tenancy/types.go` `RolePlatform`, `IsAssignableViaAPI`; `internal/server/route_authz.go`; `cmd/mockagents/start.go` bootstrap |
| Route-authz unification       | `internal/server/route_authz.go` — single `managementRouteFloors` table + `mountManaged` chokepoint (panics on an un-floored route) |
| Live-feed routing + flush     | `internal/server/server.go` (broadcaster built before `registerRoutes`), `internal/server/middleware.go` + `internal/observability/tracing.go` (`Unwrap`/`Flush`) |
| SSE lifecycle + isolation     | `internal/server/log_handlers.go` (write-deadline reset, per-tenant `SubscribeTenant`), `internal/server/log_broadcaster.go`, `internal/server/server.go` Shutdown order |
| Auth hardening                | `internal/tenancy/{middleware,auth_cache,store}.go` — fail-closed verified, full-digest cache key, timing-oracle dummy bcrypt, bearer parsing, plaintext redaction |
| Tests                         | `internal/server/*_test.go` (88→**144**), `internal/tenancy/*_test.go` (27→**46**) |
| Verification                 | `go test ./... -count=1` green; `go vet ./...` clean; benches unaffected (engine/adapter untouched — see `docs/benchmarks/README.md`) |

Two full multi-pass reviews (Pass 0 scope → per-file → cross-file →
synthesize → 4 reports) with adversarial verification of every S0/S1 and
a neuter-verify discipline (temporarily break the fix, confirm the new
test fails, restore) on every security fix.

What landed:

- **Tenant isolation.** Fixed a P0 cross-tenant API-key IDOR (a tenant
  admin could rotate/delete/promote another tenant's key) and the
  residual tenant-CRUD privilege gap (X-TN-001): tenant list/create/delete
  now require a dedicated `platform` role minted only by the bootstrap, so
  a per-tenant admin cannot enumerate or destroy other tenants nor
  self-escalate.
- **Live feed, twice over.** `GET /api/v1/logs/stream[/metrics]` were
  silently never mounted (the broadcaster was built *after* `registerRoutes`)
  and SSE flush was a no-op through the full middleware chain (a wrapper
  didn't forward `Flush`); both fixed. Streams now reset the per-connection
  write deadline so the global `WriteTimeout` can't sever them, are
  per-tenant scoped so a noisy tenant can't starve another's buffer, and no
  longer hang graceful shutdown.
- **Authorization, unified.** Every `/api/v1` management route's role floor
  lives in one table behind a single `mountManaged` chokepoint that panics
  on an un-floored route, so an ungated route can't slip in again.
- **Auth boundary, verified.** Auth fails closed on a store error, the auth
  cache flushes on every key mutation (no stale-auth), error messages carry
  no key-existence oracle, and `config.ValidateBytes` is not exposed to a
  YAML billion-laughs DoS (yaml.v3 bounds alias expansion — proven by test).
- **Hardening + hygiene.** Body-size caps + `413`, shared bounded `limit`
  + RFC3339 `since`/`until` validation, a uniform error envelope (no raw
  store errors leaked), `409` on duplicate tenant via an `ErrConflict`
  sentinel, `errors.Is` for all wrapped sentinels, atomic `DenialHook`,
  configurable CORS origins, robust bearer parsing, secret redaction on
  `NewAPIKeyResult`, an `RWMutex` cache hot path, and a sweep of dead code
  + doc drift. Every fix is regression-tested.

The four action plans (`review/pkg-internal-{server,tenancy}/03-ACTION-PLAN.md`)
are fully checked off; no open review items remain in either package.

### 2.54 Performance handoff + P1 hot-path optimizations

| Item                         | Location |
| ---------------------------- | -------- |
| Performance handoff guide     | `docs/PERFORMANCE.md` — perf model, prioritized backlog (PERF-01..21), "already optimized" list, scaling ceilings, methodology |
| PERF-01 O(1) tenant model index | `internal/engine/agent_registry.go` — `byModelTenant` (model → owner → smallest-named agent) replaces the per-request O(n) `GetByModelForTenant` scan |
| PERF-02 tracing wrapper skip  | `internal/observability/tracing.go` — `HTTPMiddleware` returns `next` unwrapped when no exporter is configured |
| PERF-03 auth `last_used` coarsening | `internal/tenancy/store.go` — bump only when stale ≥ 1 min (`shouldBumpLastUsed`) + `synchronous(normal)` on the tenancy DSN |
| PERF-04 pooled response encoder | `internal/adapter/encode.go`, `internal/server/handlers.go` — `respEncoder` (pooled buffer + encoder) for `writeJSON` |
| PERF-08 memoized match lowering | `internal/engine/scenario_matcher.go` — `lowerCache` for static `content_contains` literals |
| PERF-09 single-copy body capture | `internal/server/log_handlers.go` — `bodyString()` replaces the `snapshot()`+`string()` double copy |
| Tests/benches                | `agent_registry`/`tenant`, `tracing`, `scenario_matcher`, `adapter/encode`, `tenancy` (last_used), `server` capture — each fix neuter-verified |
| Verification                 | `go test ./... -count=1` green; `go vet ./...` clean |

A grounded performance review (parallel per-subsystem surveys → adversarial
verification of each headline finding) produced `docs/PERFORMANCE.md`, then the
four P1 items plus two cheap wins landed. Each fix carries a regression guard
and was neuter-verified (revert the fix → the guard fails → restore).

Measured results — and the honest ones, not the predicted ones:

- **PERF-01 (clear win):** `GetByModelForTenant` was an O(n) RLock scan of the
  agents map on **every** model-based LLM request. A `byModelTenant` index makes
  it two map reads while exactly preserving the tenant-visibility rule and the
  F-AR-002 lexicographic tie-break (rebuilt per-model on Register/Remove).
  **0 allocs, flat 29 ns/op at N=1000.**
- **PERF-02 (clear win):** the OTel HTTP middleware allocated a wrapper + span
  attributes + a context copy per request even under the NoOp tracer — and since
  no exporter is wired in the binary, that was *every* request. Now skipped at
  construction when tracing is disabled.
- **PERF-03 (clear win):** the auth path issued a full-fsync `last_used` write on
  every cache miss, serialized behind `MaxOpenConns=1`. Coarsened to ≤ once per
  key per minute and switched the tenancy DB to `synchronous=NORMAL`.
- **PERF-08 (clear win):** the matcher re-lower-cased the static `content_contains`
  literal per request; memoized, so mixed-case literals stop allocating.
- **PERF-04 (modest, measured honestly):** pooled the response JSON encoder —
  ~28 % faster encode but **unchanged B/op**, because `encoding/json` already
  pools its scratch and the per-call encoder stack-allocates. Kept as a safe CPU
  win; the doc says so.
- **PERF-09 (premise corrected):** the survey claimed the captured response body
  was buffered "purely for cost extraction" — but it is persisted and shown in
  the GUI log-detail view (`gui/app/logs/[id]/page.tsx`), so dropping it would be
  a feature regression and was *not* done. The safe adjacent win — collapsing a
  redundant double-copy of the body into one — landed instead:
  **3072 B/2 allocs → 1536 B/1 alloc** per loggable request.

The remaining backlog (PERF-05/06/07/11 + P3) is small alloc-shaving on an
already-healthy path. The `docs/benchmarks/latest.{json,md}` baseline was
**refreshed 2026-06-03** off-governor (temp High-performance plan, Balanced
restored) — `allocs/op` flat within ±1 vs. the 2026-04-14 baseline (three
benches improved); ns/op swings are full-sweep thermal noise. The PERF-01
tenant-model index now shows in the report at 14.3 ns / 0 allocs
(`GetByModelForTenant_ManyAgents`). See the 2026-06-03 refresh note in
`docs/benchmarks/README.md`.

---

### 2.55 GUI console redesign — "MockAgents Console" design system

| Item                         | Location |
| ---------------------------- | -------- |
| Design tokens + theme        | `gui/app/globals.css` — SentinelRAG `--sr-*` tokens + dark mirror + a legacy-var alias layer (`--surface`/`--border`… → `--sr-*`) so pre-redesign page CSS auto-adopts the palette and light/dark theme |
| App shell                    | `gui/app/Shell.tsx` (client: grouped nav via `usePathname`, breadcrumbs, theme toggle) + `gui/app/layout.tsx` (server: reads a `mockagents-theme` cookie for SSR `data-theme` — no flash, no inline script) |
| Icon set                     | `gui/lib/icons.tsx` — lucide paths as JSX (not `dangerouslySetInnerHTML`, preserving the no-raw-HTML posture); `gui/app/Stat.tsx` shared tile |
| Agents catalog + detail      | `gui/app/page.tsx` + `AgentCatalog.tsx`; `agents/[name]/page.tsx` + `AgentTabs.tsx` (tabbed, with a real Reload server action) |
| Logs                         | `gui/app/logs/LogsConsole.tsx` — design table + sticky inspector + live SSE (replaced `LogsTable`/`AutoRefreshLogs`) |
| Costs / Audit / Pipelines    | `costs/page.tsx`, `audit/page.tsx`, `pipelines/` list + detail (DAGViewer kept) rebuilt to the design card/`.tbl`/`Stat` vocabulary |
| Editor                       | `editor/YamlEditor.tsx` — two-column card grid: `agent.yaml` code card (line gutter reddens flagged lines) + result card (eyebrow, success/error banners, per-error cards); server-action validation preserved |
| Tenants / keys / Account     | `admin/tenants/page.tsx` (row-list card), `admin/tenants/[id]/page.tsx` (card-head controls over a `.tbl`), `account/page.tsx` (identity + actions cards) — all keep the existing server-action + flash-store secret handling (GUI-02) |
| Verification                 | `npm run typecheck` + `npm run build` green under `tsc --strict`; every surface runtime-verified 200 against the real Go backend (single- and multi-tenant) |

The Next.js console was restyled end-to-end to the "MockAgents Console" design
handoff. The chosen scope was **foundation + restyle of the existing surfaces**
plus a light/dark toggle; net-new mock-only surfaces (Playground, Chaos panel,
Contracts diff, Record/replay, MCP inspector, Settings) were **deferred**.

The foundation is a token + alias layer in `globals.css`: every legacy class
(`data-table`, `inline-form`, `plaintext-box`, `section-title`, `editor-*`, …)
is bridged to the `--sr-*` palette, so surfaces adopted the new look and the
theme toggle before being individually rebuilt — and an old-class regression
audit confirmed nothing broke during the staged rollout. Each surface landed as
its own `--no-ff` merge to `main` (commits 692115b foundation → 22739ea final
surfaces, PR #11). The GUI security posture from §2.53 is intact: icons render
as JSX (no raw HTML), one-time key plaintext flows through the server-side flash
store, and auth stays cookie-`Bearer` on every upstream fetch.

The remaining GUI gap is unchanged: the drag-to-rewire **workflow editor** for
`kind: Pipeline` (§6) and the deferred net-new mock-only surfaces above.

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
| `internal/adapter`                 | 32    | OpenAI + Anthropic + token estimation + conformance, decode buffer pool |
| `internal/audit`                   | 14    | Event kinds, recorder, SQLite append-only store         |
| `internal/build`                   | 9     | Binary smoke + conformance                              |
| `internal/cli`                     | 17    | Scaffold + integration                                  |
| `internal/config`                  | 127   | Loader, validator, defaults, kind dispatch, ValidateBytes (7), pipeline validator (9), graph checks (7), TestSuite (9), MCPServer (13), edge polish (5), **cross-doc reference checking (9)** |
| `internal/contract`                | 9     | Diff severity classification                            |
| `internal/engine`                  | 144   | Scenario matcher, tool processor, template gen, pipeline (3), chaos (9), tenant-scoped visibility |
| `internal/engine/state`            | 12    | Session store                                           |
| `internal/mcp`                     | 38    | JSON-RPC dispatch, HTTP, stdio, completion + logging + notifications, **bidirectional SSE + response (9)** |
| `internal/observability`           | 5     | Span attributes, HTTP middleware                        |
| `internal/pricing`                 | 12    | Price table, YAML override, OpenAI + Anthropic usage extract |
| `internal/recording`               | 11    | Cassette hash, proxy, replay, full round-trip, streaming capture |
| `internal/runner`                  | 7     | Agent + pipeline target, assertion failure paths, JUnit |
| `internal/server`                  | 88    | Handlers, middleware, conformance, security, log broadcaster + SSE stream (8), config validate handler (4), pipeline handlers (5), rotate handler (2), self-rotation handler (2), stream metrics (5), bulk rotation handler (3 — incl. ?except=self), burn handler (2) |
| `internal/storage`                 | 23    | SQLite log store, concurrent readers+writers            |
| `internal/streaming`               | 34    | SSE chunking, tool-call streaming                       |
| `internal/tenancy`                 | 32    | Store CRUD, auth middleware, RBAC, denial hook, role update, auth cache, rotation (3), bulk rotation (5 — incl. exclude test) |
| `sdk/go/mockagents`                | 44    | Client, scenario, expect, streaming (12), in-process (5), **MCP bidirectional helper (11)** |
| `sdk/python/mockagents` (pytest)   | 104   | Includes 11 adapter tests + 14 streaming tests, **MCP bidirectional helper (14)** |
| `sdk/typescript/mockagents` (vitest)| 53    | Client, scenario, assertions, server, adapters, streaming (13), **MCP bidirectional helper (15)** |

Running `make test-all` covers Go + Python + TypeScript. GUI type
safety is covered by `next build` running `tsc --strict`.

Roll-up: **646 Go tests** + **104 Python tests** + **53 TypeScript
tests** = **803 tests** across all three surfaces. All green at
2026-04-15.

---

## 6. Known Gaps (intentional)

From the SaaS slice README and per-feature READMEs, the following are
deferred deliberately:

| Area             | Gap                                                      | Earmarked for          |
| ---------------- | -------------------------------------------------------- | ---------------------- |
| Multi-tenancy    | Postgres store, SSO/OAuth, billing/quotas                | Future SaaS slices     |
| Multi-tenancy    | Per-tenant agent name collisions (currently global)      | Postgres slice         |
| GUI              | Workflow editor for `kind: Pipeline` (drag-to-rewire)    | GUI v0.3 (continued)   |

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
