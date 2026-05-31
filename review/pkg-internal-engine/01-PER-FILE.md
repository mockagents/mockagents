# Per-File Findings (Pass 1) — internal/engine package audit

_Each file judged in isolation by a dedicated reviewer. Severities are post-verification (deep mode). Cross-file issues live in `02-INTEGRATION.md`. ✎ = severity adjusted from the raw Pass-1 rating after adversarial verification._

## `engine.go` · orchestration entry point

| ID | Sev | Conf | Line | Dimension | Evidence → Fix |
|----|-----|------|------|-----------|----------------|
| F-EN-001 | S2 | High | :184 | Correctness | `if len(resp.ToolCalls) > 0 && len(agent.Spec.Tools) > 0` — scenario emits tool calls but agent declares no tools → `ToolResults` silently empty, no warning. Log a warning in the `else`, or process unconditionally and let `ProcessToolCalls` report the missing definition. |
| F-EN-002 | S2 | High | :185-192 | Error handling | `ProcessToolCalls` error is logged `Warn`, then `resp.ToolResults = results` and the turn returns success — a failed/partial tool resolution is surfaced as success. Decide abort-vs-best-effort and document it. |
| F-EN-004 | S2 | Med | :73-217 | Concurrency | `ctx` is read once at :88 then never threaded into `GetOrCreate`/`ApplyTurn`/`Matcher`/`Generator`/`ProcessToolCalls`/`Save`/`Chaos`. Client cancel/deadline can't short-circuit. (Root of cross-file **X-01**.) Thread `ctx` or add `ctx.Err()` checks at turn boundaries. |
| F-EN-006 | S3 | Med | :200-206 | Performance | `var toolCallMsgs []state.ToolCallMsg` + `append` in a loop, unsized → `growslice` on the hot path. `make([]state.ToolCallMsg, 0, len(resp.ToolCalls))`. |
| F-EN-003 | S3 | High | :115 | Readability | `_ = ctx` is dead — `ctx` is already used at :88. Remove. |
| F-EN-009 | S3 | High | :226-228 | Dead code | `resolveAgent` has no callers (confirmed by grep + LSP `unusedfunc`); its doc comment claims "legacy tests and the in-process Go SDK" use it, but they don't. Remove method + stale comment. |
| F-EN-005 | S3 | Med | :211 | Error handling | `e.States.Save(session)` return ignored — but `Save` is `void` in the `Store` interface (`store.go:17`) and redundant given in-place mutation (see **X-06**). Not an error-handling bug; tracked as X-06. |
| F-EN-007/008 | S3 | Low | :108 / :106-114 | Security / Consistency | `Logger.Info(..., "error", chaosErr)` log-injection only if chaos errors ever embed author content (low). `Chaos` is nil-guarded but `Logger` is not — inconsistent; document that `Logger` is required. |

## `agent_registry.go` · agent lookup + tenant visibility

| ID | Sev | Conf | Line | Dimension | Evidence → Fix |
|----|-----|------|------|-----------|----------------|
| F-AR-002 | S2 ✎ | High | :174-187 | Correctness | `GetByModelForTenant` returns the first map-iteration match → non-deterministic when two same-tenant agents share a model; disagrees with the deterministic `byModel` index path. Define a tie-break (sort by name / mirror last-writer-wins). |
| F-AR-003 | S2 | High | :33-47 | Doc/contract | `Register` silently does last-writer-wins on a model collision (both stay in `agents`, only the last owns `byModel[m]`), so `GetByModel` and `List` disagree. Behavior is by-design but undocumented on `Register`. Document it. |
| F-AR-004 | S3 | Med | :36 | Error handling | `Register` derefs `def.Metadata.Name`/`def.Spec.Model` with no nil-`def` guard → panic under the write lock. Early `if def == nil { return }`. |
| F-AR-005 | S3 | High | :170-187 | Performance | `GetByModelForTenant` is an O(n) scan per tenant-scoped lookup, defeating the `byModel` index for tenant deployments. Optional: `byModelTenant` index kept in lockstep. |
| F-AR-006 | S3 | Med | :65-76,191-204 | Performance | `ListForTenant` pre-sizes capacity to `len(r.agents)` even when most are filtered out — minor over-alloc. Acceptable. |
| F-AR-001 | — | — | :178 | _Withdrawn_ | Claimed tenant leak refuted: global agents are intentionally shared, so a bogus/unknown tenant receiving *globals* is by-design, not a cross-tenant leak. |

## `scenario_matcher.go` · match precedence

| ID | Sev | Conf | Line | Dimension | Evidence → Fix |
|----|-----|------|------|-----------|----------------|
| F-SM-001 | S2 ✎ | High | :129,92-94 | Error handling | `compileRegex` drops the `regexp.Compile` error and returns `nil`; `evaluate` then treats a bad `content_regex` as "not matched" — indistinguishable from a legit non-match, no log. Surface/log it (or rely on validator — see backlog). |
| F-SM-007 | S2 | High | :84 | Performance | `strings.ToLower(userMessage)` recomputed per scenario inside one `MatchWithCaptures` → N full-message allocs. Lowercase once before the loop. Hot path. |
| F-SM-004 | S2 | High | :82-119 | Correctness | A present-but-empty `Match` rule (all fields zero) falls through every guard and matches everything (`matchedSentinel`), shadowing later scenarios. Confirm intended (wildcard) or reject in validation. |
| F-SM-005 | S2 | Low | :50-52 | Correctness | Multiple `Match==nil` scenarios → only the first is kept as default; later defaults silently ignored. "First default wins" is undocumented. Document or reject multiples. |
| F-SM-003 | S2 | High | :23,133 | Memory | `regexCache sync.Map` never evicts; benign for config-sourced patterns, unbounded if patterns ever become request-derived. Use `LoadOrStore`; document the bound. |
| F-SM-006 | S3 | Med | :103 | Readability | `i < len(match)` guard is dead (`SubexpNames()` and `FindStringSubmatch` are equal length). Remove or comment. |
| F-SM-002 | — | — | :91,129 | _Withdrawn → S3 info_ | ReDoS refuted (RE2 is linear). Residual nice-to-have: cap input/message size. |

## `reqmeta.go` · context/tenant carrier — **cleanest file in the package**

| ID | Sev | Conf | Line | Dimension | Evidence → Fix |
|----|-----|------|------|-----------|----------------|
| F-RM-001 | S3 | Med | :35-37 | Doc accuracy | Doc says `WithTenantID(ctx,"")` "clears"; it actually shadows with `""`. Observable result matches, wording doesn't. Reword to "overrides with empty (treated as no tenant)". |
| _note_ | ✅ | — | — | Positive | Context keys are unexported zero-size structs (`tenantKey{}`, `requestMetaKey{}`) — no string-key collisions; engine correctly avoids importing `tenancy` (CLAUDE.md rule honored). |

## `response_generator.go` · template expansion _(helpers added last task; re-audited)_

| ID | Sev | Conf | Line | Dimension | Evidence → Fix |
|----|-----|------|------|-----------|----------------|
| F-RG-002 | S2 | High | :175-187 | Security (DoS) | `randomString(length)` does `make([]byte, length)` with no bound; `{{ random_string 1000000000 }}` allocates ~1 GB; negative length panics. Author-controlled (tenant-triggerable in multi-tenant). Clamp to a max + guard `length<=0`. |
| F-RG-001 | S3 ✎ | High | :118-126 | Memory | `sync.Map` template cache keyed on the full content string, never evicted. Bounded while content is author-static; unbounded if content ever interpolates per-request data. Bound it or assert keys come only from loaded scenarios. (Backlog gates severity.) |
| F-RG-007 | S3 | High | :211-217 | Info leak | `to_json` marshals any value; `{{ to_json . }}` serializes the whole `TemplateContext`/`AgentDefinition` (incl. `SystemPrompt`, `Vars`). Restrict to explicit data or document. |
| F-RG-003 | S3 | High | :189-198 | Correctness | `randomFloat` is quantized to 10^6 steps and never reaches `max` (`[min,max)`). Document the half-open quantized range. |
| F-RG-008 | S3 | High | :164-173 | Correctness | `randomInt` silently returns `min` when `min>=max` (`{{ random_int 10 5 }}`→10, no diagnostic). Document. |
| F-RG-004 | S3 | Med | :122 | Error handling | No `Option("missingkey=error")`; `{{ .Vars.missing }}` renders `<no value>` silently. Decide strict-vs-lenient project-wide. |
| F-RG-005 | S3 | High | :112 | Correctness | Fast-path `Contains(content,"{{")` hard-errors on unbalanced `{{` in otherwise-literal prose. Consider falling back to literal when no `}}`. |
| F-RG-006 | ✅ | — | :247-254 | — | `title` confirmed rune-safe (last task's fix holds). No action. |

## `tool_processor.go` · simulated tool calls

| ID | Sev | Conf | Line | Dimension | Evidence → Fix |
|----|-----|------|------|-----------|----------------|
| F-TP-001 | S2 | High | :191-199 | Correctness | `valuesEqual` uses `fmt.Sprintf("%v",…)` → `"1"==1`, `"true"==true` match; type-confusing tool-response match criteria. Type-aware compare. (Dup with F-TV-005 → **X-04**.) |
| F-TP-003 | S2 | Med | :66-72 | Error handling | On a non-validation error the returned `results` slice is partially populated with ambiguous contract. Document what `results` holds when `err!=nil`, or return `nil`. |
| F-TP-002 | S3 | High | :150 | Dead code | `&& !rule.IsDefault` is always-true (the `IsDefault` branch already `continue`d). Simplify. |
| F-TP-004 | S3 | Low | :209-214 | Correctness | `generateToolCallID` returns constant `"call_000000000000"` on `rand.Read` failure → ID collision/predictability. Propagate error or counter fallback. |
| F-TP-005 | S3 | Med | :108,217-229 | Correctness | Error-injection granularity is 0.01% (`rand.Int(…,10000)`); finer rates silently floored. Document. |
| F-TP-006 | S3 | Low | :16 | Dead code | `ErrNoToolResponse` declared, never used (the `resp==nil` path returns a `{"status":"ok"}` fallback instead). Wire it up or remove. |

## `tool_validator.go` · tool argument validation

| ID | Sev | Conf | Line | Dimension | Evidence → Fix |
|----|-----|------|------|-----------|----------------|
| F-TV-003 | S2 | Med | :32-35 | Correctness | Only `type=="object"` triggers validation; a schema that omits top-level `type` (common — params implicitly an object) gets **no** validation at all, incl. `required`. Treat absent type + `properties`/`required` as object. |
| F-TV-001 | S2 | High | :62-112 | Correctness | Nested object/array schemas aren't recursed — only top-level prop types checked. Incomplete vs the "validates against JSON Schema" doc. Recurse or document the single-level limit. (No stack-overflow risk — there is *no* recursion.) |
| F-TV-002 | S2 | High | :63-66 | Error handling | A malformed `properties` value early-returns, skipping the `additionalProperties` check entirely. Fall through instead. |
| F-TV-005 | S2 | Med | :166-173 | Correctness | `inEnum` uses `fmt.Sprintf("%v",…)` — same stringly-typed flaw as F-TP-001 (**X-04**). |
| F-TV-004 | S2 | Med | :135-143 | Correctness | `integer` check casts via `float64`; loses precision for `|n|>2^53`; `uint64`/`json.Number` unhandled. Low practical impact; note. |
| F-TV-006 | S3 | Med | :114-124 | Correctness | `additionalProperties` only honored when exactly `false`; schema-object form ignored (allows everything). Document or implement. |
| F-TV-007 | S3 | High | :188-198 | Correctness | `toInt(float64)` truncates (`minLength:2.9`→2) silently. |
| F-TV-008 | S3 | Low | :200-203 | Dead code | `FormatValidationError` exported but `tool_processor.go` uses its own formatter; verify a caller exists (e.g. adapter) or remove. |
| F-TV-009 | S2 | — | file | Tests | No tests for malformed `required`/`properties`, enum coercion, nested schemas, `additionalProperties:false`, absent top-level type. |

## `pipeline.go` · pipeline/DAG execution

| ID | Sev | Conf | Line | Dimension | Evidence → Fix |
|----|-----|------|------|-----------|----------------|
| F-PL-003 | S2 ✎ | High | :151-193 | Correctness | Comment promises "cyclic pipelines unsupported" but cycles aren't detected — `visited` just truncates traversal → silent partial execution, no error. Detect cycles and error, or document silent-break. (No stack overflow — `visited` bounds it.) |
| F-PL-004 | S2 | High | :159-194 | Correctness | Traversal starts from a single root; multi-root or disconnected nodes are silently dropped from `res.Nodes`. Walk all zero-incoming roots / all unvisited nodes. |
| F-PL-006 | S2 | High | :109-134 | Concurrency | `runParallel` returns only the first error and still appends successful nodes → partial `res` + error. Document the contract or `errors.Join`. |
| F-PL-005 | S2 | Med | :152-161 | Correctness | All-inbound graph falls back to `Agents[0]` as start, conflating "fully cyclic" with a legit entry (compounds F-PL-003). |
| F-PL-010 | S2 | High | :183 | Correctness | Edge condition uses `strings.Contains(content, edge.When)` — a substring probe, so `When:"no"` matches "another". `When` reads like a predicate; confirm intended semantics. |
| F-PL-001 | S3 ✎ | — | :104 | _Refuted S0→S3_ | Nil-deref requires `ProcessRequest`→`(nil,nil)`, which **cannot happen** (verified: `ApplyTurn` always sets a non-nil `resp`). Keep a defensive `if nr.Response == nil` guard as hardening only. |
| F-PL-002 | S3 ✎ | — | :183,186 | _Refuted S0→S3_ | Same root as F-PL-001; defensive guard only. |
| F-PL-007/008/009/011 | S3 | Med | various | Concurrency/API | Engine assumed goroutine-safe across sessions (backlog); recursion depth == longest path; no ctx/cancellation in `Run`; `invokeNode` returns value+error footgun. |

## `pipeline_registry.go`

| ID | Sev | Conf | Line | Dimension | Evidence → Fix |
|----|-----|------|------|-----------|----------------|
| F-PR-001 | S2 | Med | :23-27 | Concurrency | No transactional bulk-replace for reloads; per-item `Register` leaves half-updated state visible to readers and never removes pipelines dropped from the new config. Add atomic `ReplaceAll`. (Cross-file **X-05**.) |
| F-PR-002 | S3 | Low | :23-27 | Error handling | No nil-`def`/empty-name guard; empty name keys under `""` and shadows. Guard. |
| F-PR-003 | S3 | Low | :38-47 | Performance | `List` allocates + sorts each call. Fine for admin endpoints. |

## `chaos.go` · fault injection

| ID | Sev | Conf | Line | Dimension | Evidence → Fix |
|----|-----|------|------|-----------|----------------|
| F-CH-001 | S2 ✎ | High | :97,104,164,175 | Concurrency | `Before`/`After` take no `ctx`; injected `Sleep`(latency/timeout) ignores client cancellation → blocked goroutine on disconnect. Thread `ctx`, `select{ case <-time.After(d): case <-ctx.Done(): }`. (Part of **X-01**.) |
| F-CH-002 | S2 | High | :153 | Correctness | Normal-distribution latency `NormFloat64()*Stddev+Mean` has no upper clamp → a tail draw blocks for seconds (with F-CH-001, ignoring cancel). Clamp to a max. |
| F-CH-003 | S3 ✎ | Med | :141-159 | Performance | `sampleLatency` locks `c.mu` even on the `fixed`/`default` no-randomness branches, serializing concurrent requests. Lock only around `RandSrc` draws. |
| F-CH-004 | S3 | Med | :166-168 | Correctness | `rate` not clamped; `>1.0`→always, `<0`→never, silently. Validate/clamp. |
| F-CH-005/006/007 | S3 | Low/Med | various | Readability/Memory | Explicit `uniform` with `Max<=Min` silently degrades to fixed; `buckets` map never pruned (bounded by agent count); timeout-branch doc undersells the blocking sleep. |
| _note_ | ✅ | — | :151-200 | Positive | Shared `*rand.Rand` is consistently mutex-guarded (no data race); chaos-off hot path allocates/locks nothing. |

## `state/session.go`

| ID | Sev | Conf | Line | Dimension | Evidence → Fix |
|----|-----|------|------|-----------|----------------|
| F-SS-002 | S2 | High | :57 | Concurrency | `ApplyTurn` passes the live `s.Variables` map into the caller's closure; no accessor guards `Variables`, so a concurrent read elsewhere races. Document "mutate only under the session lock". |
| F-SS-001 | S2 | Med | :53-57,38-42 | Concurrency | `ApplyTurn`/`WithLocked` hold `s.mu` while calling closures; if a closure re-enters a locking method (`LatestUserMessage`, `IsExpired`) → self-deadlock (`sync.Mutex` non-reentrant). Today's engine closure only reads `session.ID` (safe) but it's fragile. Document the constraint. |
| F-SS-006 | S3 | Low | :74-85,122-129 | API | `NewSession(…, ttl=0)` → never expires (`IsExpired` treats `TTL<=0` as eternal). Only `NewMemoryStore` defaults ttl; a direct `NewSession(…,0)` leaks. Clamp in `NewSession` too. |
| F-SS-003 | S3 | High | :94-118 | Performance | `time.Now()` called 2-3× per turn. Capture once and reuse. |
| F-SS-005 | S2 | Med | :122-129 | Concurrency | Lock order store→session (store holds its lock while calling `IsExpired`). Consistent here; flagged for the cross-file pass — ensure no inverse ordering exists anywhere. |

## `state/store.go`

| ID | Sev | Conf | Line | Dimension | Evidence → Fix |
|----|-----|------|------|-----------|----------------|
| F-ST-001 | **S1** | High | :44-71 | Security | No tenant/owner scoping — keyed purely on caller-controlled `id`. Cross-session/cross-tenant sharing in multi-tenant mode. **Headline / X-02.** Namespace the key by tenant, or store+verify owner. |
| F-ST-002 | S2 ✎ | High | :58-71 | Security | Unbounded map growth from unique ids (TTL only reclaims idle). Spray of unique ids outpaces TTL → memory pressure. Add a max-session cap / LRU. (Downgraded from S0 — TTL mitigates.) |
| F-ST-003 | S2 | High | :44-55 | Concurrency | `Get` RUnlocks, then `IsExpired`+`Delete(id)` unconditionally → TOCTOU: can delete a *fresh* session created by another goroutine under the same id between RUnlock and Delete. Compare-and-delete the specific pointer. |
| F-ST-005 | S2 | Med | :91-99 | Performance | `Cleanup` holds the write lock across the full scan + per-session `IsExpired` locks → blocks all `Get`/`GetOrCreate` for the scan. Snapshot ids under RLock, then delete in batches. |
| F-ST-004 | S3 ✎ | High | :73-77 | API | `Save` is redundant (GetOrCreate already stored the pointer; mutation is in-place) and can resurrect a concurrently-deleted session. (Cross-file **X-06**.) Remove `Save` or make it conditional. |
| F-ST-006 | S3 | High | :13,44 | API | `Get` returns the internal shared `*Session` pointer; callers mutate shared state without store-level sync. Document the aliasing contract on the interface. |
| F-ST-007 | S3 | Med | :69 | Readability | Keys on `session.ID` while the lookup used `id`; equal today, fragile if `NewSession` ever normalizes. Key on `id`. |
| F-ST-008 | S2 | — | file | Tests | No tests for concurrent access, TTL eviction, unknown-id, or the `Get` TOCTOU path. (`-race` unavailable; use stress loops.) |
