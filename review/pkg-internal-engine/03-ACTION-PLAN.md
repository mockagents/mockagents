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
  - **Note:** `X-03` (sessions also ignore `agentName` for an existing id) is **still open** — this change scopes by tenant only, not by agent. See P2.

## P2 — Schedule

- [ ] **Key sessions by agent (and tenant) — stop `GetOrCreate` ignoring `agentName`** — `X-03` · effort:S · folds into X-02
  - `engine.go:127` + `store.go:58-71`. **Done when:** a request naming agent B with an id first used by agent A does not receive A's session.
- [ ] **Thread `context` through the engine pipeline + chaos sleeps** — `X-01` / `F-EN-004` / `F-CH-001` · effort:M
  - Pass `ctx` into `ApplyTurn`/`Generator`/`ProcessToolCalls`/`Save` and `Chaos.Before/After`; sleep via `select { case <-time.After(d): case <-ctx.Done(): }`; add `ctx` to pipeline `Run`. **Done when:** a cancelled request aborts before/at the next turn boundary and injected latency returns early on cancel.
- [ ] **Clamp unbounded sleeps/allocations from config** — `F-CH-002` + `F-RG-002` · effort:S
  - Cap normal-distribution latency to a max; clamp/guard `random_string` length (`length<=0`→"", cap ~4096). **Done when:** a pathological `stddev_ms`/`random_string N` can't block seconds / allocate GBs.
- [ ] **Extract one type-aware scalar comparison** — `X-04` / `F-TP-001` / `F-TV-005` · effort:S
  - Replace both `fmt.Sprintf("%v",…)` compares with a shared `equalScalar` (numeric→float compare, else typed). **Done when:** `1 != "1"` and `true != "true"` in both match-rule and enum checks, with a test.
- [ ] **Fix tool-call swallow cases** — `F-EN-001` / `F-EN-002` · effort:S
  - Warn (or process) when a scenario emits tool calls but the agent declares none; decide abort-vs-best-effort on `ProcessToolCalls` error and stop surfacing a failed resolution as success. **Done when:** both cases log/return distinctly; documented.
- [ ] **Validate object schemas that omit top-level `type`** — `F-TV-003` (+ `F-TV-001`/`F-TV-002`) · effort:M
  - Treat absent `type` with `properties`/`required` as an object; don't early-return on malformed `properties`; recurse nested schemas (or document the single-level limit). **Done when:** a `parameters` block without `type: object` still enforces `required`; tests in `F-TV-009`.
- [ ] **Pipeline graph: detect cycles, don't drop nodes** — `F-PL-003` / `F-PL-004` / `F-PL-005` · effort:M
  - Reject (or document) cycles instead of silent truncation; traverse all roots / unvisited nodes. **Done when:** a cyclic pipeline errors and a multi-root DAG visits every node.
- [ ] **Surface swallowed regex compile errors** — `F-SM-001` · effort:S (after checking validator — see needs-investigation)
  - Log/return on bad `content_regex` instead of silent non-match. **Done when:** a bad pattern is diagnosable, not indistinguishable from no-match.
- [ ] **Session store concurrency hardening** — `F-ST-003` (Get TOCTOU) + `F-ST-005` (cleanup lock-hold) + `F-SS-002` (live `Variables`) · effort:M
  - Compare-and-delete the specific pointer in `Get`; snapshot-then-delete in `Cleanup`; document `Variables` lock contract. **Done when:** stress test shows no lost fresh sessions / no `Get`-`GetOrCreate` blocking during cleanup.
- [ ] **Hot-path alloc nits** — `F-SM-007` (lower-case once) + `F-EN-006` (pre-size `toolCallMsgs`) · effort:S
  - **Done when:** `make bench-report` shows no regression and these allocs are gone.
- [ ] **Registry determinism + docs** — `F-AR-002` (tie-break) + `F-AR-003` (document last-writer-wins) · effort:S
- [ ] **Pipeline edge `When` semantics** — `F-PL-010` · effort:S — confirm substring-vs-equality is intended; document or tighten.
- [ ] **Add the missing tests** — `F-TV-009` + `F-ST-008` · effort:M — validator edge cases; store concurrency/TTL (stress loops, `-race` unavailable).

## P3 — Opportunistic (cheap, low-risk)

- [ ] `F-EN-003` remove dead `_ = ctx` · `F-EN-009` remove dead `resolveAgent` + stale comment · `F-TP-002` dead `&& !rule.IsDefault` · `F-TP-006` unused `ErrNoToolResponse` · `F-TV-008` unused `FormatValidationError` · `F-SM-006` dead guard — **dead-code sweep**, effort:S total.
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
- [ ] **Config-validator coverage** — does `internal/config` already reject bad `content_regex`, multiple `default:true`, cyclic pipelines, and out-of-range chaos `rate`/`stddev`? If yes, downgrade `F-SM-001`, `F-SM-005`, `F-PL-003`, `F-CH-002/004` to documentation tasks.
- [ ] **`StartCleanupTicker` shutdown** — confirm the server calls the returned stop func (else goroutine+ticker leak).
- [ ] **`behavior.go` chaos doc drift** — verify whether `Timeouts.AfterMs`/`Types` referenced in doc comments still exist on the struct (lives in `internal/types`).
