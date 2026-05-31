# Action Plan — internal/engine package audit

_Execute top-down. Each task maps to a finding ID (detail in `01-PER-FILE.md` / `02-INTEGRATION.md`). Check off when "Done when" holds. Self-contained — you don't need the other reports to act._

**This is an audit of already-merged code — nothing here blocks an existing build.** "Gate" below means "decide before shipping the next multi-tenant release," not "before merge."

## P0 — Blockers

_None. (Two S0 candidates were refuted: F-PL-001/002 nil-deref is unreachable; the validator no-op worry was disproven.)_

## P1 — High (do this cycle)

- [x] **Tenant/owner-scope the session store** — `X-02` / `F-ST-001` · effort:M · owner:claude · **DONE 2026-05-30**
  - **Where:** `engine.go` (`scopedSessionKey` helper + namespaced `GetOrCreate`; `TemplateContext.SessionID` now uses the logical client id).
  - **Fix shipped:** chose the compose-the-key option — sessions are keyed `tenantID + "\x00" + sessionID`. NUL never appears in a server-generated tenant id, so no client `session_id` can forge another tenant's namespace. Empty tenant (anonymous/single-tenant) reproduces the pre-tenancy key space, so single-tenant behavior is byte-identical. `Store` interface left unchanged (zero ripple). Two internal tests that fetched the store by raw id were updated to scope their lookups.
  - **Done when:** ✅ `TestEngine_SessionsIsolatedAcrossTenants` (two tenants + anonymous reusing one `session_id` stay independent) and `TestEngine_SingleTenantSessionBehaviorUnchanged` pass; regression-verified (test fails `turn=3` when the helper is neutered). Full suite green; gofmt/vet clean.
  - **Note:** `X-03` (sessions also ignored `agentName` for an existing id) was **fixed as a follow-up** — the key now includes the agent (see P2).

## P2 — Schedule

- [x] **Key sessions by agent (and tenant) — stop `GetOrCreate` ignoring `agentName`** — `X-03` · effort:S · owner:claude · **DONE 2026-05-30**
  - `scopedSessionKey(tenantID, agentName, sessionID)` now includes the agent (`tenant\x00agent\x00session`). Pipelines were already per-node-scoped (`pipeline.go:202`), so nothing relied on cross-agent session sharing. **Done when:** ✅ `TestEngine_SessionsIsolatedAcrossAgents` (two agents reusing one `session_id` stay independent; regression-verified — fails `turn=3` when the agent component is dropped). Full suite green.
- [x] **Honor request cancellation — ctx-aware chaos sleeps + early-out** — `X-01` / `F-EN-004` / `F-CH-001` · effort:M · owner:claude · **DONE 2026-05-30**
  - `Chaos.Before/After` now take `ctx`; injected latency/timeout sleep via `select { <-timer.C / <-ctx.Done() }` (`ChaosInjector.sleep`). Engine adds a cheap `ctx.Err()` early-out before any work and passes `ctx` to chaos. Also removed the dead `_ = ctx` (F-EN-003).
  - **Scope boundary (deliberate):** did **not** thread `ctx` into `Matcher`/`Generator`/`ProcessToolCalls`/`Save` — those are CPU-bound and non-blocking, so a ctx check there is noise; the single early-out + cancellable sleeps cover the real blocking paths. Pipeline `Run` ctx (`F-PL-009`) is still **open** (separable, low value).
  - **Done when:** ✅ `TestChaos_AfterHonorsContextCancellation` (10 s latency returns <1 s on cancel) and `TestEngine_CancelledContextShortCircuits` (returns `context.Canceled`). Full suite green.
- [x] **Clamp unbounded chaos latency** — `F-CH-002` · effort:S · **DONE 2026-05-30** — normal-distribution draw capped at `maxChaosLatencyMs` (60 s). _(`F-RG-002` `random_string` length clamp is **still open** — separate file, P2 below.)_
- [ ] **Clamp `random_string` length** — `F-RG-002` · effort:S — guard `length<=0`→"" and cap (~4096) in `response_generator.go`. **Done when:** `{{ random_string 1e9 }}` can't allocate GBs.
- [x] **Extract one type-aware scalar comparison** — `X-04` / `F-TP-001` / `F-TV-005` · effort:S · owner:claude · **DONE 2026-05-30**
  - Added shared `equalScalar(a,b)` + `toFloat` in `tool_validator.go`: numeric kinds (int/int32/int64/float32/float64) compare by value (`1 == 1.0`), everything else via `reflect.DeepEqual` (safe across mismatched kinds). `valuesEqual` (tool_processor) and `inEnum` (tool_validator) now both delegate to it; the duplicated `fmt.Sprintf("%v")` logic is gone.
  - **Done when:** ✅ `1 != "1"`, `true != "true"` in both match-rule and enum checks; `1 == 1.0` still matches. Tests: extended `TestValuesEqual`, new `TestEqualScalar_TypeAware` + `TestToolValidator_inEnum_TypeAware`. Full suite green.
- [x] **Fix tool-call swallow cases** — `F-EN-001` / `F-EN-002` · effort:S · owner:claude · **DONE 2026-05-30**
  - Dropped the `&& len(agent.Spec.Tools) > 0` guard so tool calls are always processed; the no-tools misconfiguration now (a) logs a distinct warning and (b) surfaces each call as an error *result* (`TOOL_NOT_FOUND`) instead of silent-empty (F-EN-001). Documented the **best-effort** contract: per-call `IsError`/`Error` carry failures, the turn doesn't fail, and the error path logs "surfaced as error results" (F-EN-002).
  - **Done when:** ✅ `TestEngine_ToolCallsWithoutToolDefs_SurfacedAsErrorResults` — turn succeeds, `ToolResults[0].IsError` + `TOOL_NOT_FOUND`. Full suite green.
- [ ] **Validate object schemas more completely** — `F-TV-001`/`F-TV-002` (+ `F-TV-003` **↓ S3**) · effort:M
  - `F-TV-003` is **largely mitigated**: the config validator *requires* a tool-param `type` (`validator.go:241`) and skips registration without it, so the runtime "absent type skips validation" path is only reachable via programmatic agents. **Still real:** `F-TV-001` (no nested-schema recursion — the validator doesn't recurse either) and `F-TV-002` (malformed `properties` early-return skips `additionalProperties`). **Done when:** nested `required`/`type` constraints are enforced (or the single-level limit is documented); tests in `F-TV-009`.
- [x] **`X-07` Wire `ValidatePipeline` into the server start path** — `X-07` (new, from validator investigation) · effort:S · owner:claude · **DONE 2026-05-31**
  - Extracted the pipeline-loading loop from `runStart` into `registerPipelines(pipelines, logger) *engine.PipelineRegistry` (`start.go`). It now calls `config.ValidatePipeline(def, filePath, node)` before `Register` and skips-on-failure (logs `"skipping invalid pipeline"`), mirroring the agent path. So the full cycle + reachability validator (`pipeline_validator.go`) now runs under `mockagents start`/`make run`, not just `mockagents validate`/GUI. **Watcher unchanged:** `watcher.go` only live-reloads Agent-kind docs (confirmed lines 21/140/154), so pipelines are boot-only — no hot-reload path to wire. **Done when:** ✅ `TestRegisterPipelines_SkipsCyclic` (a→b→a cycle registered 0, valid one registered 1) — regression-verified (fails "got 2" when the validation guard is neutered to `&& false`). `TestRegisterPipelines_SkipsNilAndAllValid` covers nil-guard + happy path. gofmt/vet clean.
  - **Note:** the validator↔executor multi-root mismatch (`F-PL-004`) is **not** closed by this — see next item.
- [ ] **Pipeline graph: detect cycles, don't drop nodes (engine-side hardening)** — `F-PL-003` / `F-PL-004` / `F-PL-005` · effort:M
  - Defense-in-depth for programmatic pipelines + the validator↔executor reachability mismatch: a **multi-root DAG passes `ValidatePipeline` but the executor walks one source and drops the rest** (`F-PL-004` stands even with X-07). Reject/ignore cycles in `runGraph`; traverse all roots. **Done when:** a cyclic pipeline errors and a multi-root DAG visits every node.
- [ ] **Surface swallowed regex compile errors** — `F-SM-001` · **↓ S3** (validator rejects bad `content_regex` at agent load — `validator.go:203` — and skips registration, so this is defense-in-depth for programmatic agents only) · effort:S
  - Log/return on bad `content_regex` instead of silent non-match. **Done when:** a bad pattern is diagnosable, not indistinguishable from no-match.
- [ ] **Session store concurrency hardening** — `F-ST-003` (Get TOCTOU) + `F-ST-005` (cleanup lock-hold) + `F-SS-002` (live `Variables`) · effort:M
  - Compare-and-delete the specific pointer in `Get`; snapshot-then-delete in `Cleanup`; document `Variables` lock contract. **Done when:** stress test shows no lost fresh sessions / no `Get`-`GetOrCreate` blocking during cleanup.
- [ ] **Hot-path alloc nits** — `F-SM-007` (lower-case once) + `F-EN-006` (pre-size `toolCallMsgs`) · effort:S
  - **Done when:** `make bench-report` shows no regression and these allocs are gone.
- [ ] **Registry determinism + docs** — `F-AR-002` (tie-break) + `F-AR-003` (document last-writer-wins) · effort:S
- [ ] **Pipeline edge `When` semantics** — `F-PL-010` · effort:S — confirm substring-vs-equality is intended; document or tighten.
- [ ] **Add the missing tests** — `F-TV-009` + `F-ST-008` · effort:M — validator edge cases; store concurrency/TTL (stress loops, `-race` unavailable).

## P3 — Opportunistic (cheap, low-risk)

- [ ] ~~`F-EN-003` remove dead `_ = ctx`~~ ✅ (done with X-01) · `F-EN-009` remove dead `resolveAgent` + stale comment · `F-TP-002` dead `&& !rule.IsDefault` · `F-TP-006` unused `ErrNoToolResponse` · `F-TV-008` unused `FormatValidationError` · `F-SM-006` dead guard — **dead-code sweep**, effort:S total.
- [ ] `F-ST-004`/`X-06` remove redundant `Save` · `F-ST-006`/`F-ST-007` document aliasing + key on `id` · `F-SS-003` single `time.Now()` · `F-SS-006` clamp `NewSession` ttl=0 · `F-SS-001` document closure re-entry rule.
- [ ] `F-RG-003`/`F-RG-008` document quantized/floor behavior · `F-RG-007` restrict `to_json` · `F-RG-001` bound template cache (or assert author-static) · `F-RG-004` decide `missingkey` policy.
- [ ] `F-CH-003` lock only around RNG draws · `F-CH-004` clamp `rate` · `F-CH-006` note bucket growth · `F-AR-004`/`F-PR-002` nil-`def` guards · `F-PR-003` note List cost · `F-RM-001` reword "clears".
- [ ] `F-PL-001`/`F-PL-002` add defensive `nr.Response == nil` guards (hardening; not a live bug) · `F-PL-006` document partial-result-on-error or `errors.Join` · `F-TP-004` non-constant tool-call-id fallback.

## Workstream clusters (batch these)

- **Tenant/session isolation:** `X-02` + `X-03` + `F-ST-006` — one change to the store key/contract. **Highest value.**
- **Context & cancellation:** `X-01` + `F-EN-004` + `F-CH-001` + `F-PL-009` — one ctx-threading pass.
- **Type-aware comparison:** `X-04` + `F-TP-001` + `F-TV-005` — one shared helper.
- **Schema-validation completeness:** `F-TV-001/002/003/006` + `F-TV-009` — one validator pass + tests.
- **Pipeline graph correctness:** `F-PL-003/004/005` + `F-PL-010`.
- **Dead-code sweep:** the P3 first bullet — one trivial commit.

## Needs investigation (gate severities — read before acting)

- [ ] **`X-05` registry reload atomicity** — read `cmd/mockagents/start.go` + `internal/server/watcher.go`. If reload builds a fresh registry and swaps, `F-PR-001`/`X-05` are moot; if it mutates in place via repeated `Register`, they're real (stale entries + torn reads). 
- [x] **Config-validator coverage — RESOLVED 2026-05-31.** Read `internal/config/validator.go`, `pipeline_validator.go`, `cmd/mockagents/start.go`, `watcher.go`. Findings:
  - **Agent validation is a hard gate.** `start.go:94-100` and `watcher.go:162` run `Validator.Validate` and **skip registration on any error** (`continue`). So for YAML-loaded agents the validator already catches: bad `content_regex` (`validator.go:203`), missing tool-param `type` (`:241`), and **scenario `tool_calls` referencing an undefined tool** (`:258`). → **Downgrade `F-SM-001` and `F-TV-003` to S3** (defense-in-depth), and note `F-EN-001`'s fix is defense-in-depth too. **Caveat:** programmatic agents (Go SDK `NewInProcessClient`, tests building structs directly) bypass the validator, so the engine-side guards still earn their keep.
  - **NOT validated anywhere:** multiple `default:true` scenarios (`F-SM-005` stands), out-of-range chaos `rate`/`stddev`/latency (`F-CH-002` already fixed; `F-CH-004` stands), and **nested** schema constraints (`F-TV-001` stands — `validateJSONSchema` only checks top-level `type` presence, no recursion).
  - **Pipeline validator is thorough but NOT wired into the server.** `ValidatePipeline` (3-color cycle DFS + BFS reachability, `pipeline_validator.go:228-368`) is only called by `mockagents validate` and `ValidateBytes` (GUI) — **`start.go:120-125` registers pipelines with no validation.** So `F-PL-003`/`F-PL-004` are reachable at runtime via `mockagents start` (what `make run` uses). → New finding **X-07** below; `F-PL-003`/`F-PL-004` keep S2. (Even if wired: the validator's reachability is from *all* sources, but the executor walks one — so a multi-root DAG passes validation yet the executor drops nodes. `F-PL-004` is a genuine validator↔executor mismatch.)
- [ ] **`StartCleanupTicker` shutdown** — confirm the server calls the returned stop func (else goroutine+ticker leak).
- [ ] **`behavior.go` chaos doc drift** — verify whether `Timeouts.AfterMs`/`Types` referenced in doc comments still exist on the struct (lives in `internal/types`).
