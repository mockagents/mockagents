# Structural redesigns R1-R9

**Status:** Proposed. Design only; no code in this change.
**Origin:** 2026-10-06 quality review, resolution plan, iteration 9.
**Base:** tip of the unmerged fix stack (`adf037f`, branch `fix/q5-schema-parity`). Every `file:line`
citation below was read at that commit and will drift; grep the symbol, not the number.
**Scope:** nine structural changes that remove a *class* of defect rather than one instance of it.
Each section is independently reviewable and lands as a series of small PRs that keep `main` green.

Conventions used throughout:

- "Verified" means read in the worktree. "Inferred" means a consequence of what was read that I did not
  execute; those are collected again in [Appendix A](#appendix-a-claims-that-are-inferred-not-proven).
- Import direction rules in force (checked with `go list` on non-test imports at the base commit):
  `engine` imports `chaos`, `engine/state`, `metrics`, `observability`, `toolschema`, `types` and nothing
  that knows about tenants, audit or HTTP; `adapter` imports `chaos`, `engine`, `realtime`, `streaming`,
  `types`, `vector`; `streaming` imports `engine`, `types`; `tenancy` imports `clientip`, `quota`;
  `quota` and `storage` import nothing internal; `mcp` and `a2a` import `chaos`, `types` (+ `toolschema`
  for mcp); `server` imports almost everything. New packages below are placed to respect these edges.
- Perf gate: `.github/workflows/perf-guard.yml` runs `tools/benchreport -pkg ./internal/engine/...` and
  `tools/benchguard`, which gates **allocs/op exactly** and B/op within 20 % against
  `docs/benchmarks/latest.json` (ns/op is reported, not gated). Consequence used repeatedly below: any
  change inside `engine` must be allocation-neutral on the benchmarked paths, and changes outside
  `engine` currently have **no committed baseline at all** (see P0).

## Index

| ID | Title | Removes (review IDs) | Effort | Depends on | Main risk |
|---|---|---|---|---|---|
| [P0](#p0-prerequisite-widen-the-benchmark-baseline) | Widen the benchmark baseline | (enabler) | S | none | none |
| [R1](#r1-route-policy-table) | Route policy table | F2, S-02, E-04, E-06 | M | none | auth-surface regression |
| [R2](#r2-engine-metering-seam) | Engine metering seam | S-02..S-04 | M | R1 (soft: Billable declaration) | behaviour deltas, charge latency |
| [R3](#r3-adapter-dispatch) | Adapter dispatch | E-08, E-12, E-14, E-21 | L | R8 (soft) | six-provider refactor |
| [R4](#r4-exported-stream-plan) | Exported stream plan | E-02, E-08, L-15 | M | R3 | wire fidelity of new emitters |
| [R5](#r5-bounded-store-primitive) | Bounded store primitive | E-15, P-07, P-09, P-13, P-16 | M | none | re-introducing M-20 contention |
| [R6](#r6-json-rpc-core) | JSON-RPC core | P-12, L-39, L-38 | M | none | transport regressions |
| [R7](#r7-one-load-and-validate-entry-point) | One load-and-validate entry point | C-02, C-12 | S | none | `start` becomes stricter |
| [R8](#r8-session-identity-policy) | Session-identity policy | E-01, E-23 | S | none | hot-path allocation |
| [R9](#r9-shared-state-seam) | Shared state seam | HA gaps (sessions, logs, audit, quota caches) | L | R2, R5, R8 | scope: is HA a requirement? |

Recommended sequence and the dependency graph are at the end ([Ordering](#ordering-and-recommended-sequence)).

---

## P0: prerequisite, widen the benchmark baseline

**Problem.** The committed baseline covers one package: `Makefile:123` and `Makefile:126` pass
`./internal/engine/...`, and `docs/benchmarks/latest.md` lists 17 engine benchmarks. Benchmarks that already
exist elsewhere are not in it: `internal/adapter` (`BenchmarkDecodeJSONBody_Pooled`, `BenchmarkWriteJSON`,
`BenchmarkGenerateID`), `internal/server` (`BenchmarkRequestContext`, `BenchmarkLogFrame_*`,
`BenchmarkStatusWriterAcquireRelease`). `internal/streaming` has none. R1, R3, R4 and R5 change code that
the perf guard cannot see.

**Proposed.**
1. Let `tools/benchreport` accept several `-pkg` patterns (or run twice) and write one report with a
   `package` column; `benchguard` already compares by benchmark name.
2. Add the missing benchmarks *before* the refactors that need them, on unchanged code, and commit the new
   baseline in its own PR (so the later PRs show a diff against a baseline that predates them):
   - `server`: `BenchmarkServe_ChatCompletions` (full middleware chain via `httptest`, single-tenant and
     multi-tenant), `BenchmarkRouteClassify` (current `skipAuth` + `isLoggablePath` pair).
   - `adapter`: `BenchmarkHandler_{OpenAIChat,Anthropic,Gemini,Bedrock,Ollama,Responses}` (non-streaming,
     recorder-backed).
   - `streaming`: `BenchmarkStream_{OpenAI,Anthropic,Gemini}_NoDelay` (chunk delay 0, so CPU not sleep).
   - `engine/state`: `BenchmarkSessionStore_GetOrCreate_Parallel` (`b.RunParallel`).
3. `make bench` / `make bench-report` / `perf-guard.yml` pick up the extra packages.

**Tests.** The existing `benchguard` self-tests; one new test that the report contains every
`Benchmark*` function found by `go list -f '{{.TestGoFiles}}'`-style enumeration, so a benchmark can no
longer exist outside the baseline silently.

**Effort.** S.

---

## R1: Route policy table

### Problem

Who may call a route, and whether it counts as billable LLM traffic, is decided in **four unrelated
places** that must be kept in agreement by hand.

| Decision | Where it lives today |
|---|---|
| Anonymous access ("skipAuth") | `Server.skipAuth` (`server/server.go:874`) -> `openRoutes` shadow mux (`server/open_routes.go:35-69`), fed by `builtinOpenRoutes` (`open_routes.go:15`), every adapter route (`server.go:436`) and the engine endpoint (`server.go:536`) |
| Role floor | `managementRouteFloors` (`server/route_authz.go:27`), applied only by `mountManaged` (`route_authz.go:119`) |
| Billable / quota-able | `isLLMProviderPath` (`server/quota_middleware.go:25`) and its alias `isQuotaPath` (`:53`), a string classifier over paths |
| Loggable / metric-labelled | `isLoggablePath` (`server/log_handlers.go:612`), consumed by `InteractionCapture` (`:398`) and `MetricsCapture` (`server/metrics_handlers.go:58`) |

Concrete defects that follow from this shape (all verified):

1. **Two meanings of "open".** On a management route `roleOpen` means "any authenticated role"
   (`route_authz.go:9-15` comment); in `skipAuth` it means anonymous. `GET /api/v1/health` and `/ready`
   appear in both tables (`route_authz.go:29,35` and `open_routes.go:16-17`) and the two must agree.
2. **Three registration styles**, so "every route has a policy" cannot be asserted: management routes via
   `mountManaged` (panics without a floor, `route_authz.go:122`); adapter routes via
   `mux.HandleFunc(route.Pattern, ...)` plus `s.open.add` (`server.go:435-436`); and direct
   `mux.HandleFunc` for `/auth/*` (`server.go:519-521`) and `POST /v1/engines/process` (`server.go:535`).
   `http.ServeMux` has no pattern enumeration API, so no test can walk the real mux. Coverage today is
   three separate hand-maintained tests (`route_authz_test.go` Snapshot/AllValid, `open_routes_test.go`
   EveryAdapterRouteIsOpen/ManagementRoutesStayGated, `quota_paths_test.go`).
3. **Path-only classification ignores the method.** `isLLMProviderPath` takes a path, and `QuotaEnforce`
   sits *outside* the mux (`server.go:289-291`), so an authenticated tenant's `GET /v1/chat/completions`
   spends a rate token before the mux answers 405.
4. **String-suffix matching of wildcard routes.** Gemini is one pattern, `POST /v1beta/models/{modelmethod}`
   (`adapter/gemini.go:161`), covering generate, stream and countTokens; the classifier tells them apart by
   `HasSuffix(":generateContent")` (`quota_middleware.go:40-43`).
5. **Magic paths.** `/v1/realtime` is compared as a literal in `RealtimeBrowserAuth`
   (`realtime_wiring.go:92`); `/v1/engines/process` and `/v1/moderations` are literals in `isLoggablePath`.
6. **A fifth registration site.** The Go SDK's in-process client declares its own routes by hand
   (`sdk/go/mockagents/inprocess.go:94-97`).
7. **Docs are a manual mirror.** The role table in `docs/guides/multi-tenant.md` is kept in sync by a
   snapshot test that says "update this snapshot AND docs/guides/multi-tenant.md"
   (`route_authz_test.go:~191`).

### Proposed design

A leaf package that names the policy, adapters declare it next to each route, and the server owns exactly
one registration path.

```go
// internal/routepolicy: leaf, stdlib only (so adapter and server can both import it).
package routepolicy

type Access uint8
const (
    Anonymous     Access = iota + 1 // never fails closed; principal attached best-effort (today: skipAuth)
    Authenticated                   // any valid key or session (today: roleOpen on a management route)
    Role                            // Authenticated and Principal.Role >= MinRole
)
type Surface uint8 // Provider | Management | Probe | Auth | Internal  (docs, metric label, capabilities)

type Policy struct {
    Access   Access
    MinRole  string  // "viewer"|"editor"|"admin"|"platform"; only with Access==Role (validated by server)
    Billable bool    // handler is expected to consume engine work; R2's deny-all test enforces it
    Loggable bool    // interaction capture + request metrics apply
    Surface  Surface
}
// Zero Policy (Access==0) is invalid: registration fails. Convenience values keep adapter tables terse:
var ProviderLLM  = Policy{Access: Anonymous, Billable: true, Loggable: true, Surface: Provider}
var ProviderFree = Policy{Access: Anonymous, Loggable: true, Surface: Provider}
var ProviderOther = Policy{Access: Anonymous, Surface: Provider}
```

```go
// internal/adapter/registry.go
type Route struct {
    Pattern string
    Handler http.HandlerFunc
    Policy  routepolicy.Policy      // new; required
}
```

```go
// internal/server/route_table.go (replaces open_routes.go, mountManaged, the three classifiers)
type routeTable struct {
    mux       *http.ServeMux                  // the real router
    shadow    *http.ServeMux                  // sentinel handlers; used only to resolve pattern -> policy
    byPattern map[string]routepolicy.Policy
}
func (t *routeTable) Mount(pattern string, h http.Handler, p routepolicy.Policy) // the ONLY way to register
func (t *routeTable) Lookup(r *http.Request) (pattern string, p routepolicy.Policy, ok bool)
```

- `Mount` rejects a zero policy, an invalid `MinRole`, a duplicate pattern, and two patterns that share a
  path with different `Access` (the shadow mux registers by path, matching today's "a method mismatch on an
  open path still skips auth and reaches the real router's 405", `open_routes.go:43-46`). In multi-tenant
  mode it wraps `h` in `tenancy.RequireRole` for `Access==Role` exactly as `mountManaged` does today
  (`route_authz.go:137`).
- `Lookup` runs `shadow.Handler(r)` once per request and memoises the result in the existing per-request
  `*requestScope` (`server/middleware.go:30`), so no extra context node is allocated.
- Consumers: `tenancy.AuthMiddleware(store, skip)` gets `skip = Access==Anonymous`; `InteractionCapture` and
  `MetricsCapture` read `Loggable`; `mountedRoutes` (UX-01 capabilities, `identity_handlers.go:63,83`) is
  derived from `Surface==Management` entries instead of a separate map.
- Management routes keep their handlers where they are but declare policy at the `Mount` call, replacing
  `managementRouteFloors`. The Gemini `countTokens` case is handled by a per-request veto, not a second
  pattern (Go wildcards must span a whole segment): add `RequestMeta.SkipLog` (`engine/reqmeta.go:18`) which
  `HandleGenerate` sets for non-generate methods.

**Alternatives considered.**

| Option | Pros | Cons | Verdict |
|---|---|---|---|
| A. Resolve once up front via shadow mux, memoise in `requestScope` (above) | Middleware order unchanged; reuses the proven shadow-mux trick; one lookup per request | Extra `ServeMux.Handler` per request in single-tenant mode, where today only `isLoggablePath` runs | **Chosen**, behind a measured gate |
| B. Compose per route at mount time (wrap each handler with its own auth/capture/metrics chain) | Zero per-request lookup; `Request.Pattern` is set inside the mux for free | Changes the order of outermost middleware (Recovery, logger, CORS must still wrap 404/405); unmatched paths would stop returning 401 in multi-tenant mode unless a catch-all is added; `RealtimeBrowserAuth` needs a pre-auth hook | Fallback if A fails its gate |
| C. Keep four tables, add a test that cross-checks them | Smallest diff | Cross-checking four representations is what the three existing tests already do; the drift class remains | Rejected |

**Gate for A.** Added cost of `Lookup` over the current `skipAuth` + `isLoggablePath` pair must be 0
allocs/op and no more than 100 ns/op on `BenchmarkRouteClassify` (P0); otherwise switch to B.

### Migration plan (each step is one PR, `main` green after each)

1. **Add `routepolicy` and `routeTable`; `mountManaged` becomes a thin wrapper over `Mount`.** No behaviour
   change. Guard: `route_authz_test.go` (all of it), `open_routes_test.go`.
2. **Add `adapter.Route.Policy`; fill every adapter route** (mechanical, using the three convenience values).
   Server still ignores it, but a new test computes the open set from policies and asserts equality with
   the current `openRoutes` set (a parity test that is deleted in step 3). Guard:
   `adapter/registry_test.go` gains `TestDefaultRegistry_EveryRouteHasPolicy`.
3. **Switch `skipAuth` to `Lookup`; delete `openRoutes` and `builtinOpenRoutes`.** `/auth/*`, the engine
   endpoint and `/api/v1/health|ready` move to `Mount` here. Guard: `open_routes_test.go` rewritten over the
   table; anonymous-vs-401 matrix test (below).
4. **Switch capture and metrics to `Loggable`; delete `isLoggablePath`.** One-off parity test compares the
   old classifier to the table for every mounted pattern with sample paths, with the expected diffs
   spelled out (Gemini countTokens, `GET` on POST-only routes). `QuotaEnforce` temporarily reads `Billable`
   instead of `isQuotaPath`. Guard: `quota_paths_test.go` ported to table assertions; `log_*` tests.
5. **Move management floors into `Mount` calls; delete `managementRouteFloors` and the `mountManaged`
   panic.** The snapshot test now snapshots the whole table (pattern -> policy), adapters included, and the
   `multi-tenant.md` sync note stays. Guard: snapshot, `identity_handlers_test.go`.
6. **Add the bypass lint** (below) and route `sdk/go/mockagents/inprocess.go` through
   `adapter.DefaultRegistry` for its two providers (it has no auth, so it ignores `Policy`).

### Test strategy (the invariants)

- **I1: every registered route has a valid policy.** `Mount` is the only registration path, so
  `routeTable.Patterns()` *is* the route set. An AST test (`go/parser`) fails if any `.Handle`/`.HandleFunc`
  call on an `*http.ServeMux` appears in `internal/server` outside `route_table.go`.
- **I2: anonymous reachability matrix.** Build `New(...)` with every optional feature on (SSO, engine
  endpoint, audit, logs, pipelines, quota, vector, search). For each pattern, substitute wildcards and send
  a credential-less request in multi-tenant mode: the status is 401 iff `Access != Anonymous`.
- **I3: role matrix.** For each `Access==Role` pattern, one principal per role: 403 below `MinRole`, not
  401/403 at or above it. Replaces the hand-written cases in `route_authz_test.go`.
- **I4: Billable is real.** Delegated to R2's deny-all test (every `Billable` route returns the quota
  status when the meter denies everything; no non-`Billable` route does).
- **I5: docs.** `docs/guides/multi-tenant.md` role table is checked against `Surface==Management` entries
  by `tools/doccheck` rather than by a snapshot a human updates in two places.

### Performance

Hot path: yes, per request. Guarded by `BenchmarkRouteClassify`, `BenchmarkServe_ChatCompletions` and
`BenchmarkRequestContext` (`server/middleware_test.go`), all added to the baseline by P0. The engine
benchmarks are unaffected.

### Risks and open questions

- **Highest-consequence change in the set:** a mistake opens a control-plane route. Mitigations: zero
  policy is invalid (deny by construction), the parity tests in steps 2-4, and I2/I3 matrices.
- **Decision needed:** should provider routes that hold tenant data (files, batches, conversations and
  similar stores) stay `Anonymous` in multi-tenant mode, or require a credential? The table makes flipping
  one surface to `Authenticated` a one-line change; the question is whether to.
- Shadow-mux and real-mux must agree on pattern semantics. Both are `http.ServeMux`, so they do by
  construction; Go-version upgrades change both together.

**Effort: M** (six small PRs, mostly mechanical; step 3 is the delicate one).

---

## R2: Engine metering seam

### Problem

Quota admission and spend accrual are implemented **four times**, each reaching `quota.Enforcer` directly,
and the seams between them are the "side doors" the review found. The stack already closed the worst
ones by adding two more copies (`engine.NodeMeter` and `SubrequestMiddleware`); R2 replaces all of them
with one.

| Entry point | Admission | Charge |
|---|---|---|
| Direct HTTP (chat, messages, gemini, ...) | `QuotaEnforce` middleware (`server/quota_middleware.go:60-100`; `AllowRequest` `:72`, `CheckSpend` `:88`) | `InteractionCapture` spend hook (`log_handlers.go:511`, `spendHook` `server/metering.go:28`) parses the **response body** with `pricing.ExtractUsageForPath` |
| Batch sub-requests | same middleware via `SubrequestMiddleware` (`adapter/batches.go:266`, `anthropic_batches.go:295`, wired `server.go:427-429`, built `metering.go:70-83`) | `chargeSpend` tee (`metering.go:87-95`) |
| Pipeline nodes | `pipelineMeter.AdmitNode` (`metering.go:127`) via `engine.NodeMeter` (`engine/pipeline.go:41`) | `ChargeNode` (`metering.go:143`) estimates with `adapter.EstimateTokens`, which is why the interface exists (engine cannot import adapter) |
| Realtime responses | `rt.CheckQuota` (`realtime_wiring.go:38-49`) | `rt.OnResponse` (`:53-58`), token counts from `strings.Fields` word counts (`adapter/realtime.go:551-553`) |

Defects (verified unless marked):

1. **Unmetered door:** `POST /v1/engines/process` (`server.go:613`) reaches `ProcessRequestContext`, is auth-exempt
   (`server.go:536`) and is not in `isLLMProviderPath`. Documented as "not quota-metered" in
   `Config.EnableEngineEndpoint`, but it is a way around a tenant's cap.
2. **Streaming is never charged.** Capture is disabled for SSE/NDJSON/eventstream
   (`log_handlers.go:728-740`), and the spend hook reads the captured body; the comment at `:506-509`
   calls it "a documented soft-margin limitation". Any tenant using `stream:true` is outside the monthly cap.
3. **Spend accrual is hung off the log pipeline.** `InteractionCapture` is only installed
   `if s.logWorker != nil` (`server.go:296-300`) and short-circuits when `worker.store == nil`
   (`log_handlers.go:392`). `cmd/mockagents/start.go:238-242` logs a warning and continues when the log DB
   cannot open, so in that state rate limits work and **monthly spend caps never accrue**.
4. **Three token estimators** feed spend: provider-reported usage parsed from bodies, the 1.3x word heuristic
   (`adapter/token.go`), and raw word counts for realtime.
5. **Body truncation** (inferred): captured bodies are capped at 1 MiB (`log_handlers.go:610`) and
   `pricing.ExtractUsage` unmarshals JSON, so a truncated embeddings response yields zero usage.
6. **Wire shape:** every provider gets the OpenAI-style flat `ProviderQuotaError` (`quota_middleware.go:80,89`),
   including Anthropic, Gemini, Bedrock and Ollama callers.
7. **Embeddings never reach the engine.** `registry.go:77` builds `&EmbeddingsHandler{}` (no `Engine`), and
   `embeddings.go` has no `ProcessRequestContext` call, yet `/v1/embeddings` is billable per
   `isLLMProviderPath`. A meter placed only inside the engine would silently stop metering embeddings.
8. **Charge is synchronous I/O.** In backend mode `Enforcer.AddSpend` does a DB write with a 5 s timeout
   inline (`quota/quota.go:219`). Today that runs after the response is written; moving it ahead of the
   response would put database latency on every generation.

### Proposed design

The engine owns admission and charging; everything that consumes engine work is metered because it went
through the engine.

```go
// internal/engine/meter.go
type WorkKind uint8 // WorkGenerate | WorkEmbed | WorkPipelineNode | WorkRealtimeResponse (label only)
type Admission struct{ Tenant, Agent, Model, Wire string; Kind WorkKind }
type Usage struct{ PromptTokens, CompletionTokens int }

type Meter interface {
    // Admit is called once per unit of work, after agent resolution and before chaos injection.
    // Return *QuotaError to refuse. Never called for Tenant == "" (single-tenant / anonymous).
    Admit(ctx context.Context, a Admission) error
    // Charge records measured usage. Must not block on I/O.
    Charge(ctx context.Context, a Admission, u Usage)
}
type QuotaKind uint8 // QuotaRate | QuotaSpend
type QuotaError struct{ Kind QuotaKind; RetryAfter time.Duration; Message string }
func AsQuotaError(err error) *QuotaError // same shape as AsChaosError / AsStrictToolError
```

- `Engine.Meter Meter` (nil = off). `ProcessRequestContext` adds `if e.Meter != nil && tenantID != ""` at two
  points: Admit before `Chaos.Before` (`engine.go:242`), Charge after generation. Public wrappers
  `Engine.Admit/Charge` let non-engine handlers (embeddings) use the same meter: `EmbeddingsHandler` gains
  an `Engine` field, set in `registry.go:77`.
- Token estimation moves to a leaf package `internal/tokens` (the body of `adapter/token.go`; `adapter`
  keeps a forwarding `EstimateTokens`). The engine charges `tokens.Estimate(messages)` /
  `tokens.Estimate(resp.Content)`. Adapters later read the same numbers from `Response.Usage` (R3), so
  billed usage, wire usage and log usage stop being three computations.
- `server.quotaMeter` (new, `server/meter.go`) implements `Meter` over `quota.Enforcer` + `pricing.Table`.
  `Admit` calls `AllowRequest`/`CheckSpend`; `Charge` enqueues to a bounded, per-tenant-coalescing channel
  drained by a worker that calls `AddSpend`, so no I/O on the request goroutine (fixes defect 8; the
  enforcer already tracks `unsynced` spend for exactly this kind of deferral).
- Adapters render `*engine.QuotaError` through one helper `writeQuotaError(w, qe)` whose output is
  **byte-identical** to today's (`Retry-After` from `retryAfterSeconds`, body `ProviderQuotaError`).
  Native per-provider quota shapes are a separate, later, changelog-worthy step.
- `PipelineExecutor.Meter`/`NodeMeter`, `pipelineMeter`, `quotaDenial`, `subrequestMeter`, `chargeSpend`,
  `SubrequestMiddleware` (both batch handlers), `QuotaEnforce`, the spend-hook parameter of
  `InteractionCapture`, and `RealtimeHandler.CheckQuota` are deleted. `RunPipeline` maps `*QuotaError`
  where it maps `*quotaDenial` today (`pipeline_handlers.go:~150`).

**Alternatives considered.**

| Option | Verdict |
|---|---|
| Keep HTTP middleware, add the engine endpoint and streaming to it | Leaves four implementations and the streaming gap is structural (the body is not buffered). Rejected. |
| Meter in `dispatch` (R3) instead of the engine | Pipeline nodes and realtime call the engine without an adapter, so they would need their own path again. Rejected for admission; **charge** could still move there if exact wire usage (Anthropic thinking and cache adjustments) must equal billed usage. Open question below. |
| Make `Meter` async for Admit too | Admit must be able to refuse; it is a local token-bucket check today. Kept synchronous. |

### Migration plan

0. **Characterisation tests against current code** enumerating entry point x {rate denial, spend denial,
   charge recorded}. Known gaps (engine endpoint, streaming, no log store, embeddings truncation) are
   written as expected-failing with a marker that each later PR flips.
1. **Extract `internal/tokens`.** Pure move. Guard: `adapter/token_test.go`.
2. **Add `engine.Meter`, `QuotaError`, `Engine.Meter`, wrappers, call sites.** Nil by default. Guard: engine
   `BenchmarkProcessRequest_*` must be identical in allocs/op with `Meter==nil`; new
   `BenchmarkProcessRequest_Metered` (noop meter, tenant set) establishes the cost of the seam.
3. **Switch entry points, one PR each, deleting the old path in the same PR** (so nothing is double-counted):
   - 3a pipelines (delete `NodeMeter` and friends),
   - 3b realtime (delete `CheckQuota` and the spend half of `OnResponse`; `OnResponse` keeps logging),
   - 3c batches (delete `SubrequestMiddleware`; sub-requests run with a tenant-only context at
     `adapter/batches.go:~537`, which is all `Admit` needs),
   - 3d direct HTTP (add `writeQuotaError` to the six engine-error blocks, wire embeddings through
     `Engine.Admit/Charge`, delete `QuotaEnforce`, `spendHook`, `chargeSpend`). The engine endpoint and
     streaming become metered here; the step-0 markers flip.
4. **(Optional, separate changelog entry)** native provider quota error shapes.
5. **Invariant tests land** (below), and an AST test forbids `AllowRequest|CheckSpend|AddSpend` calls
   outside `internal/quota` and `server/meter.go`.

### Test strategy (the invariants)

- **Deny-all meter:** with a meter that refuses everything, every route whose policy (R1) says `Billable`
  returns its quota status for a representative request, and no route without `Billable` does.
  Table-driven over the route table plus one sample body per route (Gemini uses `:generateContent`).
- **Exact count:** with a counting meter, N requests produce N admits and N charges across chat,
  messages, gemini (+stream), bedrock, ollama, responses, embeddings, azure, an `n`-line batch (n admits),
  a `k`-node pipeline (k admits), realtime `response.create`, and the engine endpoint.
- **Parity:** old and new charge for a fixed request set agree for non-streaming bodies (tolerance noted
  for Anthropic thinking and caching).
- The AST test above.

### Performance

Hot path in the engine. Guard: `BenchmarkProcessRequest_StaticResponse` (577 B/op, 10 allocs/op in
`docs/benchmarks/latest.md`) and siblings stay exact on allocs/op with `Meter==nil` and with a meter set
for `tenant==""`; `Admit`/`Charge` must not allocate on the deny-free path. The enforcer's single mutex
(`quota.go:155`) is unchanged and remains a contention point at very high tenant request rates; not worse
than today.

### Risks and open questions

- **Deliberate behaviour changes** (all need a CHANGELOG entry): malformed requests and unknown-agent 404s
  no longer consume rate tokens (they never reach `Admit`); streaming responses now accrue spend, so
  stream-heavy tenants hit monthly caps sooner; realtime spend rises roughly 30 % (word count to the 1.3x
  heuristic); the engine endpoint becomes rate-limited.
- **Open:** keep `Meter.Charge` inside the engine (canonical estimate, closes the streaming gap, but
  Anthropic thinking/cache adjustments are not reflected in spend) or charge from the adapter with exact
  wire usage (requires R3 first)? Recommendation: engine, accept the discrepancy, document it.
- **Open:** is charge-queue loss on crash acceptable? The ledger is already a soft cap (`quota.go:7-8`:
  "concurrent requests can exceed a cap"); recommendation yes, flush on `Shutdown`.
- Replicas: rate buckets stay process-local (R9).

**Effort: M** (largest M; six PRs).

---

## R3: Adapter dispatch

### Problem

Seven call sites reach `Engine.ProcessRequestContext` from adapters: OpenAI chat (`adapter/openai.go:185`),
Anthropic (`anthropic.go:223`), Gemini (`gemini.go:236`), Bedrock (`bedrock.go:130`), Ollama
(`ollama.go:102`), Responses (`responses.go:359`) and the realtime generator (`realtime.go:542`). The six
HTTP handlers are the same skeleton, copied. Verified occurrence counts, per handler:

| Skeleton step | Copies | Locations |
|---|---|---|
| Stamp `meta.Protocol` before decode | 6 | `openai.go:144`, `anthropic.go:180`, `gemini.go:176`, `bedrock.go:108`, `ollama.go:67`, `responses.go:249` |
| Decode + 413-vs-400 split + `defer Body.Close()` | 6 (+ count_tokens/gemini 2 more) | `openai.go:149-157`, `anthropic.go:185-193`, `gemini.go:207-215`, `bedrock.go:112-120`, `ollama.go:71-79`, `responses.go:254-262` |
| Session id | 6 | five call `extractSessionID` (`openai.go:526`); `responses.go:347` reads the header directly |
| Chaos error block: `AsChaosError`, connection fault with 502 fallback, `Retry-After`, status mapping | 6 | `openai.go:190`, `anthropic.go:228`, `gemini.go:241`, `bedrock.go:135`, `ollama.go:107`, `responses.go:364` |
| Strict-tool error mapping | 6, **three different shapes** | OpenAI/Anthropic/Gemini/Responses use a typed writer (`writeOpenAIStrictError` etc.); Bedrock and Ollama return a plain 400 with `err.Error()` (`bedrock.go:148`, `ollama.go:120`) |
| Stamp agent/scenario/tool-count into `RequestMeta` | 6 | `openai.go:216-221` ... `responses.go:386-392` |
| Hallucination, strict-violation, image-count headers | 6 / 6 / 5 | `openai.go:223-225` ... `responses.go:445-446` (Responses sets no image-count header) |
| Resolve `StreamingConfig` by re-looking up the agent from the request model, with a "single visible agent" fallback | 4 | `openai.go:235`, `anthropic.go:270`, `gemini.go:287`, `responses.go:464`; `server.go:645` (`handleStreamResponse`) does it a fifth time using `resp.AgentName` instead |

Consequences actually observable in the code:

- **Drift.** Bedrock and Ollama strict errors lost their `param`/`code` structure and Responses omits the
  image-count header. The next provider will copy whichever handler is nearest.
- **Bedrock streaming is a re-parse hack.** `HandleConverseStream` runs `HandleConverse` into a capture
  writer, unmarshals its own JSON response, then re-emits eventstream frames (`bedrock.go:303-345`).
- **Streaming config can disagree with the matched agent.** The engine resolves agent by name, then model,
  then (anonymous only) the single visible agent (`engine/engine.go:503-530`); four adapters re-derive the
  agent by model with a fallback that also applies to tenant callers. They agree today only because the
  inputs coincide (inferred); the engine already knows the answer (`Response.AgentName`).
- Embeddings, moderations, Cohere, vector and search handlers carry a lighter copy of the same prologue
  (protocol stamp and decode).

### Proposed design

```go
// internal/adapter/dispatch.go
type provider interface {
    Protocol() string                                            // ProtocolOpenAIChat, ...
    // Decode parses and validates the wire request into d. A non-nil *wireError is rendered as-is.
    Decode(r *http.Request, d *decoded) *wireError
    // Render writes the non-streaming response.
    Render(w http.ResponseWriter, d *decoded, resp *engine.Response)
    // Stream writes the streaming response (R4). Providers without streaming return errNoStream at Decode.
    Stream(ctx context.Context, w http.ResponseWriter, d *decoded, resp *engine.Response) error
    // WriteError maps every engine error class (chaos, strict, quota, not-found, other) to the wire.
    WriteError(w http.ResponseWriter, d *decoded, err error)
}
// Optional hooks, discovered by type assertion so simple providers stay small:
type preparer interface{ Prepare(d *decoded, resp *engine.Response) } // OpenAI response_format, Anthropic cache usage
type finisher interface{ After(d *decoded, resp *engine.Response) }   // Responses: store the turn, append to a conversation

type decoded struct {
    Inbound *engine.InboundRequest
    Model   string
    Stream  bool
    Images  int
    Usage   engine.Usage        // prompt-token estimate from the flattened messages
    Ext     any                 // provider scratch (e.g. the typed request for Prepare/Render)
}

func dispatch(eng *engine.Engine, p provider) http.HandlerFunc
```

`dispatch` is the single implementation of: protocol stamp, `decoded` from a `sync.Pool`, session extraction
(R8's `sessionFrom(r)`), `ProcessRequestContext`, `meta` stamping on both success and error, error rendering
via `p.WriteError`, the three response headers, `Prepare`, then stream-or-render. Provider-specific logic
stays in the provider: Anthropic's `checkAnthropicAuth` (`anthropic.go:611`) becomes part of `Decode`;
Bedrock registers two routes over one type with a `stream bool` field, so the capture-and-reparse hack
disappears; Gemini's `stream` is derived from the method segment in `Decode`; Ollama's default-stream
semantics (`req.Stream == nil || *req.Stream`) stay in its `Decode`.

Two small engine changes make the providers thinner and remove the fifth lookup:

- `engine.Response` gains `Streaming *types.StreamingConfig` (taken from the agent the engine actually
  resolved) and `Usage` (lazy, memoised; see R2's `internal/tokens`). The four `GetByModelForTenant`
  lookups and `server.handleStreamResponse`'s lookup are deleted.
- Realtime is **not** folded in: it has no request/response skeleton, only the generator closure
  (`realtime.go:534`), which is metered by R2 and session-keyed by R8.

**Alternatives considered.** (a) Embedding a base struct with helper methods: removes the duplicated
helpers but not the duplicated *ordering*, which is where the Bedrock/Ollama drift came from. (b) Code
generation from a spec: heavier than six providers justify. (c) Leave the handlers and add a lint:
cannot detect behavioural drift.

### Migration plan

0. **Golden wire fixtures** recorded from current code for every provider x scenario of the matrix below
   (ids and timestamps normalised). They run against the old handlers first, so a later PR that changes
   bytes shows up as a golden diff, not a surprise. Add `BenchmarkHandler_*` (P0).
1. **Extract helpers without changing structure:** `decodeOrWrite`, `stampMeta`, `applyResponseHeaders`,
   `streamingConfigFor(resp)`; replace the six copies one-for-one. Behaviour-preserving; goldens guard it.
2. **Matrix test lands with an allowlist** of providers not yet ported; the allowlist is a ratchet that may
   only shrink.
3. **Introduce `dispatch`/`provider` and port, one PR each:** Ollama (smallest), Bedrock converse then
   stream (deletes the capture hack, `bedrock.go:303-345` and `bedrockCapture`), Gemini, Anthropic, OpenAI
   chat, Responses last (needs `finisher`).
4. **Engine:** `Response.Streaming` and `Response.Usage`; delete the four lookups and
   `handleStreamResponse`'s.
5. **Normalise strict-tool error shapes** for Bedrock/Ollama to their providers' native form (a deliberate
   fidelity fix, separate PR and changelog).

### Test strategy (the invariant)

A matrix test over `adapter.DefaultRegistry(...).Adapters()` x cases, each provider supplying fixtures via a
tiny `conformanceFixtures` interface: {valid, malformed JSON, oversize body (413), missing model, empty
messages, unknown agent (404), chaos status with `Retry-After`, chaos connection fault, strict-tool 400,
quota denial (R2), stream, hallucination header, engine error after streaming started}. For every cell:
`meta.Protocol` stamped even on decode failure; `meta.AgentName` stamped on engine failure; the error body
parses in that provider's envelope; `Retry-After` forwarded; `X-Mockagents-*` headers present;
`X-Session-Id` reaches the engine. A provider absent from the matrix fails
`TestEveryProviderIsInTheMatrix` (enumerates the registry).

### Performance

Per-request overhead of one interface dispatch is negligible next to JSON decode; the risk is allocations:
`decoded` must come from a pool and `Ext` must not force the typed request to escape twice. Guard:
`BenchmarkHandler_*`, `BenchmarkDecodeJSONBody_Pooled` (the pooled decode is worth -39 % B/op per
`CLAUDE.md`), `BenchmarkWriteJSON`; allocs/op exact per `benchguard`.

### Risks and open questions

- Responses and Anthropic carry the most provider-specific logic (conversation store, prompt-cache
  accounting); if `preparer`/`finisher` multiply, the interface is the wrong shape. Port them last and be
  willing to leave Responses on `dispatch` with a fat `finisher`.
- Goldens must not freeze existing bugs; each golden diff in steps 3-5 is reviewed as a fidelity decision.
- The `Adapter`/`Route` registry (`registry.go:24`) stays; `provider`s are wrapped into `Route`s by a small
  constructor so R1 policy declarations are unaffected.

**Effort: L.**

---

## R4: Exported stream plan

### Problem

Stream timing and chunking physics exist in one place, but only three of the six streaming protocols use
it, and chunking exists in three flavours.

- `streamPacer` is private (`streaming/pacing.go:29`) and used by `StreamOpenAI` (`openai.go:100`),
  `StreamAnthropic` (`anthropic.go:128`), `StreamGemini` (`gemini.go:76`). Its own comment claims it is shared
  "by every protocol's Stream* function" (`pacing.go:23-25`); it is not.
- **Responses** (`adapter/responses_stream.go`, the private `streamResponses`) never creates a pacer: no
  TTFT, no tokens-per-second, no jitter, no load-target distribution, no truncate/malformed faults. It
  re-implements the chunk-size/delay defaults (`:42-52`) and its own sleep (`sleepResponses`, `:237`).
- **Bedrock** emits one delta per content block with no pacing at all (`bedrock.go:~325-345`), after
  re-parsing its own non-streaming response.
- **Ollama** emits exactly two NDJSON frames (`ollama.go:149-156`), no chunks, no pacing.
- **Faults skip tool calls.** In `StreamOpenAI`, `truncateAfter` only counts text chunks
  (`openai.go:~121-133`), so a tool-only response never truncates; `malformed` returns before tool calls
  (`openai.go:~159`); tool-argument frames pace with the fixed `delayMs`, not the pacer (`openai.go:~240-250`).
  Anthropic does the same (`anthropic.go:244`) and Gemini sleeps a fixed `delayMs` per function call
  (`gemini.go:~149`).
- **Duplicated chunk-size/delay defaulting:** `streaming/openai.go:86`, `anthropic.go:115`, `gemini.go:65`,
  `adapter/responses_stream.go:42`.
- **Three chunkers with different fidelity:** `streaming.Chunker` (whitespace-preserving, `chunker.go:34`),
  `streaming.ChunkArguments` (rune-safe bytes, `openai.go:281`; the hard-coded size `20` appears at
  `openai.go:245`, `anthropic.go:239`, `adapter/responses_stream.go:196`) and `realtime.chunkText`
  (`realtime/session.go:2018`), which splits on `strings.Fields` and re-joins with single spaces: the exact
  whitespace-flattening defect the Chunker fixed in H-07 (`chunker.go:26-33`).
- `tokenLen` (`pacing.go:15`) is a fourth word-counting heuristic.

### Proposed design

Compute *what* to emit once, as data; let each protocol only decide *how it looks on the wire*.

```go
// internal/streaming/plan.go
type StepKind uint8 // Text | Refusal | ToolStart | ToolArgs | ToolEnd | Finish | Usage
type Step struct {
    Kind   StepKind
    Index  int      // content-block / tool-call index
    Text   string   // delta payload (text chunk or argument fragment)
    Tokens int       // pacing weight (replaces tokenLen)
    Call   *Call     // for ToolStart: id, name
}
type Plan struct {
    Steps []Step
    cfg   pacing    // TTFT, tokens/sec, jitter, ITL/TTFT lognormal, truncate_after, malformed
}
func NewPlan(resp *engine.Response, cfg *types.StreamingConfig, o ...Option) *Plan

type Emitter interface {
    Open(p *Plan) error                // headers, first frame; called after TTFT
    Emit(s Step) error                 // one wire frame (or group) for one step
    Fault(kind FaultKind) error        // truncate: stop cleanly; malformed: protocol-appropriate bad frame
    Close(p *Plan) error               // trailer ([DONE], message_stop, eventstream end)
}
func (p *Plan) Run(ctx context.Context, e Emitter) error
```

- `Plan.Run` owns TTFT, per-step delay, deterministic jitter, the load-target distribution, truncation and
  malformed injection, and context cancellation; the code in `streamPacer` moves inside it unchanged
  (including the fixed-seed `rng` vs per-stream `distRng` split that FB-05 depends on).
- Faults apply to **all** step kinds, so truncating a tool-only stream is expressible. `Fault(Malformed)` is
  per protocol: SSE writes `{"mockagents_fault":"malformed",` (today's `writeStop`, `pacing.go:170-178`);
  NDJSON and eventstream get an equivalent that their decoders reject.
- One chunk-size/delay default site; one argument-fragment size constant, configurable per plan; one
  chunker (`Chunker` for text, `ChunkArguments` for bytes). `realtime.chunkText` is replaced by the
  `Chunker` (and gains whitespace fidelity).
- Emitters: OpenAI, Anthropic, Gemini (ported from today's functions), **Responses, Bedrock (eventstream),
  Ollama (NDJSON)** new. Emitters are the only protocol-specific code and live where they live today
  (`streaming/*.go`, plus `adapter` for Responses until it is moved into `streaming`).
- R3 integration: `provider.Stream` becomes `streaming.NewPlan(resp, resp.Streaming).Run(ctx, emitterFor(d))`.

**Alternatives.** (a) Export `streamPacer` and have each emitter call it: fixes visibility but not tool
calls, chunk defaults or the three chunkers. (b) Interpreter pattern with callbacks per frame type: same as
`Emitter` but without the precomputed plan, which is what makes a stream testable without sleeping
(`Plan.Steps` can be asserted directly).

### Migration plan

0. **Golden stream fixtures** per protocol with `chunk_delay_ms: 0` and fixed config (SSE text, NDJSON,
   eventstream bytes), plus `BenchmarkStream_*_NoDelay` (P0).
1. **`Plan` + `Run` + OpenAI emitter;** `StreamOpenAI` becomes a three-line wrapper. Guard: all of
   `pacing_test.go`, `pacing_dist_test.go`, `parity_test.go`, `semantic_test.go`, `server_integration_test.go`
   pass unchanged; goldens identical.
2. **Anthropic and Gemini emitters.** Same guards.
3. **Responses emitter.** *Behaviour addition:* Responses streams gain TTFT, jitter, load-target and fault
   injection. Agents without streaming fields are unchanged (default `ChunkDelayMs` and `TTFTMs` 0).
4. **Bedrock and Ollama emitters** (after R3 step 3 so Bedrock has a native stream path). Behaviour change:
   they start honouring `spec.behavior.streaming`; a deliberate fidelity improvement, changelog entry.
5. **Delete duplicates:** `chunkString`, the four default blocks, `sleepResponses`, `realtime.chunkText`,
   private `streamPacer` (kept as `Plan` internals). Guard: `chunker_fidelity_test.go` extended to realtime.

### Test strategy (the invariants)

- **Stream/non-stream equivalence, every protocol:** concatenated text deltas equal the non-streaming
  content byte for byte; concatenated tool-argument fragments equal the non-streaming arguments; finish
  reason and usage match.
- **Fault matrix:** every protocol x {truncate_after_chunks, malformed} x {text-only, tool-only,
  text+tool}: a faulted stream never contains a finish frame or `[DONE]`; truncation is reachable for tool-only
  responses.
- **Emitter completeness:** each emitter handles every `StepKind` (test enumerates the kind constants via a
  `stepKindCount` sentinel).
- **Realtime parity:** `chunkText` replacement preserves whitespace (extends `chunker_fidelity_test.go`).

### Performance

Streams are sleep-bound, so the CPU path matters only for load-target runs at high concurrency (FB-05).
`NewPlan` allocates the step slice once (today `Chunk` already allocates a slice per stream); watch
allocs/op on `BenchmarkStream_*_NoDelay`, compared against step 0, via `benchguard`.

### Risks and open questions

- Emitters for NDJSON and eventstream need a defined malformed-frame form; I propose a truncated JSON
  object for NDJSON and a frame with a deliberately wrong CRC for eventstream. Needs a decision, since real
  SDK decoders react differently.
- Step ordering for providers that interleave text and tool deltas differently (Anthropic content-block
  indices) must be captured as data in `Step.Index`, not re-derived in the emitter.
- Realtime keeps its own event machine; adopting `Plan` there is optional (its pacing is a constant
  5 ms, `adapter/realtime.go:35`) and not scheduled.

**Effort: M.**

---

## R5: Bounded store primitive

### Problem

Fourteen hand-rolled stores or caches exist (counting the two batch stores separately), with at least five
different behaviours at the cap and only one store with a byte budget (A2A). Inventory (verified):

| Store | Cap and scope | Policy | TTL | Byte budget | Notes |
|---|---|---|---|---|---|
| Engine sessions `MemoryStore` (`engine/state/store.go:48`) | 100,000, global | batch LRU: sorts **all** entries by last access and drops the oldest 1/16, under the write lock (`:82-115`) | 30 min | per-session history count only | LRU order comes from session-local `lastAccess` to keep hits on the read lock (audit M-20, `:138-146`) |
| Responses (`adapter/responses.go:166`) | 1024, **global** | FIFO | none | none | key includes tenant, cap does not: one tenant's writes evict everyone's stored responses (`:195-206`) |
| Anthropic cache tracker (`anthropic.go:129`) | 4096, **global** | FIFO | none | none | same tenant-blind cap |
| Files (`files.go:55`) | 256 per tenant | FIFO | none | **none**; 50 MiB per file (`:29`) | worst case 256 x 50 MiB = 12.5 GiB per tenant |
| Batches / Anthropic batches (`batches.go:196`, `anthropic_batches.go:212`) | 256 per tenant | FIFO | none | none | `put` appends to `order` without an existence check, unlike `fileStore.put`/`responseStore.put` |
| Conversations (`conversations.go:194`) | 256 per tenant; 4096 items each | FIFO | none | none | same missing existence check |
| Realtime minted keys (`realtime.go:90`) | 1024, global | **reject new** | per entry | none | full-map scan on every mint |
| MCP streamable sessions (`mcp/streamable.go:506`) | 256 | **reject new** (P-16) | 30 min idle | none | `order` slice with O(n) removal |
| A2A tasks (`a2a/server.go:142`) | 10,000 | reject (`transitionLocked`, `:451`) | 30 min | **64 MiB** | the only store with a byte budget (P-09); full prune scan per transition |
| Auth cache (`tenancy/auth_cache.go:37`) | 1024 | **random** eviction, expired first | 5 min | none | separate negative-result map |
| Engine regex/lower/template caches (`scenario_matcher.go:30-34`, `response_generator.go:~146-155`) | none | none | none | none | justified in comments by "static scenario text"; the key space is whatever agent definitions exist |
| Realtime session items (`realtime/session.go:264`) | 4096 | drop oldest | session | audio buffer capped separately (`:232`) | |
| Quota buckets / spend cache (`quota/quota.go`) | none | none | none | none | bounded only by tenant count |

The `order = order[1:]` FIFO idiom is copied six times (`responses.go:203`, `anthropic.go:151`,
`files.go:82`, `batches.go:221`, `conversations.go:219`, `anthropic_batches.go:237`).
Each copy re-decides what happens at the cap, and several decide wrongly (global caps that cross tenants,
no byte budget where payloads are megabytes, duplicate `order` entries).

### Proposed design

A small generic package; a store *composes* it rather than inheriting from it.

```go
// internal/bounded: leaf, stdlib only.
type Policy uint8 // LRU | FIFO | RejectNew | SampledLRU

type Config[K comparable] struct {
    Policy     Policy
    MaxEntries int                  // 0 = unlimited
    MaxBytes   int64                // 0 = unlimited; needs a sizer
    TTL        time.Duration        // 0 = none
    Sliding    bool                 // TTL refreshed on Get
    Partition  func(K) string       // per-tenant (or per-anything) sub-budgets
    PerPart    Limits               // {MaxEntries, MaxBytes} per partition, applied in addition to the globals
    Shards     int                  // power of two; 1 = single lock
    Now        func() time.Time
    OnEvict    func(K, any, Reason) // Reason: Capacity | Bytes | Expired | Replaced | Rejected
}
type Store[K comparable, V any] struct{ /* ... */ }
func New[K comparable, V any](cfg Config[K], sizeOf func(V) int64) *Store[K, V]
func (s *Store[K, V]) Get(k K) (V, bool)
func (s *Store[K, V]) Put(k K, v V) (admitted bool)   // false only under RejectNew
func (s *Store[K, V]) Delete(k K) bool
func (s *Store[K, V]) Len() int; func (s *Store[K, V]) Bytes() int64
func (s *Store[K, V]) Sweep() int                      // expire; also called lazily
func (s *Store[K, V]) Stats() Stats                    // counters per Reason, exported to internal/metrics
```

- **`SampledLRU`** exists because a conventional LRU list needs a write lock on every hit, which is exactly
  what audit M-20 removed from the session store. `SampledLRU` takes an entry-supplied last-access time
  (an optional `Touched` interface the session satisfies) and, at capacity, evicts the oldest of *k* random
  samples (k=16) instead of sorting everything. Sharding bounds lock contention.
- **Partitioning** is first-class: `Partition: func(k) tenant` plus `PerPart` fixes the cross-tenant
  eviction of responses and the Anthropic cache, and gives files a per-tenant byte budget.
- Stats feed `internal/metrics`, so "how many stored responses were evicted for capacity" becomes
  observable instead of invisible.

**What does not fit, deliberately:** quota buckets (token-bucket state, not a cache) and the engine
regex/template caches stay as they are, with a note (Risks) to re-justify the "key space is static" claim
if agent definitions become tenant-writable at volume.

### Migration plan

1. **Land `internal/bounded` with no consumers.** Model-based property test against a naive reference,
   covering every `Policy` x TTL x partition combination; benchmarks.
2. **Responses store and Anthropic cache tracker** (behaviour fix: per-tenant partition). Guard:
   `responses_test.go`, `anthropic_test.go`, new cross-tenant eviction test.
3. **File store** (adds a per-tenant byte budget; default proposed `MOCKAGENTS_FILE_STORE_MAX_BYTES`
   = 1 GiB, an open question) and **batch, Anthropic-batch, conversation stores** (fixes the duplicate-order
   bug). Guard: `files_test.go`, `batches_test.go`, `conversations_test.go`.
4. **A2A tasks, MCP sessions, realtime minted keys** with `RejectNew`. Guard: the existing P-09/P-16 tests
   (`session_expiry_test.go`, a2a capacity tests).
5. **Auth cache** (`SampledLRU`, expired-first preserved by TTL sweep).
6. **Engine `MemoryStore`** last, behind the benchmark gate (below).

### Test strategy (the invariants)

- **Bounds hold:** for every store, a randomised workload never exceeds `MaxEntries`/`MaxBytes`, globally and
  per partition (property test in `bounded`, plus one smoke test per consumer that asserts the configured
  numbers are the ones wired).
- **Tenant isolation:** writes by tenant A never evict tenant B's entries while B is under its partition
  budget.
- **No hand-rolled FIFO:** a repository test greps non-test Go files for `= .*order\[1:\]` outside
  `internal/bounded`; the pattern is the signature of the idiom above.
- **TTL + clock:** all expiry tests use the injected `Now`, never `time.Sleep`.

### Performance

The session store is on the engine hot path (`GetOrCreate` fast path, `store.go:138-146`). Gate: the
`BenchmarkProcessRequest_*` family must keep exact allocs/op (`bounded.Store` generic boxing is the
risk), and the new `BenchmarkSessionStore_GetOrCreate_Parallel` (P0) must not regress, which is the
direct check that M-20 is not re-introduced. If the generic version cannot meet that, the session store
keeps its own structure and only adopts the sampling eviction helper.

### Risks and open questions

- Default byte budgets are new configuration; a default that is too low breaks large batch workflows
  (`maxBatchRequests = 50000`, `batches.go:~31`). Decide defaults with the maintainers.
- Policy changes (global to partitioned caps; FIFO to LRU) alter which entries survive under pressure;
  none of them are contractual, but changelog them.
- Engine caches: confirm whether per-tenant agent counts are bounded (`agent_write_handlers.go`); if not,
  the unbounded `sync.Map` caches become a tenant-reachable growth path (inferred).

**Effort: M.**

---

## R6: JSON-RPC core

### Problem

MCP and A2A each implement the JSON-RPC 2.0 envelope, and the copies disagree on details a client can see.

| Aspect | MCP (`internal/mcp`) | A2A (`internal/a2a`) |
|---|---|---|
| Types | `Request`/`Response`/`RPCError` (`jsonrpc.go:30,59,67`) | `rpcRequest`/`rpcResponse`/`rpcError` (`server.go:40,47,57`) |
| Response `id` | `json:"id,omitempty"`; null substituted in `newError` (`jsonrpc.go:77-80`) | `json:"id"` (nil marshals to `null`) |
| Entry | `Handle` (`server.go:137`), `HandleBytes` (`:237`) | `HandleBytes` (`:244`), `dispatch` (`:270`) |
| Null id | rejected as Invalid Request (`server.go:164`) | treated as a request |
| Batch | rejected, message cites MCP revision (`server.go:238`, `streamable.go:230-238`) | rejected (`server.go:~247-251`) |
| Panics | one recover, tool handlers only (`server.go:649-655`) | none |
| Context | none: `Handle(req)` has no `ctx`; tool handlers get `context.Background()` (`:655`) | none |
| Body read + cap | four transports (`http.go:57`, `http.go:147`, `sse.go:151,214`, `streamable.go:222`) + stdio frame reader (`stdio.go:55`) | `readBounded` (`server.go:835`) |
| Chaos wrapper re-probes id/method | `mcp/chaos.go:32,196` | `applyChaos` (`server.go:602`, `RPCHandler` `:567`) |

What is wrong today because of it (verified): `recover()` exists in only three places in the repository
(`audit/async.go:98`, `mcp/server.go:651`, `server/middleware.go:158`). A panic in any non-tool MCP method on
stdio ends the session (the loop at `stdio.go:55-90` has no recover), and a panic in an A2A handler resets the
connection with no JSON-RPC error because the A2A mux (`cmd/mockagents/a2a.go:105`) does not install
`server.Recovery`. Cancellation never reaches a tool handler. The envelope rules (null id, missing method,
notification handling) each had to be fixed twice (round-10 R10-7/8/9/12 in MCP; L-39 in A2A; P-12;
L-38 for the recover).

### Proposed design

```go
// internal/jsonrpc: leaf (encoding/json, context, log/slog).
type Request struct {            // union: call, notification, or client response to a server-initiated call
    JSONRPC string; ID json.RawMessage; Method string; Params json.RawMessage
    Result, Error json.RawMessage
}
type Error struct{ Code int; Message string; Data any }
type Response struct{ /* id always present; exactly one of result/error enforced in MarshalJSON */ }

type MethodFunc func(ctx context.Context, params json.RawMessage) (result any, err *Error)
type Options struct {
    AllowBatch  bool                      // false: -32600 with BatchMessage
    BatchMessage string
    AllowNullID bool                      // MCP false, A2A true
    OnResponse  func(*Request)            // client reply to a server-initiated request (MCP bidirectional)
    OnPanic     func(ctx context.Context, method string, v any)
}
type Server struct{ /* method table + Options */ }
func (s *Server) Handle(method string, f MethodFunc)
func (s *Server) HandleBytes(ctx context.Context, body []byte) ([]byte, error) // nil bytes = notification
func Peek(body []byte) (id json.RawMessage, method string, ok bool)             // for the chaos wrappers
func ReadBounded(r io.Reader, max int64) ([]byte, error)
const (CodeParse = -32700; CodeInvalidRequest = -32600; CodeMethodNotFound = -32601; CodeInvalidParams = -32602; CodeInternal = -32603)
```

- Envelope rules live in exactly one place: parse error vs invalid request, batch policy, null id policy,
  notification detection, id echo (number/string preserved verbatim, null when unknown), "client response is
  never answered".
- **Panic recovery is in the core:** every `MethodFunc` runs under a `recover` that logs the stack and returns
  `-32603` with a generic message. `OnPanic` lets MCP count it.
- **Context** is passed from the transport (`r.Context()`, the stdio session context, the SSE request
  context) into the method, so `mcp.ToolHandler(ctx, args)` finally receives a cancellable context and
  `callToolHandler`'s `context.Background()` goes away.
- Protocol-specific error codes stay in each package (`ErrResourceNotFound = -32002`, A2A
  `errTaskNotFound`), built on `jsonrpc.Error`.
- MCP keeps its exported names via aliases (`type Request = jsonrpc.Request`, `type Response = jsonrpc.Response`)
  because `Handle(req *Request) *Response` is part of the in-process API used by tests and
  `mcpadmin`; a `Handle(ctx, req)` is added and the old signature kept as a shim for one release.

### Migration plan

1. **Add `internal/jsonrpc` with its own conformance suite** (below) and tests for `Peek`/`ReadBounded`.
2. **Port A2A** (four methods, smallest): delete `rpcRequest`/`rpcResponse`/`newError`; `RPCHandler` passes
   `r.Context()`. Guard: `a2a/server_test.go`, `a2a/chaos_test.go` unchanged.
3. **Port MCP dispatch** (`Handle`, `HandleBytes`, the method switch becomes registrations) with type
   aliases; thread `ctx` through `http.go`, `sse.go`, `streamable.go`, `stdio.go`. Guard: `mcp_test.go`,
   `conformance_test.go`, `envelope_review_test.go`, `round10_test.go`, `round11_test.go`,
   `surface_test.go`, `streamable_test.go`.
4. **Chaos wrappers use `jsonrpc.Peek`/`ReadBounded`** (`mcp/chaos.go`, `a2a/server.go` `applyChaos`).
5. **Delete** the duplicated types, `isBatchBody`, `callToolHandler`'s recover (now generic), and the stale
   package doc at `mcp/jsonrpc.go:1-8` ("streaming notifications are out of scope").

### Test strategy (the invariant)

A shared `jsonrpctest.Suite(t, target)` run against **every** JSON-RPC surface: A2A HTTP, MCP legacy HTTP,
MCP streamable, MCP SSE, MCP stdio. Cases: parse error (id null), invalid request (wrong version, missing
method, non-object), batch (policy-dependent), notification yields no output, id echoed verbatim for
number, string, and large integer, unknown method, invalid params, **a method that panics returns `-32603`
and the next request on the same connection/session still works**, context cancellation observed by the
method, response never contains both or neither of result/error. A new surface that is not in the list of
targets fails `TestEverySurfaceIsCovered` (enumerating handlers registered in `cmd/mockagents`).

### Performance

Not on the committed baseline and not request-hot at mock scale; `json.RawMessage` ids are copied once as
today. No gate beyond P0 if desired.

### Risks and open questions

- Error `data` strings differ between the two servers today (`err.Error()` echoed); unify to the minimum
  (no Go error text) or keep? Recommend keep for parse errors (client debugging), drop for internal errors.
- The SSE/streamable bidirectional paths (`SendRequest`, `DeliverResponse`) stay in `mcp`; only the envelope
  moves.

**Effort: M** (small core, wide port).

---

## R7: One load-and-validate entry point

### Problem

Seven kinds of document, and every caller decides which validators to run, which defaults to apply, and
whether a bad document is fatal.

Kind enumerations that must be edited together when a kind is added: `appendDocument`
(`config/loader.go:372`), `ValidateBytes` (`validate_bytes.go:41`), `validateDocuments` (in the CLI package,
`cmd/mockagents/validate.go:220`), `Documents.Count/Merge` (`loader.go:435,444`), `documentKinds`
(`loader.go:572`).

Callers, verified:

| Surface | Load | Validators run | Bad document | Lint | Cross-document |
|---|---|---|---|---|---|
| `validate` (`validate.go:220-258`) | `LoadDocumentFile` per file | all 7 kinds | reported | yes | yes |
| `test` (`test.go:62-140`) | `LoadAllDocuments` | agents, pipelines, test suites (not MCP/A2A/vector/search) | fatal | no | yes |
| `start` (`start.go:155-190`, `:499`, `:521`, `:619`) | `LoadAllDocuments` | agents, then pipeline/vector/search inside `register*` helpers; **no test suites, MCP or A2A** | skipped with warning | no | **no** (`ValidateDocuments` is not called) |
| `mcp` (`mcp.go:83,160,173`) | `LoadAllDocuments` | selected MCP server; agents when `--manage` | server fatal, agents skipped | no | no |
| `a2a` (`a2a.go:58,98`) | `LoadAllDocuments` | selected A2A server | fatal | no | no |
| `contract` (`contract.go:83-90`) | `LoadFile` | agent only | fatal | no | no |
| server reload/watch (`handlers.go:340-361`, `watcher.go:250-268`) | `LoadFile`/`LoadDir` | agent (`Validate` incl. unknown-field check) | skipped | no | no |
| server write APIs (`agent_write_handlers.go:287`, `pipeline_handlers.go:293`, `validate_handler.go:85`, `mcpadmin/manager.go:158,251`) | bytes | `ValidateBytes`, **plus** a separate `UnknownAgentFields` call on the raw body (`agent_strict_fields.go:28`, `manager.go:228`) because re-marshalling drops unknown keys first | rejected | agent only | no |
| Go SDK in-process (`sdk/go/mockagents/inprocess.go:68-77`) | `LoadDir` | **none**, and no `ApplyDefaults` | n/a | no | no |

Consequences: the strict unknown-field check (a maintainer decision: unknown keys are errors) lives inside
each per-kind validator (`validator.go:91`, `pipeline_validator.go:40`, ...), so any surface that skips a
validator skips the decision. `start` serves directories that `validate` rejects (duplicate names and
dangling pipeline/test refs only surface in `validate` and `test`). `ApplyDefaults` takes only an
`AgentDefinition` and is called from eleven sites (`start.go:170`, `test.go:73`, `validate.go:230`, `mcp.go:173`,
`contract.go:87`, `handlers.go:344,361`, `watcher.go:268`, `agent_write_handlers.go:278`,
`mcpadmin/manager.go:245`, `agent_revision.go:138`), and not at all by the SDK.

### Proposed design

```go
// internal/config/pipeline.go (naming: "load_validate.go")
type Mode uint8
const (
    Strict  Mode = iota + 1 // any error makes the report invalid; callers refuse to proceed
    Lenient                 // invalid documents are dropped from the result and listed in Report.Skipped
)
type Options struct {
    Mode       Mode
    Lint       bool          // include non-fatal lint warnings
    Cross      bool          // cross-document references and name collisions (ValidateDocuments)
    AllowEmpty bool
    Kinds      KindSet       // restrict (default: all); e.g. mcp command validates MCPServer only
}
type Report struct {
    Errors     []*ValidationError
    Warnings   []*ValidationError
    LoadErrors []error
    Skipped    []SkippedDocument   // Lenient only: file, kind, reason
    Files      int
}
func (r *Report) Valid() bool

func LoadAndValidate(paths []string, opts Options) (*Documents, *Report)     // dirs or files
func LoadAndValidateBytes(data []byte, opts Options) (*Documents, *Report)   // write APIs; does the unknown-field pass on the RAW body
```

- A single **kind table** (`kindSpec{Name, Decode, Validate, ApplyDefaults, Bucket}`) drives `appendDocument`,
  `ValidateBytes`, `Count/Merge`, suggestions and the validate pass. Adding a kind without a validator is a
  compile error (field is required) plus a test over `documentKinds`.
- `ApplyDefaults` becomes part of the pipeline; callers no longer call it.
- `Lenient` returns only accepted documents, so `start` cannot register what validation rejected;
  `Strict` is `validate`/`test`/`contract` semantics.
- `ValidateBytes` stays as a thin wrapper (it is a public helper for the GUI editor, `CLAUDE.md`), now built on
  the kind table.

**Alternatives.** Keep per-caller choices but add a `validateAll` helper in `config`: removes the CLI-only
location but not the per-caller *choice* of which pass to run, which is the defect.

### Migration plan

1. **Move `validateDocuments` from `cmd/mockagents/validate.go` into `config`** as `ValidateSet(docs, Options)`.
   Pure move, tests move with it. Guard: `validate_test.go`, `config/*_test.go`.
2. **Kind table;** rewire `appendDocument`, `ValidateBytes`, `Count/Merge`, `documentKinds`. Guard:
   `loader_test.go`, `validate_bytes_test.go`, new `TestEveryKindHasValidator`.
3. **`LoadAndValidate`;** port `validate` (Strict + Lint), `test` (Strict), `contract` (Strict, one file).
4. **Port `start` (Lenient, Lint, Cross as warnings), `mcp`, `a2a`.** *Behaviour change:* `start` now logs
   cross-document findings and lint warnings and also validates test suites and MCP/A2A documents it ignores
   today; it still skips rather than refuses (no new startup failures).
5. **Server surfaces:** reload/watch use a one-file `LoadAndValidate`; the write APIs call
   `LoadAndValidateBytes`, which subsumes the separate `UnknownAgentFields` step.
6. **Go SDK in-process** uses `Lenient` (adds defaults and validation it never had).

### Test strategy (the invariant)

A **surface agreement matrix:** a corpus of bad documents (one per validator rule family, one unknown field per
kind, one dangling reference, one duplicate name, one good document) is fed to every surface (`validate`,
`test`, `start` loader, `mcp`, `a2a`, `contract`, watcher reload, each write API, SDK). For each, the
accept/reject decision must agree with the surface's declared mode, and the *error text* for the same
document must be identical across surfaces (`Report` equality). `TestEveryKindHasValidator` (above) and an
AST test that forbids `Validator{}`/`Validate*(` calls from `cmd/` and `internal/server` outside
`config` complete it. `make validate` over `examples/` stays as the positive control.

### Performance

Startup and reload only; negligible. Watcher reload latency is unchanged in order (one file).

### Risks and open questions

- Whether `start` should ever *refuse* on cross-document errors (today it does not). Recommendation: warn now,
  add a `--strict` flag later.
- Duplicate names across files: confirm what `AgentRegistry.Register` does on collision before making `start`
  report it (`agent_registry_collision_test.go` documents it; not re-read for this design).

**Effort: S.**

---

## R8: Session-identity policy

### Problem

"Which conversation is this request part of" is decided in several places with slightly different rules.

| Place | Rule | Location |
|---|---|---|
| Wire extraction | `X-Session-Id` header; absent = no session | `extractSessionID` (`adapter/openai.go:526`) used by five handlers; **Responses reads the header directly** (`responses.go:347`) |
| Allowed headers | `X-Session-Id` listed for CORS | `server/middleware.go:138` |
| Engine namespacing | `tenant \x00 agent \x00 id` | `scopedSessionKey` (`engine/engine.go:542`) |
| Anonymous requests | empty id => throwaway `NewSession("", ...)`, seeded with `PriorTurns` (Responses chains/conversations) | `engine.go:324-329`, field `engine.go:57` |
| Pipeline nodes | `"%s::%s::%s"` = request-or-client id, pipeline, node | `engine/pipeline.go:365`; the id defaults to the request id (`server/pipeline_handlers.go:145`) |
| Runner cleanup | **re-derives** the same format string to delete what the executor created | `runner/runner.go:117` (+ `DeleteSession("", ...)` `:106,118`) |
| Chaos state | re-implements the NUL-separator tenant convention for agents | `engine/chaos.go:168` (comment cites `scopedSessionKey`) |
| Realtime | `"sess_" + generateID()` per socket, passed to the engine as a session id | `adapter/realtime.go:408`, `:544` |
| Interaction log | `meta.SessionID`, else the request id | `server/log_handlers.go:496-505`; the comment still says "the generated sess-* id", which `extractSessionID` stopped minting (H-06) |
| Response / conversation stores | `(tenant, id)` keys | `responses.go:180`, `conversations.go:194` |

Verified consequences: the realtime path creates a stored engine session per WebSocket and nothing deletes it
(`DeleteSession` is called only from `runner/runner.go`), so each socket pins a session until the 30-minute TTL
or LRU; the runner can silently leak or over-delete if either format string changes; `PriorTurns` exists
because Responses chains want turn-number continuity without storing a session per call (E-01).
`InboundRequest.SessionID` and `PriorTurns` are also part of the **public JSON** of `/v1/engines/process`
(`server.go:613` decodes `engine.InboundRequest` from the client), so they cannot be renamed.

### Proposed design

One key type, one extraction function, one child-derivation rule. Public request fields stay.

```go
// internal/engine/state/key.go  (leaf: engine/state is imported by engine, never the reverse)
type Key struct{ Tenant, Agent, ID string }
func (k Key) String() string        // tenant NUL agent NUL id, byte-identical to scopedSessionKey
func (k Key) Throwaway() bool       // ID == ""
func (k Key) Child(parts ...string) Key  // ID + "::" + parts..., the only place that format exists

// internal/engine/session.go
type SessionKey = state.Key         // alias: R9 changes state.Store to take it without an import cycle
func (r *InboundRequest) sessionKey(tenant, agent string) SessionKey
```

```go
// internal/adapter/session.go
func sessionFrom(r *http.Request) string  // the only reader of X-Session-Id; grep-lint enforces it
```

- `engine.go:324-330` becomes `key := req.sessionKey(tenantID, agent.Metadata.Name)`; the throwaway branch is
  preserved structurally (no `Key` is constructed for an empty id) so the alloc baseline recorded by commit
  `5d5b517` is untouched.
- `PipelineExecutor` builds node sessions with `Key.Child(pipeline, node)`; `runner` deletes with the same
  call, so the two cannot diverge. A `PipelineExecutor.SessionKeys(def, id)` helper returns the keys a run
  will create, which is what cleanup iterates.
- `chaosKey` keeps its own shape (it keys counters, not sessions) but is rebuilt from `Key` helpers so the
  convention is stated once.
- A one-table doc (`docs/guides/sessions.md`, generated from a Go table test) states, per surface, what
  identifies a conversation, whether it is throwaway, its lifetime and who deletes it. This is the answer to
  "five places deciding session keys": there is now one decision function and one table.
- **Realtime:** either call `Engine.DeleteSession` when the socket closes (needs the agent names touched,
  recorded from `resp.AgentName`), or pass `PriorTurns` and a throwaway session per response. The second
  removes the stored session entirely but changes how `turn_number` scenarios count across responses
  on one socket; open question.

**Alternatives.** A per-agent `session_scope: agent|tenant|none` setting would let authors choose whether a
`X-Session-Id` is shared across agents. Not needed to resolve E-01/E-23 and changes the isolation contract
(X-03); recorded as a future option, not proposed.

### Migration plan

1. **`adapter.sessionFrom`** replaces `extractSessionID` and the direct read in `responses.go:347`. No
   behaviour change. Guard: existing adapter tests; new grep test forbidding `Header.Get("X-Session-Id")`
   elsewhere in `adapter`.
2. **`state.Key`, `engine.SessionKey`;** `scopedSessionKey` becomes `Key.String()`. Store stays string-keyed
   (`GetOrCreate(key.String(), ...)`). Guard: `tenant_test.go`, `anonymous_session_test.go`,
   `session_concurrency_test.go`, `state/*_test.go`; a test asserts `Key.String()` equals the old
   concatenation for a table of inputs including NUL-adjacent and empty fields.
3. **`Key.Child` for pipelines and runner cleanup.** New property test: run a pipeline, run
   `cleanupCaseSessions`, `Store.Count() == 0`.
4. **Realtime session lifecycle** (decision above).
5. **Fix the stale comment and attribution** in `log_handlers.go:~494-505`, and add the sessions table doc
   with its generating test.

### Test strategy (the invariant)

- **One decision point:** grep/AST tests for (a) `X-Session-Id` read only in `adapter/session.go`, (b) the
  NUL-concatenation only in `state/key.go`, (c) the `"::"` pipeline format only in `Key.Child`.
- **Isolation properties** (extend `tenant_test.go`): tenant A and tenant B using the same id never share a
  session; two agents with the same id never share a session; an empty id never stores anything
  (`Store.Count()` unchanged after N anonymous requests).
- **Lifecycle:** after a run of each surface (chat with and without header, responses chain, pipeline,
  realtime connect/close, runner case) the store count returns to its pre-run value or the surface is
  explicitly marked "retained until TTL" in the generated table.

### Performance

Engine hot path. A comparable struct key can be passed without the three-string concatenation that
`scopedSessionKey` performs per request when a client sends a session id (`engine.go:330`); this should
remove an allocation on the benchmarked paths (`BenchmarkProcessRequest_*` set `SessionID`, so they exercise
this branch) once R9 lets the store accept `Key` directly. Until then the step-2 `String()` call keeps
allocation exactly as today. Guard: `benchguard` exact allocs/op; any *increase* fails CI.

### Risks and open questions

- The alias `SessionKey = state.Key` is a placement decision forced by the import rules (the brief's
  `engine.SessionKey` cannot be defined in `engine` and used in `state` without a cycle).
- Realtime lifecycle choice (above).

**Effort: S.**

---

## R9: Shared state seam

### Problem

Under more than one replica, a request's behaviour depends on which pod it reaches. The chart already says
so and refuses multi-replica rendering unless acknowledged (`deploy/helm/mockagents/values.yaml:4-10`,
`templates/deployment.yaml:13-21`). Inventory of process-local state (verified):

| State | Location | Shared today? | Failure under HA |
|---|---|---|---|
| Conversation sessions | `state.MemoryStore` (`cmd/mockagents/start.go:209`; interface `state/store.go:18`) | no | turn 2 on another pod restarts at turn 1; `turn_number` scenarios misfire |
| Interaction log | `storage.SQLiteStore` file (`start.go:237-238`); concrete type threaded through `server.Config.LogStore` (`server.go:77`), `LogHandlers.Store` (`log_handlers.go:25`), `LogWorker.store` (`log_worker.go:40`), `CostsHandlers.Store` (`costs_handler.go:15`) | no | each pod has its own log; `/api/v1/logs` and `/costs` show a random slice |
| Live log feed | in-process `LogBroadcaster` (`log_broadcaster.go:24`, `Publish` `:121`) | no | SSE clients see only their own pod's traffic |
| Audit log | `audit.NewSQLiteStore` (`start.go:290`); interface exists (`audit/store.go:39`) with one implementation | no | audit trail is split per pod |
| Auth cache | `tenancy/auth_cache.go:37`, invalidated locally; TTL 5 min (`start.go:374,382`) | no | a key revoked or rotated on pod A keeps authenticating on pod B for up to 5 minutes (inferred from the local `Invalidate`; verify with the two-server test below). This is a security item, not only consistency |
| Rate buckets, quota overrides | `quota.Enforcer` (`quota.go:1-9`: "process-local") | spend: yes via `SpendBackend` + 5 s cache; rate: no | effective rate = configured x replicas; override changes reach only the pod that received the `PUT` |
| Chaos counters (`FailFirst`) | `engine/chaos.go` maps keyed by `chaosKey` | no | "fail the first N" is per pod |
| Agent registry edits (write API, reload) | `engine.AgentRegistry` | no | create/replace/delete on one pod invisible to others |
| Responses, conversations, files, batches | adapter in-memory stores (R5 inventory) | no | `previous_response_id` or a file id works only on the pod that created it |
| MCP sessions, A2A tasks | `mcp/streamable.go`, `a2a/server.go` | no | separate CLI servers; same property |

`pgx/v5` is already a dependency (`go.mod:14`) and the tenancy store has a Postgres implementation selected
by `MOCKAGENTS_TENANCY_DSN` (`start.go:369`), so a Postgres backend adds no new dependency class. There is
no Redis or NATS client.

### Decision required before this is scheduled

**Is multi-replica a supported topology?** The mock's value is determinism, and several items above (chaos
counters, registry edits) are inherently single-writer. If the answer is "no", do only the *interface*
extractions in phase 0 (cheap, independently useful for testing) and keep the Helm guard. If "yes", phases
1-3 follow. This document recommends phase 0 now, phase 1 when a deployment needs it, and treats phases 2-3
as optional.

### Proposed design

Phases, each shippable on its own.

**Phase 0: interfaces without new backends.**

```go
// internal/storage: extract from *SQLiteStore (methods used by server: Log, Query, GetByID, DeleteAll,
// DeleteForTenant, Count, Ping, PruneToMaxRows, DatabaseSize, Close)
type Store interface { ... }          // SQLiteStore satisfies it unchanged
// server.Config.LogStore, LogHandlers.Store, LogWorker.store, CostsHandlers.Store become storage.Store.
```

```go
// internal/engine/state: sessions as an operation, not a shared pointer
type Store interface {
    // Apply runs fn with exclusive access to the session for key (created if absent) and persists the result.
    // Remote backends may invoke fn more than once (optimistic retry): fn must be a pure function of the session.
    Apply(ctx context.Context, key Key, agent string, fn func(*Session) error) error
    Delete(ctx context.Context, key Key) error
    Count(ctx context.Context) (int, error)
    Cleanup(ctx context.Context) error
}
```

The current interface hands out a shared `*Session` and documents "no separate save step" (`store.go:11-17`);
that contract cannot be satisfied by any store that is not in the same address space, which is the only reason
sessions cannot be externalised. `Session` already carries JSON tags (`state/session.go:20-33`), so
serialisation is not the blocker. `MemoryStore.Apply` takes the per-session lock exactly as `ApplyTurn` does
now; throwaway sessions (`engine.go:324`) never touch the store, which keeps SDK traffic without
`X-Session-Id` off any remote backend.

**Phase 1: shared logs, audit, auth invalidation, quota honesty.**
- `storage.PostgresStore` and `audit.PostgresStore` (conformance suites gated on `MOCKAGENTS_TEST_PG_DSN`, the
  same pattern as `tenancy/conformance_test.go:39`); selection by DSN env vars alongside
  `MOCKAGENTS_TENANCY_DSN`.
- **Live feed without new infrastructure:** replace broadcaster-only delivery with "broadcast locally, plus
  poll `id > last` every ~1 s for tenants with subscribers", using the existing `(tenant_id, id DESC)` index.
  `LISTEN/NOTIFY` is the later optimisation.
- **Auth-cache invalidation across replicas:** a monotonically increasing `auth_epoch` row bumped by every key
  mutation and checked by each replica at most once per second (cache flush when it changes). Bounds the
  revocation lag to ~1 s instead of 5 minutes.
- **Quota:** keep local token buckets; add an explicit `MOCKAGENTS_REPLICAS` divisor (rate per pod =
  configured / N) and document that overrides apply per pod until a shared override table exists. The R2
  `Meter` is the seam where a shared limiter would later plug in.

**Phase 2: sessions.** `state.SQLStore` (Postgres): one row per key `(key, version, doc jsonb, expires_at)`;
`Apply` is read, run `fn`, `UPDATE ... WHERE version = $v`, retry up to three times on conflict. Requires
auditing `engine.go:333-480`, the `ApplyTurn` callback, for side effects that must not repeat (metrics
increments, chaos counters). Alternative with zero code: **sticky routing** on `X-Session-Id` / API key at the
load balancer, which is what most mock deployments want and should be the documented default.

**Phase 3 (optional).** Provider stores (responses, conversations, files, batches) behind `bounded`-backed
interfaces with SQL backends, and a shared agent source. Not recommended unless asked for.

After phases 0-2 the chart guard can name exactly which classes remain per pod (registry edits, chaos
counters, provider stores) instead of a blanket refusal.

### Migration plan

0a. Extract `storage.Store` (pure type change; guard: `server/*_test.go`, `storage/*_test.go`).
0b. R8 step 2 first (so `state.Key` exists), then change `state.Store` to `Apply` and port `engine.go:323-340`;
    `MemoryStore` keeps a `Get/GetOrCreate` pair as concrete methods for the runner and tests. Guard:
    `session_concurrency_test.go`, `state/store_limits_test.go`, engine benchmarks (exact allocs/op; the
    `fn` closure must not add one, the likely cost being a captured-variable closure that escapes through the
    interface call, so verify before merging).
1a. `audit.PostgresStore` + conformance. 1b. `storage.PostgresStore` + conformance + polling feed.
1c. `auth_epoch`. 1d. `MOCKAGENTS_REPLICAS` divisor and docs.
2.  `state.SQLStore`, `ApplyTurn` purity audit, two-replica integration test, chart guard text update.

### Test strategy (the invariants)

- **Backend conformance:** one suite per interface (`state.Store`, `storage.Store`, `audit.Store`) run
  against every implementation; Postgres cases skip without `MOCKAGENTS_TEST_PG_DSN`, as tenancy does.
  For SQLite, two handles on one file emulate two replicas.
- **Two-server test:** two `server.New` instances over the same backends: write on A, read on B for logs,
  audit, spend (existing `SpendBackend`), sessions (turn 1 on A, turn 2 on B yields `turn_number == 2`), and
  **key revoke on A is rejected on B within the documented bound**.
- **Honesty test:** a table listing each per-pod class from the inventory, asserted against the chart's guard
  message so the documentation cannot overstate or understate.

### Performance

Memory backends are the default and unchanged on the hot path. Remote session I/O occurs only for requests
that carry an explicit session id. Guard: engine benchmarks (exact allocs/op), `BenchmarkSessionStore_*` (P0),
plus a Postgres-backed `Apply` benchmark that is informational only.

### Risks and open questions

- `ApplyTurn`'s callback holds the session lock across scenario matching, template rendering and tool
  processing; under optimistic retry that work repeats. If it is not pure (metrics, `uuid`/random templates
  producing different output on retry are acceptable, counters are not), phase 2 needs a per-key advisory
  lock instead of CAS.
- Polling feed adds steady DB load proportional to subscribed tenants; cap by interval and by
  subscriber count.
- The `auth_epoch` check adds one DB read per second per replica; acceptable, measurable.

**Effort: L** overall; phase 0 is S-M and worthwhile alone.

---

## Ordering and recommended sequence

### Dependency graph

```
 P0  ---> gates R1, R3, R4, R5, R8, R9 (their code is outside the committed baseline)

 R6, R7        independent of everything; schedule for capacity

 R1 ---> R2 ---> R9
 R5 ---------->  R9
 R8 ---> R3 ---> R4
 R8 ---------->  R9

 soft: R2 ~~> R3   R3 collapses the six QuotaError render sites that R2 step 3d adds
 soft: R1 ~~> R3   dispatch/provider declare policy per route
 soft: R2 ~~> R9   the Meter is where a shared limiter would plug in
```

Hard edges: R1 -> R2 (the deny-all invariant needs `Billable`), R8(step 2) -> R9 (state seam takes
`Key`), R3 -> R4 (Bedrock/Ollama emitters need native stream paths), R2 + R5 + R8 -> R9.

### Recommended sequence

| Wave | Work | Why here |
|---|---|---|
| 0 | P0; R7 steps 1-3; R6 steps 1-2; R8 steps 1-3 | cheap, independent, and they de-risk later waves (baseline, one-place invariants, a conformance suite) |
| 1 | R1 | the policy table is the declaration R2's invariants and R3's providers hang from; highest-consequence change, so do it early and alone |
| 2 | R2 (3a pipelines, 3b realtime, 3c batches, 3d direct) | closes the security side doors (S-02..S-04) with the smallest remaining blast radius; R5 may run in parallel by a different owner |
| 3 | R3, then R4 | the largest mechanical refactor, done after metering and session extraction have stopped moving under it |
| 4 | R5 hot-path consumers (session store), R6 step 3-5, R7 steps 4-6, R9 phase 0 -> 1 -> (2) | R9 only after R2, R5, R8; stop after phase 0 if HA is not required |

### Decisions needed from maintainers

1. Should provider routes that store tenant data remain `Anonymous` in multi-tenant mode (R1)?
2. Charge location and the Anthropic thinking/cache accounting gap (R2).
3. Default byte budgets for stored files and batches (R5).
4. Malformed-frame shape for NDJSON and eventstream (R4).
5. Realtime session lifecycle: delete on close, or throwaway plus `PriorTurns` (R8).
6. Is multi-replica a supported topology (R9)? Everything after phase 0 hangs on this.

### Biggest risks across the set

1. **R1:** a mistake opens a control-plane route. Deny-by-default zero policy, parity tests, and I2/I3 matrices.
2. **R2:** deliberate behaviour changes (streaming now charged, engine endpoint now limited, invalid requests
   no longer consume tokens) and moving a database write near the response path.
3. **R3:** six handlers refactored at once; goldens recorded first make fidelity changes visible.
4. **R5:** a naive LRU reintroduces the contention audit M-20 removed from the session store.
5. **R9:** scope. The inventory is long and part of it (chaos counters, registry edits) is inherently
   single-writer.

---

## Appendix A: claims that are inferred, not proven

- R2.5: truncated captured bodies yielding zero usage (reading `pricing.ExtractUsage` and `maxCaptureBodyBytes`;
  not executed). Step 0 of R2 turns it into a test.
- R3: that the four re-derived streaming-config lookups agree with the engine today for all inputs.
- R5: tenant-reachable growth of the engine's regex/template caches depends on whether agent counts per
  tenant are bounded, which was not checked.
- R8: the realtime engine-session leak follows from `DeleteSession` having a single caller (`runner`); a
  runtime check of `Store.Count()` after a socket closes would confirm it.
- R9: cross-replica auth-cache staleness follows from `Invalidate` being a method on a per-process object;
  the two-server test in R9 proves or refutes it.
- Performance statements marked "expected" (alloc removal in R8, near-zero dispatch cost in R3) are
  hypotheses to be checked against `benchguard`, not measurements.
