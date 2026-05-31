# Review Summary — internal/engine package audit

- **Target:** `internal/engine/` (full-package audit, as-is — not a diff)
- **Reviewed at:** 2026-05-30  ·  **Depth:** deep
- **Scope:** 12 production files, ~2,180 LOC — `engine.go`, `agent_registry.go`, `scenario_matcher.go`, `response_generator.go`, `tool_processor.go`, `tool_validator.go`, `pipeline.go`, `pipeline_registry.go`, `chaos.go`, `reqmeta.go`, `state/session.go`, `state/store.go`
- **Method:** Pass 1 fanned out to 8 isolated per-file reviewers (subagents); Pass 2 cross-file done single-threaded; all S0/S1 adversarially verified against the real code before entering the plan.
- **Reviewer:** multi-pass-review skill

## Verdict

> **GO WITH FIXES (not merge-blocking — this is existing code).** No S0 survived verification. One S1 worth prioritizing: the session store is not tenant-scoped while everything around it is (X-02). The rest is a healthy backlog of correctness-completeness and robustness gaps typical of a maturing engine; none are crashes-on-the-happy-path.

## Findings by severity (post-verification)

| Severity | Count | Notable IDs |
|----------|-------|-------------|
| S0 Blocker | 0 | — (2 candidates refuted, see below) |
| S1 High    | 1 | X-02 (F-ST-001) |
| S2 Medium  | ~22 | X-01, X-03, X-04, F-EN-001/002, F-TV-001/002/003, F-PL-003/004, F-CH-002, F-ST-002/003/005, F-SM-001 |
| S3 Low     | ~20 | F-EN-003/006/009, F-PL-001/002 (downgraded), F-ST-004/006, F-RG-002/007, F-TP-002/004, nits |

## Top risks (what matters)

1. **Session store isn't tenant/owner-scoped** (`X-02` / `F-ST-001`, S1) — agents are tenant-isolated but `GetOrCreate(req.SessionID, …)` keys on the raw client session id with no tenant component (`engine.go:127`). In multi-tenant mode two tenants sharing a `session_id` share conversation history/variables. **Escalates to S0 if per-tenant session isolation is a product guarantee.**
2. **`GetOrCreate` ignores `agentName` on an existing id** (`X-03`, S2) — a session created for agent A is returned verbatim for a later request naming agent B with the same id; the `agentName` arg is only used at creation (`store.go:58-71`).
3. **Request `context` dies at `engine.go:88`** (`X-01`, S2) — nothing downstream (session, matcher, generator, tool processor, chaos sleeps, store) receives `ctx`, so client cancellation/deadline can't abort work and injected chaos latency (`chaos.go`, unbounded — `F-CH-002`) blocks the goroutine ignoring disconnect.
4. **Two duplicated stringly-typed comparisons** (`X-04`, S2) — `valuesEqual` (`tool_processor.go:191`) and `inEnum` (`tool_validator.go:166`) both use `fmt.Sprintf("%v",…)`, so `1 == "1"` and `true == "true"` match; they will rot independently.
5. **Tool-call handling swallows two cases** (`F-EN-001/002`, S2) — scenario emits tool calls but agent declares none → silent empty results; `ProcessToolCalls` error is logged `Warn` and still surfaced as success.

## Refuted / downgraded during verification (shown for honesty)

- **F-PL-001, F-PL-002 (claimed S0 nil-deref):** refuted. `ProcessRequest` never returns `(nil, nil)` — `ApplyTurn` always runs the closure and `resp` is always set non-nil on the success path. Kept as S3 defensive guards.
- **F-ST-002 (claimed S0 unbounded growth):** downgraded to S2 — real, but TTL cleanup + ticker mitigate; not a guaranteed break.
- **F-AR-001 (claimed S1 tenant leak):** withdrawn — global agents are intentionally visible to all tenants, so a bogus tenant receiving *globals* is by-design, not a leak of another tenant's agents.
- **F-SM-002 (claimed S1 ReDoS):** withdrawn to informational — Go's RE2 is linear-time; no catastrophic backtracking. (Input-size cap remains a nice-to-have.)
- **"Validator silently no-ops" worry:** refuted — `JSONSchemaObject` is `map[string]any` (`types/tool.go:17`) and yaml.v3 yields string keys, so the validator's type assertions hold at every depth.

## Coverage & confidence

- Passes run: 0,1,2,3,4. All S0/S1 verified against source (`engine.go`, `state/*`, `types/tool.go` read directly).
- **Not covered / blind spots:** (a) reload atomicity for both registries (`F-AR`/`F-PR-001`/`X-05`) depends on how `cmd/mockagents/start.go` + `internal/server/watcher.go` perform reloads — **not read**, left in "needs investigation"; (b) whether `config` validation already rejects bad regex / multiple `default:true` / cyclic pipelines / out-of-range chaos rates — **not read**, several S2 severities are conditional on it; (c) `_test.go` files were not audited (production code only); (d) `-race` is unavailable on this repo (no-cgo), so concurrency findings are by inspection, not the race detector.

## Where to act

→ Execute **`03-ACTION-PLAN.md`** (start at P1 — X-02). Per-file detail in `01-PER-FILE.md`; cross-file detail + refutations in `02-INTEGRATION.md`.
