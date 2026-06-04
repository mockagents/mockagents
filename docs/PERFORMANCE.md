# MockAgents — Performance Engineering Handoff

**Author:** performance review, 2026-06-03
**Audience:** developers optimizing the request hot path, the auth path, and the
storage/logging layers.
**Companion docs:** `docs/benchmarks/README.md` (how to measure + the published
baseline), `CLAUDE.md` (architecture + the hot-path perf envelope rule).

> This is a *forward-looking* optimization guide. The cheap, zero-risk
> micro-optimizations already landed (see **§5 — Already optimized**). What
> remains is a prioritized backlog of the work that's *left*, each item grounded
> in a specific `file:line` with a concrete change and the regression test to
> add. Treat the **§5** list as "do not redo."

---

## 1. TL;DR — where to spend the next perf budget

The engine hot path is already fast (static request ~500 ns/op, 9 allocs;
registry lookup ~14 ns/0 allocs). The profile shows **GC scan dominates (~54 %
of CPU in the tight bench loop)**, driven by ~9–23 small heap allocations per
request — so on the request path, **allocations/op is the lever, not raw CPU**.
The biggest wins are *per-request work that shouldn't run at all*:

| # | Opportunity | Why it matters | Effort |
|---|-------------|----------------|--------|
| ~~**PERF-01**~~ ✅ | Registry does an **O(n) scan on every model request** (`GetByModelForTenant`), bypassing its own index — *done 2026-06-03 (0 allocs, flat at N=1000)* | On *every* `/v1/chat/completions` & `/v1/messages` | M |
| ~~**PERF-02**~~ ✅ | OTel HTTP middleware **allocates per request even when tracing is disabled** (the default) — *done 2026-06-03* | On *every* HTTP request | S |
| ~~**PERF-03**~~ ✅ | Auth `Resolve` writes `last_used` (an **fsync, serialized behind one connection**) on every cache miss — *done 2026-06-03* | Every first/re-warm auth | S |
| ~~**PERF-04**~~ ✅ | Response JSON encoding is **not pooled** (the request *decode* side is) — *done 2026-06-03 (modest: ~28% faster encode, allocs unchanged)* | Every response | M |

**All four P1 items are done (2026-06-03)** — PERF-01/02/03 are clear wins,
PERF-04 a modest one. Everything below P1 is incremental alloc-shaving on an
already-healthy path — measure before investing.

---

## 2. The performance model

### Critical paths (by request frequency × cost)

1. **LLM request** (`POST /v1/chat/completions`, `/v1/messages`) — the hot path.
   Stages, in order, with current cost:
   - adapter decode-in (`adapter/decode.go` — **pooled buffer**, good) →
   - convert-in (`openai.go:206` / `anthropic.go:165`) →
   - **agent resolve** (`engine.go:257` → `agent_registry.go` — see PERF-01) →
   - session get/create (`engine.go:296`, `state/store.go`) →
   - `ApplyTurn` under the per-session lock: scenario match → response generate →
     tool processing (`state/session.go:64`) →
   - translate-out + `writeJSON` (`openai.go:314` — **not pooled**, see PERF-04).
   - Baselines (`docs/benchmarks/latest.md`): static ~500 ns/9 allocs, template
     ~1300 ns/22, tool-calls ~1815 ns/23, regex-match ~794 ns/14.

2. **Authenticated control-plane request** (multi-tenant mode) — the auth path.
   `tenancy.AuthMiddleware` → `store.Resolve`: cache hit = sub-µs (skips bcrypt);
   **cache miss = prefix SELECT + bcrypt (~50–80 ms) + a `last_used` write**, all
   serialized behind `MaxOpenConns=1`. The auth cache (`auth_cache.go`) is the
   relief valve; keeping it warm is the whole game (see PERF-03 and §4).

3. **Interaction logging** (async, off the request path) — `LogWorker` (4
   workers, 1024 queue, drop-on-full) + `LogBroadcaster` SSE fan-out. Already
   bounded; `Submit` is non-blocking. Good.

### The dominant cost: GC pressure, not CPU

Per `docs/benchmarks/latest.md` profile notes, the engine isn't CPU-bound on
logic — it's bound on `runtime.scanObject`/`gcDrain` because the bench drives
allocation fast. **Every alloc you remove from the request path is a direct GC
win.** This is why the backlog below is mostly "stop allocating X per request,"
not "make X faster."

---

## 3. Optimization backlog (prioritized, grounded)

Each row: `ID · impact · effort · site · evidence → change`. Add the noted
regression guard with every fix (and follow the **neuter-verify discipline** —
break the fix, confirm the new bench/test regresses, restore).

### P1 — High impact (per-request waste on the hot path)

| ID | Eff | Site | Evidence → Change |
|----|-----|------|-------------------|
| **PERF-01** ✅ | M | `engine/agent_registry.go` `GetByModelForTenant` | Adapters send a *model*, not an agent name, so this runs on every LLM request and does a full `for _, def := range r.agents` RLock scan — the O(1) `byModel` index is never reached from the hot path. **Caveat (verified):** you can't just reuse `byModel`; this method has tenant-visibility (owner vs global) **and** a deterministic lexicographic tie-break (F-AR-002). **Change:** add a `byModelTenant` index — `map[modelKey] → {ownerSorted, globalSorted}` (or a small sorted slice per model) maintained in `Register`/`Remove`, so resolve is a map lookup + visibility pick, preserving the tie-break. **Guard:** a new `BenchmarkResolveByModel_ManyAgents` (N=1000 agents) must stay flat as N grows; keep the existing determinism tests. **✅ DONE 2026-06-03:** added `byModelTenant map[model]map[owner]*agent` holding the lexicographically-smallest agent per (model, owner); the lookup is two map reads (`owners[tenantID] ?? owners[""]`), exactly preserving the visibility + tie-break. Maintained by rebuilding only the affected model bucket on Register/Remove (rare). Measured: **0 allocs, flat 29 ns/op at N=1000** (was an O(n) scan). Guards `BenchmarkGetByModelForTenant_ManyAgents`, `…_NoAllocs`, `…_IndexMaintained` (Remove/model-change; neuter-verified) + the existing determinism tests. |
| **PERF-02** ✅ | S | `observability/tracing.go:114-128`, wired at `server/server.go` (`observability.HTTPMiddleware(handler)`) | `HTTPMiddleware` allocates a `statusRecorder`, builds 2 `attribute.String` + a variadic slice for `StartSpan`, copies the request (`r.WithContext`), and calls `SetAttributes` — **on every request, even under the default NoOp tracer**. The engine path already gates this via `observability.IsEnabled()` (`engine.go:82`); the HTTP wrapper doesn't. **Change:** in `server.New`, only add the wrapper when `observability.IsEnabled()` (or early-return inside it). Removes one ResponseWriter wrapper + ~3 allocs + a context copy per request when tracing is off (the default). **Guard:** an HTTP-level bench asserting 0 tracing allocs when disabled. **✅ DONE 2026-06-03:** `HTTPMiddleware` returns `next` unwrapped when `tracingEnabled` is false — and since `NewTracerProvider` is never wired in the binary, that's *every* request today. Guard `TestHTTPMiddleware_SkippedWhenDisabled` (identity check, neuter-verified); the span tests now flip the flag via `newTestTracerProvider`. |
| **PERF-03** ✅ | S | `tenancy/store.go` `Resolve` (`last_used` UPDATE) + DSN at `store.go:126` | On every authenticated **cache miss** that resolves, `Resolve` issues `UPDATE api_keys SET last_used = ?` — a separate write, and the tenancy DSN omits `synchronous(normal)` so it's a **full fsync**, serialized behind `MaxOpenConns=1`. **Change (two parts):** (a) coarsen the write — skip it if `last_used` is within the cache TTL (extend the existing on-hit suppression to misses, or batch via a background flusher); (b) add `_pragma=synchronous(normal)` to the tenancy DSN to match the log/audit stores (API-key metadata isn't a FULL-durability system of record). **Guard:** a test asserting a second auth within the window issues no `last_used` write. **✅ DONE 2026-06-03:** (a) `Resolve` now reads `last_used` in the existing prefix SELECT and bumps it only when stale by ≥ `lastUsedResolution` (1 min) via `shouldBumpLastUsed` — no new state/locks, and it also covers the cache-disabled mode (every request a miss) and eviction/invalidation bursts; (b) added `_pragma=synchronous(normal)` to the tenancy DSN (verified it reads back NORMAL). Guards `TestShouldBumpLastUsed` (table) + `TestResolve_CoarsensLastUsedWrite` (integration, cache-disabled; neuter-verified). |
| **PERF-04** ✅ | M | `server/handlers.go` `writeJSON`, `adapter/openai.go:314` / `anthropic.go:160` | Both `writeJSON`s do `json.NewEncoder(w).Encode(v)` — a fresh encoder + scratch per response, reflect-encoding interleaved with socket writes, **no pooling** — while the inbound path *is* pooled (`adapter/decode.go`, cited at −39 % B/op in CLAUDE.md). **Change:** marshal into a pooled `*bytes.Buffer` (optionally a pooled `*json.Encoder`), set `Content-Length`, single `w.Write`. Mirror `decodeBufPool` exactly, including its oversize-buffer guard. **Guard:** `BenchmarkWriteJSON` shows reduced B/op; the existing conformance tests cover correctness. **✅ DONE 2026-06-03 — smaller win than predicted (honest result):** pooled a `respEncoder` (buffer + bound `json.Encoder`) in both packages; byte-for-byte identical output (newline preserved; conformance + `TestWriteJSON_MatchesEncoderOutput` green). **Measured ~28 % faster encode (234→169 ns/op) but UNCHANGED B/op (64) / allocs (2)** — the predicted alloc win doesn't materialize because `encoding/json` already pools its internal encode scratch and the per-call encoder is stack-allocated by escape analysis. Kept because it's a real, zero-risk CPU win; the "−39 % B/op" framing applied to the *decode* side, not this one. |

### P2 — Medium (hot-path allocations & contention)

| ID | Eff | Site | Evidence → Change |
|----|-----|------|-------------------|
| ~~**PERF-05**~~ ✅ | M | `server/middleware.go` `statusWriter`, `log_handlers.go` `captureWriter` | Three ResponseWriter wrappers per LLM request; `statusWriter` is heap-allocated fresh every request (`&statusWriter{...}`) while `captureWriter` is pooled. **Done 2026-06-03:** pooled `statusWriter` via `statusWriterPool` + `acquire/releaseStatusWriter` (Option A). Collapsing into `captureWriter` was rejected — it's mounted only for the 3 loggable LLM paths, but `StructuredLogger` wraps every request. 0 allocs/op acquire/release (`BenchmarkStatusWriterAcquireRelease`); reset neuter-verified (`TestStatusWriter_PoolReset`). |
| ~~**PERF-06**~~ ✅ | M | `server/middleware.go` (`RequestID`, `ExtractAPIKey`, `WithPrincipalTenantScope`) + `log_handlers.go` (`WithRequestMeta`) | Up to **4 `context.WithValue`/`r.WithContext`** per request, each a context node + shallow request copy. **Done 2026-06-03:** merged `RequestID` + `ExtractAPIKey` into one `RequestContext` middleware storing a single `*requestScope{requestID, apiKey}` under one context entry — one node + one Request copy instead of two of each, and a pointer-in-interface avoids the string-boxing allocs. `BenchmarkRequestContext`: **10→7 allocs/op, 841→472 B/op, −25 % ns/op** on an auth'd request; neuter-verified (`TestExtractAPIKey_BearerToken` fails when the scope isn't stored). Tenant-scope was **not** folded in: it must run after `tenancy.AuthMiddleware` (reads the principal auth sets), which sits below `StructuredLogger`. Bonus: the old `ExtractAPIKey` stored an `APIKeyKey` value **no production code read** (the tenancy auth path parses its own bearer). |
| ~~**PERF-07**~~ ✅ | S | `server/middleware.go` `generateRequestID`, `adapter/openai.go` `generateID`/`extractSessionID` | Cosmetic correlation/response IDs use `crypto/rand` (a syscall-backed CSPRNG) + `fmt.Sprintf` — 2–3 rand draws + Sprintf allocs/request for IDs that need *uniqueness, not unpredictability* (same rationale as the existing `fallbackToolCallID`). **Done 2026-06-03:** `math/rand/v2.Uint64()` (auto-seeded, lock-free, goroutine-safe) + `strconv.FormatUint(_, 16)`; callers concatenate the prefix instead of `fmt.Sprintf` (`chatcmpl-`/`sess-`/`toolu_`/`msg_`/`req-`). `generateRequestID` **173→54.7 ns/op, 3→2 allocs**; `generateID` **166.7→29.7 ns/op, 3→1 alloc** (fires per response + message + tool call). Uniqueness guarded by `TestGenerateID_UniqueAndHex` (neuter-verified). None of these are security tokens — API keys remain bcrypt in `tenancy`. |
| **PERF-08** ✅ | S | `engine/scenario_matcher.go:106` | `strings.Contains(lowerMessage, strings.ToLower(rule.ContentContains))` re-lowercases the **static** scenario literal on every request, every scenario. **Change:** pre-lower `ContentContains` once at config load (store a lowered copy on the rule). The per-request match becomes a pure `strings.Contains`, zero allocs. **✅ DONE 2026-06-03:** memoized via a `lowerCache sync.Map` on the matcher (`lowerContains`), matching the existing `regexCache` pattern — no type/load-path change. The lowered literal is computed once; repeat matches are an allocation-free map read (eliminates the per-request alloc for mixed-case literals like `"Hello"`/`"Error"`). Guard `TestScenarioMatcher_LowerContainsMemoized` (`AllocsPerRun == 0`, neuter-verified). |
| **PERF-09** ✅ | S | `server/log_handlers.go` `InteractionCapture` | ~~Buffers up to 1 MiB of the response body … only so `pricing.ExtractUsage` can re-parse the usage block.~~ **⚠️ The survey premise was WRONG (verified):** the response body is **persisted and returned by the log API** (`storage/models.go` `response_body`) and **displayed in the GUI log-detail view** (`gui/app/logs/[id]/page.tsx:98`). It is a real feature, not cost-extraction scratch — dropping the buffering would empty the log-detail panel. **The buffering itself is already pooled** (`captureWriter` `sync.Pool` reuses the backing array), so it isn't even a hot-path alloc. **✅ DONE 2026-06-03 — implemented the *safe* adjacent win:** the body was copied **twice** per request — `snapshot()` (a `[]byte` copy) then `string(snapshot)` — when only one independent copy is needed. Replaced with `bodyString()` = a single `string(cw.body)`, and the agent-name probe now reads the still-live `cw.body` directly. Measured on a ~1.5 KB response: **3072 B/2 allocs → 1536 B/1 alloc (~53 % faster on the copy)**, scaling with response size, on every loggable request. Guard `TestCaptureWriter_BodyStringIndependent` (independence across pool reuse) + existing capture/pool tests. |
| ~~**PERF-10**~~ ✅ | M | `tenancy/store.go` `BulkRotateTenantKeys` | `bcrypt.GenerateFromPassword` runs **inside the open transaction**, once per key — a 20-key tenant holds the write tx (and the single tenancy connection) locked for >1 s of bcrypt, blocking all auth. **Done 2026-06-03:** the `SELECT` + all `generateAPIKey`/`bcrypt` now run *before* `BeginTx`; the tx holds the connection only for the N cheap `UPDATE`s. A concurrent-delete race (read is now outside the tx) is handled by skipping any `UPDATE` that affects 0 rows. Guarded by `TestBulkRotate_AuthNotBlockedDuringHashing` (a relative-timing concurrency test, neuter-verified: with bcrypt back inside the tx a concurrent read blocked 470 ms of a 493 ms rotation). No alloc benchmark — this is a lock-hold-time fix, invisible to a single-threaded bench. |
| ~~**PERF-11**~~ ✅ | S | `server/log_handlers.go` `StreamLogs` | Per delivered row: `json.Marshal` (alloc) + `fmt.Fprintf("event: log\ndata: %s\n\n", ...)` (reflect + alloc), and `SetWriteDeadline` (a syscall) **every loop iteration** including pure heartbeats. **Done 2026-06-03:** per-connection reused `bytes.Buffer` + `json.Encoder` (`appendLogFrame` encodes straight into the buffer), heartbeat emitted from a package-level byte slice, dropped-frame built with `strconv` not `fmt`. `BenchmarkLogFrame_*`: **3→1 allocs/op, 1179→256 B/op, −43 % ns/op** per row; zero-alloc heartbeats. Wire bytes are byte-for-byte identical (guarded by `TestAppendLogFrame_WireFormat`, neuter-verified, plus the existing SSE e2e tests). **Correction to the survey:** the deadline bump was relocated to immediately-before-each-write (tidier; drops the one wasted bump on the ctx-cancel teardown) but is **not** a per-row syscall win — heartbeats still bump before writing, so the bump count is essentially unchanged. The real win is the allocation drop. |

### P3 — Low / opportunistic (alloc-shaving + robustness)

| ID | Eff | Site | Change |
|----|-----|------|--------|
| PERF-12 | S | `tenancy/store.go`, `audit/store.go` | Prepare the hot `Resolve` prefix-SELECT + `last_used` UPDATE and the audit `Append` INSERT once (`db.Prepare`) instead of re-parsing every call — matters more on the serialized tenancy connection and under `auth.denied` floods. |
| PERF-13 | S | `storage/sqlite.go` | Add a composite `(tenant_id, id DESC)` index for the tenant-scoped log dashboard, and prefer keyset pagination (`WHERE id < ?`) over `OFFSET` for deep pages (single-column indexes can serve the WHERE *or* the `id DESC` sort, not both). |
| PERF-14 | S | `engine/tool_processor.go` | Fast path: run `processOne` inline when `len(toolCalls) <= 2` (skip the goroutine + WaitGroup + `errs` slice); cache `indexTools` per agent at registration instead of rebuilding the map per request. |
| ~~PERF-15~~ ✅ | S | `adapter/anthropic.go:166` | Pre-size `convertAnthropicMessages` result (`make(..., 0, len(msgs)+1)`); the OpenAI twin already does. **Done 2026-06-03.** |
| PERF-16 | S | `server/handlers.go` error responses | Encode a fixed `type apiError struct{ Error string }` instead of `map[string]string{"error": …}` literals (matters under chaos 4xx/5xx storms). |
| ~~PERF-17~~ ✅ | S | `engine/response_generator.go` `generateUUID`/fakers | Hand-roll hex into a fixed `[36]byte` instead of `fmt.Sprintf` (the hottest template builtin's reflect cost). **Done 2026-06-03:** `generateUUID` now `hex.Encode`s into a `[36]byte` (kept crypto/rand — it's a real-looking v4 UUID, not a cosmetic id). `BenchmarkGenerateUUID`: **7→1 allocs/op, 184→48 B/op, −54 % ns/op** (and New still includes the rand draw the Old bench isolates out). Format guarded by `TestGenerateUUID_V4Format`, neuter-verified. The faker `Sprintf`s were left — they're rare and not on the per-token hot path. |
| PERF-18 | M | `server/log_broadcaster.go` `Publish` | Copy-on-write `atomic.Pointer[[]*LogSubscription]` snapshot so `Publish` ranges a lock-free slice (today it holds one `Mutex` across the whole subscriber iteration on the async write loop). Low impact at the single-process target. |
| ~~PERF-19~~ ✅ | S | `adapter` token callers | `estimatePromptTokens` re-extracts/`strings.Join`s message content that `convertOpenAIMessages` already flattened into `inbound.Messages`; count off the flattened strings. **Done 2026-06-03:** both handlers now call a shared `sumMessageTokens(inbound.Messages)`; the per-message `extractStringContent`/`extractAnthropicContent` re-extraction (incl. a `strings.Join`+slice alloc per multi-part message) is gone. Totals are identical (the Anthropic system message is already prepended into the flattened slice) — verified by the existing adapter usage-token tests. (The site is in `adapter/`, not `engine/token.go` as the survey said.) |
| ~~PERF-20~~ ✅ | S | `storage/sqlite.go`, `tenancy/store.go` scan loops | Pre-size result slices to the known `limit` (`make([]T, 0, limit)`) to avoid `growslice` churn on large pages. **Done 2026-06-03** for the log `Query` (`make([]InteractionLog, 0, limit)`, limit clamped to `[1, MaxLimit=1000]`). The tenancy `ListTenants`/`ListAPIKeys` loops have **no LIMIT** (they return all rows), so there is no known cap to pre-size to — left as-is rather than guess. |
| ~~PERF-21~~ ✅ | S | `server/server.go` http.Server | Add `ReadHeaderTimeout` (e.g. 10 s) — robustness, slow-loris hardening, not an alloc fix. **Done 2026-06-03:** `DefaultReadHeaderTimeout = 10s` + Config field + a fallback in `New` so a hand-built `Config{}` still gets the protection. |

---

## 4. Scaling beyond micro-optimization

The items above shave the *single-process* hot path. The structural ceilings:

- **Tenancy `MaxOpenConns=1` is a deliberate correctness ceiling**, not an
  oversight (`tenancy/store.go`): it guarantees `foreign_keys(on)` on the one
  connection that runs `ON DELETE CASCADE`, and serializes SELECT-then-UPDATE
  pairs. **Do not raise it to "fix" auth throughput** — that would silently drop
  FK enforcement under modernc/sqlite. The correct relief is the **auth cache**
  (keep it warm; PERF-03 stops it self-inflicting writes). If tenancy write
  volume ever genuinely saturates one connection, the fix is a connector-level
  pragma + a higher pool, or the Postgres swap below.
- **SQLite is the storage ceiling.** The `Store` interface is explicitly shaped
  for a mechanical Postgres swap; interaction-log volume (the highest-write
  table) is the first thing that will demand it at real SaaS scale. WAL + 8-conn
  pool + `synchronous=NORMAL` already buys a lot of headroom for the log/audit
  stores.
- **Horizontal scaling is gated by the in-memory session store.**
  `state.MemoryStore` is process-local, so multiple instances need sticky
  session routing or an external/shared session store. The engine is otherwise
  stateless. Document this before anyone puts N replicas behind a round-robin LB.
- **bcrypt cost (10) is correct** and *not* the steady-state bottleneck because
  of the cache; don't lower it. The cache TTL is the worst-case stale-auth bound
  — a perf/security tradeoff, already documented in `auth_cache.go`.

---

## 5. Already optimized — **do not redo**

These are deliberate, measured wins. Re-doing or "simplifying" them is a
regression:

- **Engine hot path (v0.2 perf workstream, `docs/benchmarks/`):** session slice
  pre-sizing (`cap=16`), tracer NoOp bypass at the engine layer
  (`IsEnabled()`), lazy captures map in the matcher (shared `matchedSentinel`
  for content_contains → ~29 ns/1 alloc), `bytes.Buffer` `sync.Pool` in the
  response generator (with a static-content fast path that skips it), and the
  O(1) `byModel` index (correct; just bypassed on the *tenant* path — PERF-01).
- **Regex compile cache** (`scenario_matcher.go`): `*regexp.Regexp` cached in a
  `sync.Map`, typed-nil for known-bad patterns. Compiled exactly once.
- **Single `ToLower` of the user message** per scenario sweep (not per scenario).
- **Adapter request-decode buffer pool** (`adapter/decode.go`, −39 % B/op) — the
  template the *response* path should copy (PERF-04).
- **`captureWriter` pool** (`log_handlers.go`): struct + body backing array
  reused via `body[:0]`, with an oversize-array guard. Streaming responses are
  sniffed once and skip buffering.
- **Bounded async `LogWorker`** replaced goroutine-per-request fan-out (was ~54 %
  cum GC); non-blocking `Submit`, atomic counters, clean guaranteed-stop
  shutdown. **Broadcaster** drop-don't-block per-subscriber channels.
- **Storage/log pool** `MaxOpenConns=8` + WAL + `synchronous=NORMAL`; pagination
  bounded everywhere (`MaxLimit=1000`). **Audit** deliberately avoids a Go-level
  mutex (relies on WAL write serialization).
- **`Resolve` drains + closes the Rows iterator before the `last_used` UPDATE** —
  required for the single-connection store; do not turn it into a streaming scan.
- **Auth cache** is `RWMutex` (shared-lock hits), evicts expired-first, never
  evicts on overwrite, keyed on the full SHA-256 digest.
- **SSE write-deadline plumbing** (`ResponseController` + `Unwrap`/`Flush` on the
  wrappers) so long-lived streams survive the global `WriteTimeout`.

---

## 6. Measurement & anti-regression

**Always measure first.** The methodology, the published baseline, and the
pprof workflow live in `docs/benchmarks/README.md`. Key rules:

- **`make bench-report` rewrites the committed baseline** — only run it on
  non-throttled hardware. This dev machine's power governor throttles sustained
  bench runs ~2.5×, corrupting `ns/op`. For a quick check anywhere, `make bench`
  *prints without writing*; **`allocs/op` and `B/op` are machine-independent** and
  are the signal that matters most here (GC-bound path).
- **The baseline is currently stale** (predates the engine-review slice — see the
  ⚠️ note in `docs/benchmarks/README.md`). Refresh it off-governor before using
  `ns/op` deltas for a release gate.
- **Hot-path envelope (from CLAUDE.md):** registry lookup < 100 ns, scenario
  matcher (content) < 1,000 ns, `ProcessRequest` static < 2,000 ns, with tools
  < 5,000 ns. Anything outside → profile before tagging a release.
- **Per-fix discipline:** for each optimization, add a benchmark (or assert
  allocs/op via `testing.AllocsPerRun`), and *neuter-verify* it — revert the fix,
  confirm the bench regresses, restore. This mirrors the security-fix discipline
  used throughout the codebase and keeps the wins from silently rotting.
- **CI gate (recommended, not yet wired):** track `allocs/op` for the
  `BenchmarkProcessRequest_*` family and fail the build on a regression beyond a
  small threshold. `latest.json` (schema v1) already exists for exactly this
  trend-tracking; nothing consumes it yet.

---

## 7. Suggested execution order

1. ~~**PERF-02** (S, drop the tracing wrapper when disabled) and **PERF-08** (S,
   pre-lower scenario literals)~~ — **✅ done 2026-06-03** (smallest, pure wins,
   no behavior change; both neuter-verified).
2. ~~**PERF-03** (S, stop the auth path self-inflicting `last_used` fsyncs)~~ —
   **✅ done 2026-06-03** (coarsened bump + `synchronous(normal)`; neuter-verified).
3. ~~**PERF-01** (M, model-keyed tenant index)~~ — **✅ done 2026-06-03**
   (byModelTenant index; 0 allocs, flat at N=1000; neuter-verified).
4. ~~**PERF-04**~~ — **✅ done** (pooled response encoder; modest ~28% encode
   speedup, allocs unchanged). **PERF-09** (drop capture body buffering) is
   still open and pairs with it to make the *logged* response path cheaper.
5. ~~**PERF-09**~~ **✅ done** (the survey's "drop the body" premise was wrong —
   the body is displayed in the GUI log detail — but the safe adjacent win,
   eliminating the redundant double-copy, halved the per-request body alloc).
6. ~~**PERF-05**~~ **✅ done 2026-06-03** (pooled `statusWriter` — Option A. The
   alternative of collapsing status capture into the already-pooled
   `captureWriter` was rejected: `captureWriter` is mounted only for the 3
   loggable LLM paths while `StructuredLogger` wraps *every* request, so merging
   would drop status logging on all other routes. `acquire/release` mirror the
   `captureWriter` pool — release nils the embedded `ResponseWriter` so a pooled
   entry can't pin a finished request. Eliminates one `&statusWriter{}` heap
   alloc/request; `BenchmarkStatusWriterAcquireRelease` reports 0 allocs/op;
   reset is neuter-verified by `TestStatusWriter_PoolReset`.)
7. ~~**PERF-06**~~ **✅ done 2026-06-03** (merged `RequestID` + `ExtractAPIKey`
   into one `RequestContext` middleware backed by a single `*requestScope`
   context entry: 10→7 allocs/op, 841→472 B/op, −25 % ns/op on an auth'd
   request. Tenant-scope stayed separate — it has a hard ordering dependency on
   `tenancy.AuthMiddleware`. Found en route: the old `ExtractAPIKey` stored a
   context value no production code consumed.)
8. ~~**PERF-07**~~ **✅ done 2026-06-03** (swapped the cosmetic id generators
   from `crypto/rand`+`fmt.Sprintf` to `math/rand/v2.Uint64()`+`strconv`:
   `generateRequestID` 173→54.7 ns (3→2 allocs), `generateID` 166.7→29.7 ns
   (3→1 alloc). Uniqueness-not-unpredictability is the documented contract;
   no security tokens touched.)
9. ~~**PERF-11**~~ **✅ done 2026-06-03** (SSE log streaming: reuse a
   per-connection `bytes.Buffer`+`json.Encoder` and literal framing instead of
   `json.Marshal`+`fmt.Fprintf` per row — 3→1 allocs/op, 1179→256 B/op, −43 %
   ns/op; zero-alloc heartbeats. Wire format byte-identical, guarded + neuter-
   verified. The doc's "deadline syscall per iteration" premise was optimistic:
   the bump moved to right-before-each-write for clarity, not a syscall win.)
10. ~~**PERF-10**~~ **✅ done 2026-06-03** (moved the SELECT + per-key bcrypt out
    of the write transaction in `BulkRotateTenantKeys`, so a bulk rotation no
    longer pins the single tenancy connection for >1 s of hashing and stalls
    auth. Concurrent-delete race handled via a 0-rows-affected skip. Guarded by
    a relative-timing concurrency test, neuter-verified.)
    That closes the entire prioritized backlog (PERF-01..11). Remaining: **P3**
    as opportunistic alloc-shaving + robustness nits, each behind a bench.

Re-baseline (`make bench-report` off-governor) after each P1 item so the next
diff is honest.
