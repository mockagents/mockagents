# Cross-File Integration Findings (Pass 2) — internal/engine package audit

_Assumes each file is internally sound; inspects only the seams. Each finding names ≥2 related sites. Verified against source where it gated an S0/S1._

## Relationship / blast-radius map

```
                      ProcessRequestContext(ctx, req)            ── engine.go (orchestrator)
                                │
        tenantID = TenantIDFromContext(ctx)   ── reqmeta.go (typed ctx key)
                                │
       ┌────────────────────────┼───────────────────────────────┐
       ▼                        ▼                                ▼
 resolveAgentForTenant   States.GetOrCreate(req.SessionID,…)   Chaos.Before/After(agent)
   AgentRegistry           state.Store / MemoryStore             chaos.go
  (TENANT-SCOPED ✓)        (NOT tenant-scoped ✗  → X-02/X-03)   (no ctx → X-01)
       │                        │
       ▼                        ▼
 GetForTenant /          Session.ApplyTurn(closure)  ── state/session.go
 GetByModelForTenant       └─▶ Matcher.MatchWithCaptures ── scenario_matcher.go
                           └─▶ Generator.Generate        ── response_generator.go
                           └─▶ ToolProcessor.ProcessToolCalls ── tool_processor.go
                                                            └─▶ ToolValidator ── tool_validator.go

  Pipelines: pipeline.go ──Run──▶ Engine.ProcessRequest (per node) ; pipeline_registry.go (parallel to AgentRegistry)
```

Key seam observation: **`ctx` and `tenantID` both enter at the top but neither reaches the session/state layer.** That single gap produces X-01, X-02, and X-03.

## Findings

| ID | Sev | Conf | Related sites | Check | Evidence → Fix |
|----|-----|------|---------------|-------|----------------|
| **X-02** | **S1** | High | `engine.go:88-89` (tenant threaded into agent lookup) vs `engine.go:127` + `state/store.go:58-71` | Data flow / layering | The engine carefully constrains *agent* resolution by `tenantID`, then keys the *session* store on the raw `req.SessionID` with no tenant component. Two tenants sharing a `session_id` share history/variables in multi-tenant mode. The isolation boundary is inconsistent across the two stores. Namespace the session key by tenant (e.g. `tenantID + "\x00" + id`) or store+verify owner. **Escalate to S0 if per-tenant session isolation is a guarantee.** |
| **X-03** | S2 | High | `engine.go:127` (`GetOrCreate(req.SessionID, agent.Metadata.Name)`) + `state/store.go:58-71` | Data flow | `GetOrCreate` returns an existing session by id **ignoring `agentName`** (the arg is only used when creating). A request naming agent B with a session id first used by agent A gets A's session (and A's `AgentName`/history). Include agent (and tenant) in the key, or reject id reuse across agents. |
| **X-01** | S2 | High | `engine.go:88-217` + `chaos.go:97,164` + `pipeline.go:118` | Lifecycle / cancellation | `ctx` dies at `engine.go:88`; `GetOrCreate`/`ApplyTurn`/`Matcher`/`Generator`/`ProcessToolCalls`/`Save`/`Chaos.Before`/`After` all run context-free. Client cancel/deadline can't abort, and unbounded chaos latency (`F-CH-002`) sleeps ignoring disconnect. Thread `ctx` through the engine pipeline and chaos sleeps; pipeline `Run` should also accept a `ctx`. |
| **X-04** | S2 | High | `tool_processor.go:191` (`valuesEqual`) + `tool_validator.go:166` (`inEnum`) | Duplication / correctness | Two independent type-loose comparisons both use `fmt.Sprintf("%v",…)`, so `1 == "1"` and `true == "true"` match. Duplicated flawed logic that will rot apart. Extract one type-aware `equalScalar` helper and use it in both. |
| **X-06** | S3 | High | `engine.go:211` (`States.Save`) + `state/store.go:73-77` + `GetOrCreate` | Redundant contract | `Save` re-inserts a pointer the store already holds (session mutated in place under its own lock). It adds no persistence and can resurrect a concurrently-deleted/expired session. Remove the `Save` call + interface method, or make it a documented conditional upsert. |
| **X-05** | _needs-investig._ | Med | `agent_registry.go` (reload via `*ForTenant`) + `pipeline_registry.go:23-27` (`F-PR-001`) + `cmd/mockagents/start.go`/`internal/server/watcher.go` (**not read**) | Lifecycle / reload atomicity | Neither registry exposes a transactional bulk-replace; if hot-reload calls `Register` per item, readers see half-updated state and pipelines/agents dropped from the new config are never removed. **Whether this is real depends on the reload path** (fresh-registry-and-swap would make it moot). Read `start.go`/`watcher.go` to confirm before acting. |

## Checks performed (for auditability)

- [x] **Signature & call-contract consistency** — clean. `resolveAgentForTenant`, `GetForTenant`, `GetByModelForTenant`, `ListForTenant` signatures match their call sites in `engine.go`. The `Store` interface (`store.go:11-24`) matches `MemoryStore`'s methods.
- [x] **Interface ↔ implementation** — `MemoryStore` satisfies `state.Store`; `pipeline_registry` satisfies the `runner.PipelineRegistry` interface (verify nil-on-miss contract — backlog). No drift found.
- [x] **Data flow across boundaries** — **3 findings (X-01, X-02, X-03).** `ctx` and `tenantID` both fail to cross into the state layer.
- [x] **Layering & import direction** — clean. `engine` does **not** import `tenancy`; `reqmeta.go` uses typed context keys (CLAUDE.md rule honored). `state` is a leaf subpackage. No cycles.
- [x] **Duplication & divergence** — **1 finding (X-04).** Also noted: `agent_registry` and `pipeline_registry` are near-parallel implementations (acceptable; different value types).
- [x] **Contract/schema drift** — `JSONSchemaObject` is `map[string]any` (`types/tool.go:17`); yaml.v3 yields string keys, so validator assertions hold (the "silent no-op" worry was **refuted**). `behavior.go` chaos doc may reference fields that don't exist on the struct (`Timeouts.AfterMs`/`Types`) — flagged to backlog, lives in `types`, out of this package's scope.
- [x] **Concurrency & shared state** — registries and store use locks consistently; chaos `*rand.Rand` is mutex-guarded. Residual: F-ST-003 (Get TOCTOU), F-ST-005 (cleanup lock-hold), F-SS-001/002 (closure re-entry / live `Variables` map). Lock order store→session is consistent (F-SS-005) — no inverse found *within this package*; verify server layer.
- [x] **Error propagation** — F-EN-002 (tool error logged-Warn but surfaced as success) and F-SM-001 (regex error swallowed → silent non-match) are the two propagation breaks; both within-file, listed in `01`.
- [x] **Test integration gaps** — wired path has `engine_test.go` + `_e008_` suites (not audited), but the new state/validator concurrency and schema-completeness paths (F-ST-008, F-TV-009) lack coverage.
- [~] **Lifecycle & ordering** — **1 finding + 1 needs-investigation (X-05).** `StartCleanupTicker` returns a stop func — confirm the server calls it on shutdown (else ticker+goroutine leak); not read here.

## Refutations (verified, not silently dropped)

- **F-PL-001 / F-PL-002 (claimed S0 nil-deref):** REFUTED against source. `ApplyTurn` (`session.go:53-63`) always runs the closure; the closure always sets `resp = generated` (non-nil) before returning nil; therefore `ProcessRequest` never yields `(nil, nil)`. Downgraded to S3 defensive guards.
- **"Validator silently no-ops" (cross-file worry):** REFUTED. `JSONSchemaObject = map[string]any` + yaml.v3 string-keyed maps → assertions succeed at all depths.
- **F-AR-001 (claimed S1 tenant leak):** WITHDRAWN. Global agents are intentionally shared across tenants.
- **F-SM-002 (claimed S1 ReDoS):** WITHDRAWN to informational. RE2 is linear-time.
