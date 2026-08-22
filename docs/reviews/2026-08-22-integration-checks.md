# Cross-file Integration Checks: benchmark baseline and pipeline HTTP execution

Date: 2026-08-22

## Flow Checks

| Flow | Files Checked | Status | Evidence | Gap/Risk | Action |
|---|---|---|---|---|---|
| CI benchmark measurement -> JSON/Markdown -> guard | workflow, benchreport, latest files, benchguard, docs | Pass | Same package/platform metadata and 17 benchmark names; tolerance wording fixed. | External CI samples are not committed. | Retain artifact-based verification on refresh. |
| HTTP run -> registry -> executor -> engine -> ordered JSON | server routes, handler, pipeline registry/executor, SDK clients/tests | Pass | Server tests and TypeScript SDK tests pass; Python sources compile. | Python pytest unavailable locally. | Verify Python tests in CI. |
| Auth principal -> tenant context -> node lookup | auth middleware, tenant scope, executor lookup | Pass | `TenantIDFromContext` drives `GetForTenant` before every node. | Add a full multi-tenant HTTP test. | Track as P2 coverage. |
| Route -> role floor -> RBAC docs/snapshot | server, route table, snapshot, multi-tenant guide | Pass | Viewer floor is declared and documented. | None. | None. |
| Qdrant/Pinecone HTTP -> shared vector store -> ranked/partial response | config loader, startup seeding, adapter registry, provider handlers, vector core | Pass | Recursive fixtures, namespaces, CRUD/search/stats, post-ranking truncation, and explicit partial-response signaling pass. | Chroma profile remains. | Continue R11. |

## Contract Checks

| Contract | Producer | Consumer | Status | Notes |
|---|---|---|---|---|
| Benchmark schema v1 | benchreport | benchguard/latest docs | Pass | Platform changed intentionally to Linux/amd64. |
| Pipeline run request | HTTP handler | applications/SDKs | Pass | Typed Python and TypeScript clients map the wire response. |
| Ordered `nodes` trajectory | PipelineExecutor | HTTP response/TestSuite concept | Pass | Partial result is retained on 422. |
| Exact pipeline agent refs | PipelineDefinition | AgentRegistry | Pass | No model/default fallback is permitted. |
| Qdrant IDs/payload/scores | Qdrant adapter | Vector core | Pass | String/numeric IDs are preserved; payload filtering is type-sensitive; stable-ID ties are deterministic. |

## Integration Findings

- P1 completed: exact node-reference resolution prevents wrong-agent execution.
- P1 completed: Python/TypeScript typed clients and exact node-sequence assertions consume the contract.
- P1 in progress: R11 has a reusable core plus Qdrant/Pinecone profiles; Chroma and RAG-demo migration remain explicit.
