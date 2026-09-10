# Architecture, protocol, and multi-agent release audit

{% raw %}

## Scope and method

Reviewed local `main` at `6ddb03e54a14484e5929a19673f0cfd8a1975f07` on 2026-09-10. This is one persona report for the repository-wide release audit: architecture, engine/state, multi-agent pipelines, config/types, MCP/A2A, vector adapters, and selected streaming/realtime seams. Security of the main HTTP server, persistence/recording, CI/build, GUI, and SDKs are covered by the other review personas. No production source was changed.

Pass 1 inspected individual source and test files. Pass 2 traced provider requests through handlers into shared state, then back through response encoding; compared public guide promises with implementations and provider primary documentation; and added temporary executable regression probes. Passing existing tests is not treated as evidence of missing behavior being correct.

## Findings

| ID | Severity | Release gate | Evidence | Finding and impact |
|---|---|---|---|---|
| AR-01 | High | Block sustained/shared A2A release | `internal/a2a/server.go:144,269,284,333,351`; `cmd/mockagents/a2a.go:95` | A2A stores every task and complete input history indefinitely, with no TTL, count/byte budget, or deletion. Normal successful requests grow memory without bound. The CLI exposes the handler on all interfaces. |
| AR-02 | High | Block concurrent A2A release | `internal/a2a/server.go:377-382,392-402` | A2A responses expose mutable stored Task pointers after releasing the mutex. Concurrent cancel writes `Status.State`/`Timestamp` while another HTTP request marshals the same fields. |
| AR-03 | High | Block core engine release | `internal/engine/state/session.go:74-79,149-152`; `internal/engine/engine.go:278-327`; `internal/config/validator.go:187-238` | Failed generation commits a user message/turn but bypasses the retained-history limit. Repeated rendering failures grow one live session indefinitely and consume turn-based scenarios. |
| AR-04 | High | Block advertised Chroma RAG profile | `internal/adapter/chroma.go:158-163,203-246` | Chroma query always emits `documents:null` and `uris:null`, including when requested. Stored document retrieval works via get but fails via similarity query, breaking normal RAG document ingestion/retrieval workflows. |
| AR-05 | Medium | Fix before claiming metric-compatible Chroma | `internal/adapter/chroma.go:59-64,88,234,289` | Collection creation ignores requested metric/configuration and always chooses cosine; Euclidean seeded collections return the wrong distance conversion. Ranking can be wrong even when the create request succeeds. |
| AR-06 | Medium | Fix before claiming Euclid-compatible Qdrant | `internal/adapter/qdrant.go:191-203`; `internal/vector/store.go:418-420,477` | Qdrant Euclid returns normalized similarity rather than Euclidean distance and applies a minimum similarity threshold directly to a maximum-distance input. Legitimate neighbors disappear. |
| AR-07 | Medium | Fix before claiming multi-turn A2A task support | `internal/a2a/server.go:242,269,333` | Both send and stream decode `message.taskId` but ignore it and create a new task. An input-required/working task cannot be continued, and terminal/nonexistent task references are silently accepted. |
| AR-08 | Medium | Block invalid-success HTTP responses | `internal/vector/store.go:418,468,481-488`; `internal/adapter/encode.go:42-45` | Finite accepted vector values can overflow computed scores to Inf/NaN; ignored JSON encoding errors produce HTTP 200 with an empty body. This is not an explicitly configured chaos response. |

No Critical finding was established in this scope. High means a demonstrated core behavior failure, unsafe concurrent behavior, or unbounded state on a supported request path. Medium findings are specific compatibility/edge-case failures, not assertions that every upstream feature must be implemented.

## AR-01: bound A2A task retention

Every non-`as_message` `message/send` adds a unique task (`server.go:269-284`), and `message/stream` does the same (`333-354`). `tasks/get` and `tasks/cancel` do not remove anything; `Server` has no retention option or background cleanup. A 4 MiB per-request body limit at `readBounded` bounds one request only. Each task retains the parsed user's parts/history after the response completes. A short diagnostic sent 300 ordinary completed messages and observed exactly 300 retained tasks; the absence of any cleanup/cap was verified in the full A2A implementation. At 1 MiB retained input per task, 1,000 completed calls alone can retain roughly 1 GiB plus Go object overhead.

**Patch instructions.** Add `TaskStore` or equivalent bounded state in `internal/a2a`, with explicit maximum task count, retained bytes, terminal-task TTL, and per-task history cap. Configure these at `NewServer`/CLI construction. Inject a clock for deterministic expiry tests. Apply admission and pruning under the same mutex as insertion; expire completed tasks first and reject new work with a documented resource-limit error if all capacity is occupied by live tasks. Do not silently evict an active task. Reuse the admission function in send and stream rather than duplicating it.

```go
// Called under the task-store mutex, before storing either send/stream output.
s.pruneExpiredTasksLocked(s.now())
if len(s.tasks) >= s.maxTasks || s.retainedBytes+taskBytes > s.maxTaskBytes {
    return newError(req.ID, errInternal, "mock task capacity exceeded", nil)
}
s.tasks[task.ID] = task
s.taskExpiry[task.ID] = s.now().Add(s.taskTTL)
s.retainedBytes += taskBytes
```

The snippet shows the shared admission seam; define `taskBytes` from the retained representation (including user parts) and update byte accounting on every mutation/prune. Add CLI flags/environment mapping and document defaults/expiry semantics in `site/docs/guides/a2a.md`. Add retained-task/byte/eviction/admission-rejection metrics. Acceptance: sends and streams both stay inside a small test budget; expired IDs return task-not-found; live tasks are preserved; repeated large bodies reach a bounded memory plateau.

## AR-02: take A2A response snapshots while locked

`handleTasksGet` obtains `task := s.tasks[p.ID]`, unlocks, and returns that pointer. `HandleBytes` marshals only after dispatch returns. `handleTasksCancel` mutates that exact task in place. `handleMessageSend` also returns the inserted pointer, which can be canceled before its marshal completes. A deterministic probe fetched a response object in `working`, canceled the task, and then observed that the previously returned response object had become `canceled`. This establishes aliasing without needing a probabilistic race; the read/write overlap during concurrent marshaling follows directly from those paths. `Server` is explicitly documented as safe for concurrent use (`server.go:139-140`).

**Immediate patch.** Copy the task value before releasing the lock in get/send/cancel. All currently mutable fields are value fields in `Task.Status`, so the following snapshot closes the confirmed race without serializing JSON encoding under the lock:

```go
s.mu.Lock()
task, ok := s.tasks[p.ID]
if !ok {
    s.mu.Unlock()
    return newError(req.ID, errTaskNotFound, "task not found", nil)
}
snapshot := *task // includes a value copy of TaskStatus
s.mu.Unlock()
return newResult(req.ID, &snapshot)
```

For send/cancel, replace `return newResult(req.ID, task)` with a value snapshot under the existing deferred unlock. When AR-07 adds history/artifact mutation, expand this to a deep snapshot of message/part slices and JSON data, or use immutable copy-on-write task versions. Tests: retain a get response, cancel, assert retained response is unchanged; concurrently get/send/cancel and encode using `go test -race ./internal/a2a`. Document that returned internal responses are snapshots. Race instrumentation could not be executed here because the installed Go environment has `CGO_ENABLED=0` and no `gcc`; the sequential alias test was executed and failed as described.

## AR-03: commit conversation state only after successful generation

`ApplyTurn` appends/increments first, then executes `build`, and returns immediately on error. History trimming exists only inside `appendAssistantMessage`, which error paths skip. The store supplies a default 256-entry cap, but failed turns do not respect it. Session TTL does not mitigate a client retrying under the same ID because each failed append refreshes LastAccess.

An engine integration probe used an otherwise valid agent with response `{{index .Message 999}}`. `config.Validator.Validate` accepted it; requests with message `x` failed at template execution. After ten attempts with `MaxHistory=2`, the session contained ten messages. The lower-level probe also reported `TurnCount=10`. This is both an unbounded-retention bug and a retry correctness problem. The current validator checks content presence, not template execution semantics; no load-time static validation can eliminate every input-dependent template error.

**Patch instructions.** Refactor `ApplyTurn` so it builds against the proposed turn number and a recursively cloned JSON-compatible variable map, then commits both messages and variables only on success. Avoid copying nested variable maps shallowly, otherwise a failed build can still leak changes.

```go
s.mu.Lock()
defer s.mu.Unlock()
vars := cloneVariables(s.Variables) // recursively clone map[string]any / []any
content, calls, err := build(s.TurnCount+1, vars)
if err != nil {
    return err
}
s.Variables = vars
s.appendUserMessage(userContent)
s.appendAssistantMessage(content, calls) // enforces retained-history cap
return nil
```

Document that failed generation does not consume a turn and adjust `ApplyTurn`'s callback contract accordingly. Add tests for parse errors, runtime errors, nested variable mutation followed by error, successful retry selecting the same turn scenario, and 1,000 errors with a small history cap. Add syntactic template validation using the same function map as `ResponseGenerator` to reject authoring mistakes earlier; retain runtime rollback regardless. If product policy intentionally counts failures, implement bounded failure records separately and still enforce limits on every path.

## AR-04: honor Chroma query field selection

`write` stores documents under `__chroma_document` and URIs under `__chroma_uri`. `chromaRows` already retrieves these for get. `Query` builds IDs, distance, metadata, and embedding columns, then hard-codes document and URI columns to nil. The executed probe inserted `release evidence` and queried with `include:["documents"]`; it received:

```json
{"distances":[[0]],"documents":null,"embeddings":[[[1,0]]],"ids":[["a"]],"include":["documents"],"metadatas":[[{}]],"uris":null}
```

This additionally returns unrequested embeddings and metadata. [Chroma's query/get contract](https://docs.trychroma.com/docs/querying-collections/query-and-get) returns documents, metadata, and distances by default and respects explicit `include`. The repository guide advertises Chroma record query support (`site/docs/guides/vector-mock.md`, Chroma v2 compatibility), with no warning that document payloads are unavailable.

**Patch instructions.** Normalize omitted include to the provider defaults, build document/URI arrays from the matched point metadata, and use one aligned point snapshot per query. Extend the store query return type with optional retained vector payload if embeddings are requested, instead of performing per-ID fetches after releasing the query lock: concurrent deletes currently can also make embedding and ID columns unequal lengths.

```go
include := q.Include
if include == nil {
    include = []string{"documents", "metadatas", "distances"}
}
// For each match, while constructing that query's aligned columns:
docs = append(docs, match.Metadata["__chroma_document"])
uris = append(uris, match.Metadata["__chroma_uri"])
// Publish nested columns only for requested fields; ids are always returned.
out["documents"] = nil
if slices.Contains(include, "documents") {
    out["documents"] = documentRows
}
```

Apply the same selection to embeddings, metadata, distances, and URIs, preserving null entries for absent individual values and empty rows for no matches. Tests: explicit and omitted include, multiple query vectors, partial/no-match results, null documents, URI roundtrip, and a pinned Chroma SDK retrieval example returning the actual source document. Update the guide with the supported field/filter matrix and a document-bearing query example.

## AR-05: preserve Chroma metric configuration

`chromaCreate.Configuration` and `.Metadata` are decoded but ignored by create; the store is always initialized with `vector.Cosine`. A probe requested `configuration.hnsw.space:"l2"` and the response reported cosine. An omitted configuration also becomes cosine, whereas [Chroma's configuration contract](https://docs.trychroma.com/docs/collections/configure) defaults to squared L2 and supports `l2`, `cosine`, and `ip`. For startup-seeded Euclidean collections, `1-m.Score` converts the core's `1/(1+d)` to `d/(1+d)`, rather than squared L2. This is a numerical contract error on supported operations, not a missing optional upstream feature.

**Patch instructions.** Parse the supported metric config into a typed structure, honor legacy `metadata["hnsw:space"]` only if the supported SDK version sends it, reject unsupported metric values, and choose Euclidean by default for runtime Chroma-created collections. Preserve collection metadata/configuration and emit Chroma's wire names (`l2`, `ip`, `cosine`) in `collectionJSON`. Keep declarative shared-store fixtures' selected metric. Use metric-aware result conversion:

```go
switch space {
case "", "l2": metric = vector.Euclidean
case "cosine": metric = vector.Cosine
case "ip": metric = vector.Dot
default: return errors.New("unsupported Chroma distance space")
}
// Existing internal Euclidean similarity is 1/(1+d):
if metric == vector.Euclidean {
    distance := 1/m.Score - 1
    wireDistance = distance * distance
} else {
    wireDistance = 1 - m.Score
}
```

Prefer retaining raw metric values in the core (see AR-06) over inverting rounded similarity for the final implementation. Tests: default metric, each explicit metric, invalid metric, create/get metadata roundtrip, vectors whose cosine and L2 rankings differ, and known squared L2 25 for `[0,0]` versus `[3,4]`. Update the vector guide's metric names/defaults and compatibility matrix.

## AR-06: translate Qdrant Euclid scores and thresholds

The core intentionally ranks normalized similarities descending. Qdrant's adapter exposes that internal number as its public `score` and passes the user's threshold straight into `MinScore`. [Qdrant documents that Euclidean scores above the threshold are excluded](https://qdrant.tech/documentation/search/search/#filtering-results-by-score). The executed probe created Euclid, inserted `[3,4]`, queried `[0,0]` with threshold 6, and got an empty result. The expected distance is 5 and is within 6.

**Patch instructions.** Keep provider-neutral ranking separate from wire scores. For an immediate compatible adapter patch, inspect the collection metric, convert a nonnegative Euclidean threshold `t` to internal minimum similarity `1/(1+t)`, and invert returned Euclidean scores before serialization. Reject negative thresholds according to the supported provider contract. For numerical stability and clear contracts, add raw distance to `Match` and avoid inverse transforms in the longer-term core.

```go
minScore := req.ScoreThreshold
if cfg.Metric == vector.Euclidean && req.ScoreThreshold != nil {
    threshold := 1 / (1 + *req.ScoreThreshold)
    minScore = &threshold
}
// Build Query using minScore, then at the protocol boundary:
wireScore := match.Score
if cfg.Metric == vector.Euclidean {
    wireScore = 1/match.Score - 1
}
```

Tests: distances 0, 5, and 10; thresholds below/equal/above 5; descending similarity versus ascending distance order; cosine/dot unchanged; stable-ID ties. Correct the vector guide's blanket statement that all public results sort by descending score. Do not retrofit a normalized-score contract onto a provider compatibility claim without an explicit versioned limitation.

## AR-07: honor A2A task continuation

The request type contains `TaskID`, but the only runtime uses are new task ID allocation. A second message referring to `task-2` returned `task-6` in the probe. Both `message/send` and `message/stream` duplicate this logic. The [A2A v0.3.0 specification, sections 6.1 and 7.1/7.2](https://a2a-protocol.org/v0.3.0/specification/) defines continuation on an existing task and errors for restarting terminal tasks. This repository advertises protocolVersion 0.3.0 and explicitly accepts `input-required` states, so silently changing the task identity hides the workflow under test.

**Patch instructions.** Extract one locked transition function used by send and stream. If taskId is supplied, resolve the existing task, verify contextId consistency, reject missing/terminal tasks, append the user and matched agent messages to a new immutable task version, update status/artifacts, and preserve IDs. New IDs are allocated only if taskId is absent. Apply AR-01 budgets and AR-02 snapshots to transitions as well as insertion.

```go
if p.Message.TaskID != "" {
    prior, ok := s.tasks[p.Message.TaskID]
    if !ok { return newError(req.ID, errTaskNotFound, "task not found", nil) }
    if isTerminal(prior.Status.State) {
        return newError(req.ID, errInvalidParams, "terminal task cannot be continued", nil)
    }
    if p.Message.ContextID != "" && p.Message.ContextID != prior.ContextID {
        return newError(req.ID, errInvalidParams, "contextId does not match task", nil)
    }
    // Clone, append bounded history, transition status, and store same task ID.
}
```

Use the exact error contract for the pinned A2A version when completing this patch. Tests: input-required to completed, working continuation, send-to-stream and stream-to-send continuation, nonexistent task, terminal task, context mismatch, and get/history consistency. Add a complete multi-turn example and the supported lifecycle matrix to the A2A guide. If continuation is intentionally unsupported for this release, reject taskId explicitly and document that limitation instead of reporting a new successful task.

## AR-08: reject non-finite computed scores and handle encoder errors

Both upsert and query reject explicit NaN/Inf inputs, but multiplication of finite numbers can overflow. With a one-dimensional Dot collection containing `1e308`, querying `1e308` produces +Inf. `json.Encoder.Encode` returns an error, but `writeJSON` ignores it and commits the caller's 200 status with an empty buffer. The HTTP probe confirmed status 200 and a zero-length body. Cosine also risks overflow in squared norms and NaN division. This bypasses deliberate chaos metadata and corrupts the API contract on otherwise accepted JSON.

**Patch instructions.** Enforce a documented vector numeric domain or use stable scaled arithmetic (scaled cosine norms; `math.Hypot` for Euclidean distance) and validate every computed score before adding it to candidates. Return a provider-shaped input/numerical error if a result cannot be represented. Independently, change the shared encoder so errors never become successful empty responses:

```go
if math.IsNaN(score) || math.IsInf(score, 0) {
    return QueryResult{}, errors.New("vector score is not finite")
}
```

```go
if err := re.enc.Encode(v); err != nil {
    re.buf.Reset()
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusInternalServerError)
    _, _ = w.Write([]byte(`{"error":{"message":"response encoding failed"}}`))
    return
}
```

Prefer making `writeJSON` return an error and letting each protocol select its own envelope; if retaining the shared fallback, explicitly document the common internal-failure envelope and log only the encoding error, without payload contents. Tests: large finite Dot/Cosine vectors, zero vectors, representable edge values, NaN/Inf in authored metadata, and a direct `writeJSON` unencodable value asserting non-2xx and valid JSON. Document accepted vector numeric range.

## Pass 1 inventory and existing safeguards

The following is the concrete manual inspection inventory for this persona; it does not claim that every file in these directories received equal line-by-line review. Existing sibling tests were inventoried and the entire scoped package suite was run. Root report combines this with the other personas.

| Files inspected | Per-file observations and checks |
|---|---|
| `internal/engine/engine.go`, `response_generator.go`, `strict.go`, `tool_processor.go`, `agent_registry.go`, `pipeline.go`, `state/session.go`, `state/store.go` | Agent resolution/tenant namespacing; fallback policy; generation errors; response aliasing; strict-tool overrides; sequential/parallel/graph traversal; session locking, expiry, retention. AR-03 confirmed. No additional proven registry isolation bypass. |
| `internal/engine/engine_test.go`, `pipeline_test.go`, `state/store_test.go` | Read helper setups and core/multi-root/cycle/substring/store tests. Existing pipeline fixes prevent cycles, cover all roots, preserve parallel partial results, and use explicit tenant-visible refs. Missing error-history regression reproduced. |
| `internal/config/loader.go`, `defaults.go`, `validator.go`, `pipeline_validator.go`, `schema_parity_test.go`; `internal/types/agent.go`, `pipeline.go` | Loader document discrimination; pointer-valued explicit zero delays; chaos and protocol validation; duplicate graph IDs/edges/cycles; schema parity covers selected enum/bounds rather than all public behavior. Template execution failures still possible after validation. |
| `internal/a2a/server.go`, `server_test.go`; `cmd/mockagents/a2a.go`; `site/docs/guides/a2a.md` | Entire task storage/dispatch/HTTP/lifecycle implementation and primary tests; JSON-RPC notification asymmetry and omitted required-field validation noted as follow-up; task state, retention, concurrency and continuation defects confirmed above. |
| `internal/mcp/server.go`, `streamable.go`, `bidirectional.go`, `tool_handler.go` | Stream sessions have 256-session count limit, 30-minute idle TTL, bounded replay, origin guard, and capped subscriptions. Server-scoped logging/subscriptions/notifications are shared across HTTP sessions. Programmatic tool context is explicitly background-only by current documented contract. These deserve scoped reliability work but are not claimed as new release blockers without a workload/contract reproduction. |
| `internal/vector/store.go`; `internal/adapter/chroma.go`, `qdrant.go`, `pinecone.go`, `encode.go`; `chroma_test.go`, `qdrant_test.go`; `site/docs/guides/vector-mock.md` | Whole vector core plus provider CRUD/query/metadata conversions and JSON encoding. Atomic upsert validation, tenant keys, deterministic ties and per-collection point cap already exist. AR-04/05/06/08 confirmed. Pending dimension inference and aggregate memory budgets need further boundary coverage. |
| `internal/realtime/session.go`, `internal/adapter/realtime.go` (selected config/state/concurrency sections), `internal/streaming/anthropic.go`, `gemini.go` | Realtime single-owner event-loop design; configuration union; stream usage/finish emission and cancellation checks. Existing tests were executed. This was seam-focused inspection, not a claim of exhaustive upstream protocol conformance. |
| `docs/api-spec.yaml`, `README.md`, relevant `ARCHITECTURE.md` and `docs/ADOPTION_REQUIREMENTS.md` sections; schema file inventory | OpenAPI lists Chat/Anthropic and deliberately excludes MCP; newer vector/provider routes lack OpenAPI coverage. Public provider endpoint authentication exemptions were confirmed by the main reviewer and are intentional, not a finding in this report. Schema files exist for Agent/Pipeline/TestSuite/MCPServer/VectorCollection, not A2AServer/SearchService. |

## Pass 2 integration conclusions

1. **Validated configuration → engine → session store:** a syntactically valid, validator-accepted template can fail on live input; the per-session lock prevents interleaving, but not partial commit or retention bypass (AR-03).
2. **A2A HTTP → dispatch → task map → JSON marshal:** store lock ends before serialization, while cancel mutates shared response memory (AR-02); successful task creation retains input forever (AR-01).
3. **A2A input-required → client follow-up → task lookup:** taskId is parsed but never looked up on send/stream, so the task lifecycle branches into a new task (AR-07).
4. **Chroma add → shared metadata → query → RAG:** documents are retained internally and available via get, yet discarded on query (AR-04). Metric configuration is discarded at collection creation and raw metric semantics are lost at output (AR-05/06).
5. **Valid JSON vector → arithmetic → common encoder:** input finiteness does not ensure result finiteness; a math overflow becomes a false HTTP success because the common encoder discards its error (AR-08).
6. **Multi-agent graph semantics:** graph execution is depth-first and first-visit wins (`pipeline.go:259-288`). A merge node can execute before another parent and consumes only the first predecessor's text. No documented merge operator or explicit fan-in contract was located. Treat this as a design gap: document routing-only semantics and reject fan-in if unsupported, or implement topological execution with explicit aggregation. Do not present this as a confirmed violation of a currently specified aggregation guarantee.

## Validation and retained reproductions

Go version: `go1.26.4 windows/amd64`. The default Go cache had ACL-denied entries; the same tests succeeded in compiling with `GOCACHE=$PWD/.gotmp/go-build` and `GOTMPDIR=$PWD/.gotmp`. These are environmental workarounds, not repository code changes.

Temporary test sources are retained as inert `.go.txt` fixtures in `docs/reviews/2026-09-10-architecture-repro/`. Restore each into its original package as `release_audit_test.go` to run the probes. They assert the desired corrected behavior and therefore intentionally fail against the audited SHA, except the retention observation test which logs current size. Temporary copies were removed from production package directories after execution.

| Command/probe | Result |
|---|---|
| `go test ./internal/a2a ./internal/adapter ./internal/engine/state -run TestReleaseAudit -count=1 -v` | Six adapter/A2A correctness regressions plus failed-turn bound reproduced; task-retention probe logged 300 retained tasks. |
| `go test ./internal/engine -run TestReleaseAudit -count=1 -v` | Validator-accepted runtime template error retained 10 messages despite cap 2. |
| `go test ./internal/engine/... ./internal/config ./internal/adapter ./internal/mcp ./internal/a2a ./internal/realtime ./internal/streaming ./internal/vector -count=1` | Passed all nine packages after removing temporary package files. MCP took 61.189s; other packages completed in under three seconds each. |
| `go test -race ./internal/a2a` | Not executed: host has CGO disabled and no C compiler. Required on Linux CI as release evidence. |

## Implementation sequence and release acceptance

### Phase 1 — immediate gates

1. **Engine owner:** AR-03 atomic success commit and failure tests. Update session error semantics and template authoring diagnostics.
2. **A2A owner:** AR-01 retention/admission plus AR-02 snapshots in one coherent state-store patch. Require concurrent race tests and bounded-retention tests before merging. Add AR-07 shared continuation logic if multi-turn support remains in the release promise.
3. **Vector adapter owner:** AR-04 query field selection/payload retention. Confirm through a pinned Chroma SDK RAG test that actual document text is returned.
4. **Vector core/API owner:** AR-08 finite computed scores plus reliable JSON failure handling. Sweep all callers of the common encoder, including non-vector routes, for provider-envelope compatibility.
5. **Vector adapter owner:** AR-05/06 exact provider metric/default/threshold mapping with numeric fixtures and corrected guide text. Medium severity does not exempt known incompatibilities from a release advertised as covering those operations.

### Phase 2 — architecture and reliability

- Make task/session state updates transactional and response snapshots immutable; add explicit mutation/admission APIs rather than sharing live map values.
- Specify graph routing/fan-in/failure behavior. Add diamond DAG, canceled parallel node, branch-skipped node, and input aggregation tests against that contract; keep current cycle/multi-root guarantees.
- Pin provider compatibility profiles by supported SDK/protocol version. Classify every request option as supported, rejected, or explicitly ignored; reject behavior-changing unsupported options rather than silently broadening queries.
- Carry raw vector metric measurements separately from ranking keys so adapters can express cosine similarity, inner-product distance, Euclidean distance, and squared L2 without lossy inversion.
- Add aggregate memory budgets (bytes, not just object counts), per-tenant state budgets, and metrics for admission failures, live tasks, session errors, encoder failures, and evictions. Distinguish configured chaos from unexpected internal failure in all telemetry.
- Improve MCP request cancellation and session-local protocol state as a separate scoped design change; current transport context loss and global pending queues should not grow accidentally into a multi-client contract.
- Expand OpenAPI/schema inventory to all explicitly supported surfaces or link versioned upstream schemas from a maintained compatibility manifest. Add A2AServer/SearchService editor schemas and parity checks if they are part of the public configuration contract.

### Phase 3 — maintainability and release standards

- Add a root `AGENTS.md` with repository map, package ownership, high-risk boundaries, release branch/clean-worktree expectations, and exact verification commands. Link to this report without copying stale prior-audit claims into new code comments.
- Require two review passes in changes affecting protocols/state: per-file correctness followed by cross-file control/data-flow checks, with test evidence and a supported-provider matrix update.
- Add Linux CI race tests for A2A, engine/state, vector and registry packages; keep numerical and real-SDK contract tests independent of provider API keys or network access.
- Retain failed-turn, task lifecycle, document-query, numerical overflow, threshold, and metric tests as permanent regressions. Avoid testing only the implementation's current output shapes: use provider primary definitions and known numeric fixtures.
- Add release artifacts documenting known unsupported behavior and run validation on the exact release SHA. Existing historical audit comments are evidence of past fixes, not waivers for new changes.

**Scope verdict: Not ready.** Core error-path retention, A2A concurrent state handling/retention, and advertised Chroma document retrieval have reproducible defects. Release requires the High gates above to pass plus either fixes or explicit rejected/limited contracts for the Medium compatibility failures; passing the current broad test suite alone does not establish readiness.

{% endraw %}

