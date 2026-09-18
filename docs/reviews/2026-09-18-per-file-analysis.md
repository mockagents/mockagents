# Per-file analysis: handoff contract sample

Date: 2026-09-18. Baseline: `5d5b5172dfd3c19854bcfb1db7d5215af711165e`.

The table records files inspected for this documentation task. “Documented” means the stated contract was checked in source, not that every branch was exhaustively audited. Tagged types/imports/routes in other files were inventoried mechanically; the [inventory](../handoff/reference/source-inventory.md) does not imply manual review. Tests were executed repository-wide; selected test definitions were read to clarify contracts.

| File | Responsibility | Local status | Observation / cross-file lead | Test/validation / action |
| --- | --- | --- | --- | --- |
| [agent.go](../../internal/types/agent.go) | Agent/metadata/spec | Stale comment | Header-based tenant description differs from middleware authority | D-06; document actual context scope |
| [behavior.go](../../internal/types/behavior.go) | Scenario/fault/stream/strict definitions | Documented | Pointer omission semantics, raw arguments, labels and finish overrides | Dictionary + validation cross-check |
| [tool.go](../../internal/types/tool.go) | Tool schema/rules | Documented | Fixture responses/errors; no executable tool interface | Trace processor and MCP separately |
| [pipeline.go types](../../internal/types/pipeline.go) | Node/edge topology | Documented | Case-sensitive substring contract | Trace graph executor |
| [testsuite.go](../../internal/types/testsuite.go) | Assertions/cases | Documented | Trajectory vs final-result assertions | Trace runner aggregation |
| [mcp.go types](../../internal/types/mcp.go) | MCP config/content | Documented | Custom resource JSON nesting | Preserve wire distinction |
| [a2a.go types](../../internal/types/a2a.go) | Card/responses/faults | Documented | Runtime card defaults, Task/Message distinction | Schema absent: D-04 |
| [vector.go](../../internal/types/vector.go) | Startup collection/points | Documented | Shared profiles and partial results | Schema/store limits |
| [search.go](../../internal/types/search.go) | Search config | Documented | Runtime-only global fault defaults excluded from JSON/YAML | Schema absent: D-04 |
| [loader.go](../../internal/config/loader.go) | Kind dispatch and source nodes | Documented | JSON/YAML loading and typed buckets | Config tests pass |
| [defaults.go](../../internal/config/defaults.go) | Agent defaults | Documented | Explicit zero delay retained | Config defaults tests pass |
| [validator.go](../../internal/config/validator.go) | Agent validation | Documented | Five protocol enum values; name/rule/tool constraints | Avoid treating all adapters as Agent protocols |
| [pipeline_validator.go](../../internal/config/pipeline_validator.go) | Graph validation | Documented | Invalid refs/self-edges/duplicates/topology rejected | Executor cycle guard differs for raw programmatic input |
| [engine.go](../../internal/engine/engine.go) | Request execution | Documented | Scoped resolution, disposable session, tool-loop convergence, best-effort tool results | Engine tests pass |
| [scenario_matcher.go](../../internal/engine/scenario_matcher.go) | Ordered matching | Documented | AND conditions; first explicit match; first default | Matcher tests pass |
| [response_generator.go](../../internal/engine/response_generator.go) | Templates/response | Documented | Content-only rendering, random/time functions, unbounded distinct template cache | Explain determinism limits |
| [tool_processor.go](../../internal/engine/tool_processor.go) | Tool result simulation | Documented | Inline <=2 calls, ordered concurrent larger lists, error results | Tool tests pass |
| [chaos.go](../../internal/engine/chaos.go) | Agent fault decisions | Documented | Global inherited rate does not erase explicit rates/fail-first | D-03 doc correction |
| [pipeline.go executor](../../internal/engine/pipeline.go) | Coordination | Documented | Sequential/parallel/DFS, first-visited graph target, errors.Join | Pipeline tests pass; document no fan-in |
| [pipeline_registry.go](../../internal/engine/pipeline_registry.go) | Global pipeline registry | Documented | Name-keyed with source tracking | Justifies platform edit floor |
| [state/session.go](../../internal/engine/state/session.go) | Turn transaction | Documented | Commit only on successful build, non-reentrant lock | Session tests pass |
| [state/store.go](../../internal/engine/state/store.go) | TTL/LRU sessions | Documented | Shared pointer, bounds and eviction | No durable/distributed promise |
| [runner.go](../../internal/runner/runner.go) | Suite execution | Documented | User steps, per-case namespaces, cleanup and trajectory scope | Runner tests pass |
| [registry.go](../../internal/adapter/registry.go) | Adapter composition | Documented | Shared Files/Batches and Responses/Conversations stores; vector/search mounts | Full catalog extraction |
| [openai.go](../../internal/adapter/openai.go) | Chat DTO/handler | Documented | Model required at adapter before engine fallback | Cross-provider tests pass |
| [cohere_rerank.go](../../internal/adapter/cohere_rerank.go) | Rerank mock | Documented | Bounded string documents, token-overlap scoring | No semantic model claim |
| [tavily_search.go](../../internal/adapter/tavily_search.go) | Search fixtures/filter | Documented | Bound/default results, domains/dates; decoded options not all simulated | Explain field acceptance vs fidelity |
| [chroma.go](../../internal/adapter/chroma.go) | Composed route patterns | Documented | Local const path concatenation | Generator resolves composed routes |
| [server.go](../../internal/server/server.go) | Middleware/mount/readiness | Documented | Conditional stores/routes; runtime mounted capability set | Server tests pass |
| [route_authz.go](../../internal/server/route_authz.go) | Management floors | Documented | Required floor on every managed mount | Coverage test executes in suite |
| [open_routes.go](../../internal/server/open_routes.go) | Auth exemption | Documented | Derived adapter paths; probes/SSO explicit | Correct OpenAPI introduction |
| [middleware.go](../../internal/server/middleware.go) | Request context/tenant | Documented | Principal tenant copied into engine context | Ignore client-selected tenant authority |
| [identity_handlers.go](../../internal/server/identity_handlers.go) | Capability response | Documented | Mode and actual mounted routes/role | GUI contract trace |
| [agent_write_handlers.go](../../internal/server/agent_write_handlers.go) | Agent mutation/persistence | Documented | Ownership, 1 MiB, persist/register ordering, receipt | Failure-path tests pass |
| [agent_revision.go](../../internal/server/agent_revision.go) | Preconditions | Documented | Source+effective ETag; optional legacy unconditional path | GUI must echo ETag |
| [pipeline_handlers.go](../../internal/server/pipeline_handlers.go) | Run/edit API | Documented | 422 partial results; required If-Match on update | Documentation supplements missing OpenAPI run route |
| [validate_handler.go](../../internal/server/validate_handler.go) | Validation API | Documented | Raw YAML/JSON or yaml wrapper; diagnostics returned with HTTP 200 | API caveat added |
| [log_handlers.go](../../internal/server/log_handlers.go) | Capture/query/projection | Coverage gap | Shared narrow path classifier; `fields=meta` strips bodies after cost annotation | D-02; no universal capture claim |
| [quota_middleware.go](../../internal/server/quota_middleware.go) | Rate/spend path policy | Coverage gap | Omits mounted Ollama/Bedrock | D-02 follow-up |
| [tenancy/middleware.go](../../internal/tenancy/middleware.go) | Credentials/role gates | Documented | Managed fail-closed; exempt best-effort principal | Auth tests pass |
| [tenancy/types.go](../../internal/tenancy/types.go) | Principals/credential policy | Stale introduction | Actual types/store actor support richer scope than historical package intro | D-06 follow-up |
| [tenancy/store.go](../../internal/tenancy/store.go) | SQLite tenancy | Declaration review | FK/cascade/key/session/quota/spend schema | Go suite; live Postgres not run |
| [quota.go](../../internal/quota/quota.go) | Enforcer/backend | Stale introduction | Shared backend and 5-second spend cache exist | D-06; distinguish rate vs spend |
| [storage/models.go](../../internal/storage/models.go) | Interaction DTO/filter | Documented | Source/truncated/chaos fields; session prefix | GUI contract trace |
| [storage/sqlite.go](../../internal/storage/sqlite.go) | SQL/migrations | Declaration review | Local WAL store, additive columns/indexes | Storage suite pass |
| [recording/cassette.go](../../internal/recording/cassette.go) | Capture data/persistence | Documented | Encodings, serialized append, torn-tail handling, hash index | Recording suite pass |
| [recording/proxy.go](../../internal/recording/proxy.go) | Upstream capture | Documented | Original-body hash before redaction; error signaling | Real upstream not contacted |
| [recording/replay.go](../../internal/recording/replay.go) | Replay match/cursor | Documented | Strict misses, per-key sequence/repeat-last, relaxed index | Explain replay scope |
| [mcp/server.go](../../internal/mcp/server.go) | RPC dispatch | Contract review | Tool/resource/prompt/completion vs client-directed sampling/roots | MCP suite pass |
| [a2a/server.go](../../internal/a2a/server.go) | Task/card/RPC | Contract review | Message/Task, terminal states, task/byte/TTL limits | A2A suite pass |
| [vector/store.go](../../internal/vector/store.go) | Collections/query | Contract review | Numeric scoring, point/top-k/dimension bounds | Vector suite pass |
| [start.go](../../cmd/mockagents/start.go) | Composition/defaults | Documented | Valid agent required, global chaos policy and state stores | CLI tests, build, validation pass |
| [mcp.go CLI](../../cmd/mockagents/mcp.go) | MCP listener | Documented | Loopback default and separate unauthenticated management opt-in | No inherited server auth claim |
| [a2a.go CLI](../../cmd/mockagents/a2a.go) | A2A listener | Mount review | Separate discovery/RPC/health paths | Catalog includes listener context |
| [replay.go CLI](../../cmd/mockagents/replay.go) | Record-mode validation | Documented | Strict rejects upstream/key/record modes | Offline boundary documented |
| [Go inprocess.go](../../sdk/go/mockagents/inprocess.go) | SDK test server | Documented | Only chat/models/messages/health mounted | Explicit subset, not full server |
| [Python server.py](../../sdk/python/mockagents/server.py) | Subprocess manager | Interface review | Health polling, context-manager cleanup, opt-in download | Python tests not run |
| [TypeScript server.ts](../../sdk/typescript/src/server.ts) | Subprocess manager | Interface review | Port/process/client lifecycle | JS package tests not run |
| [gui/lib/api.ts](../../gui/lib/api.ts) | Typed backend client | Contract review | Credential forwarding, bounded fetch, source/revision fields | Runtime GUI checks not run |
| [gui/lib/auth.ts](../../gui/lib/auth.ts) | Cookie/identity lifecycle | Contract review | HttpOnly and actual identity capabilities | No cookie-inferred role promise |
| [gui/package.json](../../gui/package.json) | Tooling versions | Documented | Next 16 / React 19 | Correct overview version |
| [Dockerfile](../../Dockerfile) | Build/runtime image | Documented | No-cgo binary, non-root /data, network bind | Container not built |
| [docker-compose.yml](../../docker-compose.yml) | Local container | Documented | Read-only fixtures, persistent data | No persistent editor promise |
| [Helm values](../../deploy/helm/mockagents/values.yaml) | Topology defaults | Documented | Replica acknowledgment, local state, probes, shutdown budget | Helm not run |
| [verify.yml](../../.github/workflows/verify.yml) | Candidate gate | Contract review | SDK/GUI/container/security/Helm checks exceed local docs gate | Exact-candidate caveat |
| [go.mod](../../go.mod), [Makefile](../../Makefile) | Toolchain/dependencies/checks | Documented | Pin and actual test target coverage | No old Go/Node assumptions |

## Notes

Source comment claims were not accepted without tracing relevant implementation. No runtime code fixes were made. Further full defect review should prioritize D-02 behavior and high-churn cache/persistence assumptions with focused reproductions; do not report hypothetical defects as proven failures.
