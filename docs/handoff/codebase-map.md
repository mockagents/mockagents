# Codebase and responsibility maps

The [tracked source inventory](reference/source-inventory.md) enumerates files, and the [generated dependency map](reference/dependencies.md) enumerates direct Go imports. This page explains responsibilities and change impact. An inventory entry is not a claim that every line has undergone a security audit.

## Component map

| Path | Responsibility / interfaces | Change partners |
| --- | --- | --- |
| [cmd/mockagents](../../cmd/mockagents/) | Cobra CLI, startup, fixture loading, listener/store wiring, shutdown, recording and test commands | Environment reference, examples, container entrypoint, CLI tests |
| [internal/config](../../internal/config/) | Kind dispatch, defaults, structural and cross-document validation, diagnostics | `types`, `schema`, editor validation, loaders |
| [internal/types](../../internal/types/) | Authoring domain: agents, behavior, tools, pipelines, suites, MCP/A2A/vector/search | Schemas, API, SDKs, examples, generated docs |
| [internal/engine](../../internal/engine/) | Registry, request normalization contract, matching, templates, tools, chaos, pipelines | Adapters, state, runner, request metadata |
| [internal/engine/state](../../internal/engine/state/) | Session turn transaction, bounded history, TTL and LRU memory store | Engine, isolation tests |
| [internal/adapter](../../internal/adapter/) | Provider HTTP routes, wire conversion, resource stores, provider errors | Streaming, engine, vector store, conformance and SDK tests |
| [internal/server](../../internal/server/) | HTTP middleware, management routes, authz floors, conditional mounts, edits, logs, probes | Tenancy, audit, quota, storage, GUI |
| [internal/tenancy](../../internal/tenancy/) | SQLite/Postgres Store, API keys, credential proofs, roles, SSO users/sessions, quotas/spend | Auth middleware, OIDC, mutation handlers, concurrency/security tests |
| [internal/audit](../../internal/audit/) | Audit events and persistence; principal extraction callback | Server mutation/denial hooks and retention |
| [internal/storage](../../internal/storage/) | SQLite interaction log, migrations, filters and pruning | Log worker, GUI log fields, cost aggregation |
| [internal/quota](../../internal/quota/) | Rate buckets, spend limits and optional shared spend backend | Tenancy store, middleware, Realtime wiring, pricing |
| [internal/pricing](../../internal/pricing/) | Model price table, usage extraction, estimated costs | Log capture, quota spend, cost API |
| [internal/oidcauth](../../internal/oidcauth/) | OIDC verification and configuration | Server auth handlers, tenancy sessions, GUI cookies |
| [internal/clientip](../../internal/clientip/) | Trusted proxy aware client IP | Auth failure limiter, audit |
| [internal/streaming](../../internal/streaming/) | Provider stream frames, pacing and truncation/malformed faults | Adapter usage/finish semantics and cancellation tests |
| [internal/realtime](../../internal/realtime/) | WebSocket event/session state, audio/VAD simulation | Realtime adapter, quota/log hooks, strict event tests |
| [internal/mcp](../../internal/mcp/) | JSON-RPC dispatch, tools/resources/prompts, HTTP/stdio, sessions and events | MCP types, tool schema, CLI listener, conformance |
| [internal/mcpadmin](../../internal/mcpadmin/) | Optional agent-management MCP tools | Main management write path; separate listener trust boundary |
| [internal/a2a](../../internal/a2a/) | Agent Card, tasks, message and streaming RPC | A2A fixtures, bounded retention, standalone CLI |
| [internal/vector](../../internal/vector/) | Shared in-memory collection/point/query semantics | Qdrant/Pinecone/Chroma adapters, fixture seeding |
| [internal/chaos](../../internal/chaos/) | Shared fault selection utilities | Engine, MCP/A2A, vector/search policies |
| [internal/toolschema](../../internal/toolschema/) | Supported JSON Schema and strict function-schema subsets | Engine and MCP input validation |
| [internal/recording](../../internal/recording/) | Upstream proxy, bounded capture, cassette append/load, matching, replay, import, redaction | CLI recording/replay/import, conversion and security tests |
| [internal/runner](../../internal/runner/) | Isolated suite cases, trajectory assertions, JUnit | Engine/pipeline semantics, CLI test |
| [internal/contract](../../internal/contract/) | Consumer-visible contract extraction and change classification | CLI contract and CI baselines |
| [internal/drift](../../internal/drift/) | Compare scrubbed SDK/provider/mock artifacts | CLI drift, baseline manifests, provider-drift workflow |
| [internal/conversion](../../internal/conversion/) | Fixture migration helpers | CLI convert, migration guides |
| [internal/cli](../../internal/cli/) | Starter templates and CLI support | Init commands and example validation |
| [internal/metrics](../../internal/metrics/), [observability](../../internal/observability/) | Prometheus and opt-in OTel | Middleware, engine spans, metrics auth, operations |
| [internal/build](../../internal/build/) | Build/version information | CLI, identity/health, release ldflags |
| [sdk](../../sdk/) | Go/Python/TypeScript clients, process managers, npx installer, Vitest/Jest helpers | Binary version coupling, contracts, installer checksums |
| [gui](../../gui/) | Server-rendered console with interactive editors, logs, pipelines, exports and tenant admin | Management API, identity/capabilities, ETags |
| [deploy](../../deploy/), [.github/workflows](../../.github/workflows/) | Helm, composite actions, CI/release/install verification | Dockerfile, package versions, exact-SHA candidate evidence |
| [schema](../../schema/), [site](../../site/), [docs](../) | Machine authoring constraints and human documentation | Drift/link/Liquid checks |
| [examples](../../examples/), [conformance](../../conformance/), [tools](../../tools/) | Fixtures, protocol compatibility evidence, maintenance checks | All public surfaces |

## Agent, tool, pipeline, and behavior maps

An `AgentDefinition` contains tools and ordered scenarios. A selected scenario yields an engine `Response`; adapters map it to their wire. Tools are resolved by exact name and argument rules inside `ToolCallProcessor`; MCP tools use their own JSON-RPC dispatch. A `PipelineAgent.ref` names an agent, and `PipelineEdge` transfers the predecessor's content. A2A messages interact with an A2A task store, not those pipeline nodes.

| Trigger | Owning path | Observable result |
| --- | --- | --- |
| Provider request | adapter → engine → stream/JSON serializer | Provider-shaped response or error |
| Agent create/edit | managed route → parser/validator → source file/registry | Persistence receipt, revision headers, audit event |
| Pipeline run | managed route → executor → scoped engine calls | Node trajectory, latency, partial result on failure |
| YAML suite | CLI → runner → engine/executor | Assertions, exit status and optional reports |
| Recording request | proxy → upstream → redaction/cassette | Real response plus reusable capture |
| Replay request | canonical request hash → per-key sequence | Recorded response or explicit miss |
| Console load | Next server component → typed API client → management API | Current identity, resources, unknown/unreachable states |

The integration map and client ownership are expanded in [integrations](integrations.md). Read [design trade-offs](design-tradeoffs.md) before replacing a boundary with a shared helper: apparent duplication can preserve different wire contracts.
