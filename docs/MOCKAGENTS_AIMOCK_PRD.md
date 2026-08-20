# MockAgents: Deterministic AI Stack Testing PRD

**Status:** Proposed for product and engineering review  
**Date:** 2026-08-16  
**Owner:** MockAgents product/maintainers

## 1. Executive summary

MockAgents already provides a strong deterministic mock for model-facing traffic: a pure-Go server, OpenAI/Anthropic/Gemini-compatible APIs, tool calls, structured outputs, streaming, MCP, A2A, pipelines, chaos, record/replay, contract extraction, SDK helpers, observability, and CI packaging.

The current AIMock product boundary is broader: one configuration and one local server for LLMs, MCP tools, A2A peers, vector retrieval, search, reranking, moderation, provider drift detection, record/replay, and chaos. This PRD treats MockAgents as the foundation and defines the missing cross-service capabilities plus parity hardening. It does not replace working protocols or change the Go/no-runtime/Apache-2.0 positioning.

The first release should make RAG and external-service dependencies as deterministic as the existing LLM path. The second should make provider/API drift visible before production. The third should unify configuration, routing, diagnostics, and operational controls across every mocked protocol.

## 2. Research basis

The supplied CopilotKit URL is represented by the current canonical article, [“AIMock: One Mock Server For Your Entire AI Stack”](https://www.copilotkit.ai/blog/aimock-one-tool-to-mock-your-entire-ai-stack).

The repository baseline is the default branch of [mockagents/mockagents](https://github.com/mockagents/mockagents), primarily its [README](https://github.com/mockagents/mockagents/blob/main/README.md) and [architecture](https://github.com/mockagents/mockagents/blob/main/ARCHITECTURE.md). The comparison is a product/requirements review; it is not a claim that every README feature was independently reimplemented in this workspace.

## 3. Product problem

An agent test can be nondeterministic even when the LLM response is fixed. A realistic request may invoke an MCP tool, query a vector index, rerank documents, search the web, call moderation, and delegate to another agent. If only the model is mocked, tests still depend on mutable indexes, provider schema changes, external availability, data leakage risks, and unreproducible fault paths.

MockAgents should make the entire dependency graph reproducible, inspectable, safe to run offline, and easy to exercise from CI.

## 4. Goals and non-goals

### Goals

1. One local process and one versioned configuration for the complete agentic stack.
2. Deterministic Vector, Search, Rerank, and provider-drift capabilities without weakening existing wire compatibility.
3. Real protocol behavior: HTTP, SSE, WebSocket, JSON-RPC, sessions, streaming, status codes, and error shapes.
4. Safe record/replay: no credentials, bounded payloads, configurable redaction, and explicit no-live-egress mode.
5. First-class failure data: latency, rate limits, malformed payloads, disconnects, truncation, and connector failures.
6. One CI command for validation, scenarios, JUnit/JSON output, contract diffs, and drift gates.
7. Dependency-light, cross-platform, tenant-aware, observable operation for any supported language.

### Non-goals

- Evaluating prompt quality or model intelligence.
- Building a production vector database, crawler, reranker model, or moderation model.
- Storing unrestricted production data or making live-provider calls implicit.
- Replacing general-purpose HTTP tools such as WireMock.
- Guaranteeing byte-identical timing across operating systems; timing uses declared tolerances.

## 5. Current-state comparison

| AIMock capability | MockAgents today | Decision |
|---|---|---|
| One server/config for the whole stack | Strong single Go server and YAML documents, but MCP/A2A are separate command/listener surfaces and there is no vector/search/rerank bundle | Build unified `MockStack`, mounts, lifecycle, and one journal |
| LLM mocking | OpenAI, Anthropic, Gemini, Azure/Responses/Conversations, structured output, tools, SSE, batches, realtime, embeddings, moderation | Preserve; add profiles and compatibility coverage |
| MCP | Streamable HTTP, stdio, sessions, resources, prompts, bidirectional events, conformance tests | Preserve; mount into unified stack |
| A2A | Agent cards, JSON-RPC, task lifecycle, SSE streaming | Preserve; mount into unified stack |
| Vector database | No VectorMock package or Pinecone/Qdrant/Chroma-compatible service was found in the repository tree | Build P0 deterministic VectorMock |
| Search/rerank | Moderation exists; no first-class Tavily/Cohere-style search/rerank adapters were found | Build P0 service mocks |
| Drift detection | `tools/driftcheck` checks repository API references/licenses, not provider response drift | Build P1 provider triangulation |
| Record/replay | Cassettes, SSE replay, redaction, modes, match-ignore, diagnostics, importers | Harden for whole-stack capture, strict no-egress, and safe imports |
| Chaos | Per-agent latency/errors/rate limits and stream faults | Extend to every connector and scope |
| Embeddings/WebSockets | Deterministic embeddings and OpenAI Realtime exist | Integrate embeddings; add demand-driven WebSocket parity |
| Observability/CI | Interaction log, audit, OTEL, cost/quota, Go binary, Docker, Helm, SDKs, GitHub/GitLab helpers | Unify journal/metrics and add stack/drift workflows |

## 6. Personas and use cases

- **Agent developer:** test tool routing, retries, multi-turn state, and guardrails without application changes.
- **RAG developer:** control retrieval contents, scores, metadata, filters, empty results, and errors.
- **Platform/DevEx engineer:** operate one CI service with health checks, quotas, isolation, and logs.
- **QA/evaluation engineer:** define golden fixtures, chaos matrices, contract diffs, and JUnit output.
- **Provider maintainer:** detect SDK/provider shape changes before production.
- **Security reviewer:** prove secrets do not enter cassettes, logs, fixtures, or remote providers.

Representative flows:

1. Point an existing SDK base URL at MockAgents and exercise LLM → MCP → vector → search/rerank → moderation → A2A.
2. Record a happy path once, replay it offline, then inject a deterministic disconnect on a selected call.
3. Run nightly drift checks against a scrubbed corpus and fail CI on critical response-shape changes.
4. Inspect a single correlation-linked journal trace for every protocol call.

## 7. Functional requirements

Priority: **P0** first integrated release; **P1** broad adoption; **P2** follow-on.

### A. Unified stack

- **FR-A01 (P0):** Add versioned `kind: MockStack` containing LLM agents, MCP/A2A servers, vectors, services, record/replay, chaos, observability, and security policies; retain standalone kinds.
- **FR-A02 (P0):** Add `mockagents stack start|validate|test|logs|stop`, readiness checks, free-port allocation, reset, and base-URL export.
- **FR-A03 (P0):** Support one-port mounts (`/v1`, `/mcp`, `/a2a`, `/vector`, `/services`) plus optional multi-port compatibility mode.
- **FR-A04 (P0):** Validate cross-references, duplicate routes, collection dimensions, fixture schemas, limits, and policy contradictions before start; preserve last-known-good config on reload errors.
- **FR-A05 (P1):** Add named compatibility profiles for OpenAI, Anthropic, Gemini, Pinecone, Qdrant, Chroma, Tavily, Cohere, and OpenAI-compatible providers.

### B. Fixture and scenario engine

- **FR-B01 (P0):** Define provider-neutral fixtures for text/multimodal input, tools, structured output, usage, refusals, reasoning, streams, and errors.
- **FR-B02 (P0):** Match by service, operation, provider/model, content/regex, headers, JSON paths, turn/sequence, tenant, and metadata; explain misses with bounded diffs.
- **FR-B03 (P0):** Support sequence responses, multi-turn state, scoped counters, parallel-test isolation, reset, and deterministic seeds.
- **FR-B04 (P1):** Assert MCP calls, A2A tasks, vector queries, search/rerank calls, selected scenario, order, arguments, and retry timelines.

### C. LLM parity

- **FR-C01 (P0):** Preserve current OpenAI/Anthropic/Gemini, Azure, Responses, Conversations, batches, structured output, moderation, files, SSE, tools, and Realtime behavior.
- **FR-C02 (P1):** Add demand-driven Bedrock, Vertex, Ollama, Cohere, OpenRouter, and Anthropic Azure profiles/adapters through the common engine.
- **FR-C03 (P1):** Standardize TTFT, inter-token latency, jitter, chunking, truncation, malformed frames, and close behavior across SSE/WebSocket.
- **FR-C04 (P2):** Add Responses-over-WebSocket and Gemini Live after demand is demonstrated.

### D. MCP and A2A integration

- **FR-D01 (P0):** Mount current MCP/A2A definitions into `MockStack` with shared lifecycle, tenant policy, correlation IDs, journal, chaos, and reload.
- **FR-D02 (P0):** Preserve MCP Streamable HTTP, stdio, sessions, resources, prompts, subscriptions, bidirectional calls, and conformance behavior.
- **FR-D03 (P1):** Preserve A2A card discovery, message/send, message/stream, tasks/get/cancel, and deterministic long-running events; add unavailable/rejected/delayed/duplicate scenarios.

### E. VectorMock

- **FR-E01 (P0):** Implement Pinecone, Qdrant, and Chroma-compatible collection/index, metadata, upsert, fetch/get, query/search, delete, and health routes.
- **FR-E02 (P0):** Declare dimensions, metric, namespaces, vectors, metadata, and expected results; sort ties deterministically by stable ID.
- **FR-E03 (P0):** Support static results and deterministic handlers based on query, top-k, namespace, and filters; forbid network access and undeclared mutation.
- **FR-E04 (P0):** Model empty results, missing collection, dimension mismatch, bad filters, duplicate/deleted IDs, top-k overrun, timeout, rate limit, and partial results.
- **FR-E05 (P1):** Reuse the existing deterministic embeddings implementation with explicit dimension/normalization/version metadata.

### F. Search, rerank, moderation

- **FR-F01 (P0):** Add Tavily-compatible search with substring/regex fixtures, ordered results, score/domain/date fields, pagination, and safe catch-all behavior.
- **FR-F02 (P0):** Add Cohere-compatible rerank with query/document matching, scores, top-n, metadata, and deterministic empty/error cases.
- **FR-F03 (P0):** Expose existing moderation through `MockStack` and common fixtures/chaos/journal controls.
- **FR-F04 (P1):** Apply latency, 429, 5xx, malformed, disconnect, and partial-response faults consistently across services.

### G. Record/replay

- **FR-G01 (P0):** Capture all declared services with protocol, operation, fingerprint, response/events, timing, and service identity.
- **FR-G02 (P0):** Redact headers, cookies, URLs, JSON values, stream frames, binary metadata, and error bodies before cassette, audit, journal, OTEL, or fixture persistence.
- **FR-G03 (P0):** Preserve replay-only default and add `new_episodes`, `once`, `all`, and explicit `--strict` 503/no-egress behavior with bounded miss diagnostics.
- **FR-G04 (P1):** Preserve SSE, NDJSON, WebSocket, and provider event ordering/timing; require redaction on imports.
- **FR-G05 (P1):** Add deterministic request transforms for volatile timestamps/IDs while retaining original and normalized fingerprints.

### H. Drift detection

- **FR-H01 (P1):** Add `mockagents drift` comparing SDK/type shape, real-provider response (opt-in), and MockAgents response over one corpus.
- **FR-H02 (P1):** Compare field presence, types, nullability, enums, headers, usage, tool arguments, stream events/order, and errors, ignoring configured volatile fields.
- **FR-H03 (P1):** Critical mismatches fail CI; additive provider-only changes warn; compatible results are OK. Emit Markdown, JSON, SARIF, and JUnit.
- **FR-H04 (P1):** Use scrubbed corpora, least-privilege credentials, no third-party raw-data upload, and an offline replay mode.
- **FR-H05 (P2):** Add versioned baselines and expiring owner-approved exceptions.

### I. Chaos/resilience

- **FR-I01 (P0):** Apply chaos server-wide, per service, fixture, operation, request, and sequence index with documented precedence.
- **FR-I02 (P0):** Support fixed-seed probability, latency distributions, HTTP errors, rate limits, malformed schema, truncation, disconnect, reset/timeout, partial vector, MCP, and A2A failures.
- **FR-I03 (P0):** Record seed, policy, request ID, and effective action; allow deterministic per-request forcing.
- **FR-I04 (P1):** Bound latency/body amplification/CPU and disable chaos in strict replay unless explicitly enabled.

### J. Observability and control plane

- **FR-J01 (P0):** One bounded journal for all protocols with tenant, service, operation, scenario/fixture/cassette, status, latency, cost, chaos, retry/sequence, and correlation ID.
- **FR-J02 (P0):** Add Prometheus counters/histograms for requests, hits/misses, egress, chaos, vector queries/collections, latency, disconnects, and drift.
- **FR-J03 (P1):** Add stack status, route inventory, fixture export, journal query, drift report, and safe replay diagnostics through existing RBAC/audit controls.
- **FR-J04 (P1):** Add console views for topology, fixtures, collections, traces, chaos, cassette redaction preview, and drift findings; omit sensitive bodies by default.

### K. SDK, packaging, documentation

- **FR-K01 (P0):** Provide Python/TypeScript/Go stack lifecycle, base-URL injection, reset, journal assertions, and JUnit/JSON helpers.
- **FR-K02 (P0):** Preserve static binary, Docker, Helm, GitHub/GitLab support, and no-runtime mode; add all-service readiness and resource limits.
- **FR-K03 (P0):** Ship a complete RAG example, record/replay example, drift workflow, and chaos matrix.
- **FR-K04 (P1):** Add migration guides from MSW, ad hoc mocks, vcrpy, and separate vector-service doubles.

## 8. Proposed configuration

```yaml
apiVersion: mockagents/v1
kind: MockStack
metadata: {name: support-rag}
spec:
  server: {host: 127.0.0.1, port: 0, strict_replay: true}
  llm: {agents: [support-agent], providers: [openai, anthropic, gemini]}
  mcp: {servers: [support-tools], mount: /mcp}
  a2a: {servers: [billing-agent], mount: /a2a}
  vector:
    provider: qdrant
    mount: /vector
    collections: [{name: docs, dimension: 1536, metric: cosine, fixture: fixtures/docs.json}]
  services:
    search: {enabled: true, provider: tavily}
    rerank: {enabled: true, provider: cohere}
    moderation: {enabled: true, provider: openai}
  record: {mode: replay, cassette_dir: fixtures/cassettes, redact: true}
  chaos: {seed: 42}
  observability: {journal: true, metrics: true}
```

The exact wrapper kind is an open decision. Standalone `Agent`, `MCPServer`, `A2AServer`, and `Pipeline` documents remain backward compatible.

## 9. Canonical contracts

Every fixture includes version, service, match criteria, response/status/headers/body or events, optional timing, chaos, and labels. Every journal event includes event ID, timestamp, correlation ID, tenant, service, operation, request hash, fixture/cassette, status, latency, chaos action, egress, and a redacted bounded summary. Drift findings include operation, JSON path, severity, SDK/real/mock shapes, rule, baseline, and approval expiry.

## 10. Architecture direction

1. **Stack coordinator:** load/validate `MockStack`, start mounts/listeners, readiness, reload, and reset.
2. **Protocol adapters:** own wire translation for LLM, MCP, A2A, vector, search, and rerank.
3. **Fixture engine:** common matching, sequence, state, and bounded handlers; no wire-package imports.
4. **Recording:** normalize and redact before persistence; make egress explicit and auditable.
5. **Drift runner:** reuse adapters and shape extraction without bypassing privacy policy.
6. **Journal/metrics:** bounded, asynchronous, correlation-aware, tenant-isolated sinks.
7. **Control plane:** status/config/fixtures/drift endpoints behind the existing authorization chokepoint.

No adapter may silently call the network. Only explicit record/drift commands may use upstreams, and every such call emits `egress=true`.

## 11. Security and reliability

- Redact before cassette, audit, SQLite, OTEL attributes, metrics labels, console payloads, and errors.
- Derive tenancy from authenticated context, never user-controlled tenant headers.
- Loopback remains the default bind; remote exposure is explicit.
- Validate origin, sessions, protocol versions, bodies, stream frames, fixture counts, vector dimensions, and handler time.
- Isolate state/counters/fixtures per tenant and parallel test.
- Strict replay must prove zero upstream egress.
- Bound dynamic handlers and forbid arbitrary code execution in declarative configuration.
- Health must distinguish process readiness from enabled-service readiness.

## 12. Test and quality strategy

Unit tests cover matching, canonicalization, vector ranking/filtering, redaction, drift, chaos, and validation. Contract tests cover official SDKs and Pinecone/Qdrant/Chroma/Tavily/Cohere-compatible routes. Protocol tests cover MCP conformance, A2A JSON-RPC/SSE, SSE/NDJSON/WebSocket framing, sessions, and error shapes. Golden tests cover deterministic responses/embeddings across restarts. Record/replay tests cover all stream formats, imports, strict no-egress, canaries, and transforms. Security tests cover tenant isolation, SSRF/egress, bounds, authorization, and WebSocket origin/session checks. End-to-end tests cover a complete offline RAG stack.

Release gates:

- Existing MockAgents tests and MCP conformance remain green.
- `mockagents stack validate` rejects malformed cross-service configurations.
- Repeated runs produce identical canonical responses, ordering, hashes, and journal sequence within timing tolerance.
- Strict replay performs zero upstream requests and fails misses with documented 503 behavior.
- Vector client contracts, seeded critical drift, chaos, redaction, Docker, Helm, and CI smoke tests pass.

## 13. Phased delivery

### Phase 0 — Baseline and contracts (1–2 weeks)

Freeze the compatibility matrix, define stack/fixture/journal/policy schemas, add threat model and RAG corpus, and measure startup, memory, latency, hit rate, and CI runtime. Exit requires approved schemas and a green baseline.

### Phase 1 — Unified stack and retrieval/services (3–5 weeks)

Build stack coordinator, one-port mounts, lifecycle CLI, VectorMock, search/rerank adapters, moderation integration, shared journal/metrics/chaos, and RAG examples. Exit requires an offline one-config RAG demo.

### Phase 2 — Whole-stack record/replay (2–4 weeks)

Add cassettes, stream/event preservation, redaction, transforms, import safety, strict no-egress, and JUnit/JSON diagnostics. Exit requires a recorded RAG flow replaying without upstream access.

### Phase 3 — Drift and compatibility operations (3–4 weeks)

Add shape extraction, three-way triangulation, severity policy, baselines, scheduled/offline modes, and SARIF/JUnit/Markdown outputs. Exit requires seeded critical drift to fail CI and additive drift to warn.

### Phase 4 — Resilience and parity (2–4 weeks)

Add cross-service chaos, deterministic timing/fault profiles, demand-driven WebSocket parity, A2A/MCP stack parity, retries, duplicate events, partial vectors, and outages.

### Phase 5 — Adoption and hardening (ongoing)

Add SDK integration, migration guides, console topology, benchmark tuning, provider profiles, and production-shaped CI/homelab drills.

## 14. Success metrics

These are proposed targets, not current measurements:

- ≥99.9% canonical response/ordering repeatability; 100% fixture-body/embedding repeatability.
- 100% zero-egress strict replay runs.
- Reference corpus covers LLM, MCP, vector, search, rerank, moderation, A2A, streaming, and 10+ failure modes.
- ≥95% client contract pass rate before a profile is stable.
- Critical drift detected in the same CI run; scheduled checks within 24 hours.
- p95 local non-streaming fixture overhead ≤25 ms; stack startup ≤3 seconds plus fixture load.
- Zero secret-canary leaks; bounded journal/cassette growth.
- ≥80% reduction in live AI calls in reference CI; one config replaces at least three separately managed mocks.
- New user starts the RAG example and runs a deterministic test in under 10 minutes.

## 15. Risks and open decisions

Risks include scope expansion into a general integration platform, vendor-specific vector semantics, secret-bearing recordings, arbitrary dynamic handlers, provider credential/data exposure in drift, timing flakiness, and adapter maintenance. Mitigations are narrow protocol seams, compatibility profiles, pre-write redaction, bounded declarative handlers, scrubbed/offline drift, tolerance-based timing assertions, and demand thresholds.

Open decisions:

1. New `MockStack` kind versus a wrapper file around existing documents.
2. One-port default versus separate ports plus a generated manifest.
3. Vector API subset stable for v1: Pinecone, Qdrant, Chroma, or all three.
4. Exact search/rerank wire parity versus a neutral service API.
5. Allowed credentials/data classes and execution environment for drift.
6. Preserve current replay 404 by default and add strict 503, or change the default.
7. Supported runtime/version matrix and fixture/body/vector limits.
8. In-repo versus control-plane drift approvals and required console scope.

## 16. Definition of done

The initiative is complete when approved schemas are versioned; one stack config starts the reference LLM/MCP/A2A/vector/search/rerank/moderation flow; standalone behavior remains compatible; record/replay, strict no-egress, redaction, chaos, journal, and drift gates work across the stack; unit/protocol/security/golden/e2e/container/CI tests pass; performance/privacy/compatibility/usability targets are measured; migration guides/examples are complete; and maintainers approve ownership and support policy.

## 17. Traceability

| Article concept | Requirements |
|---|---|
| One config/one port | FR-A01–A05 |
| LLMock providers/streams/tools/reasoning | FR-B01–B04, FR-C01–C04 |
| MCPMock | FR-D01–D02 |
| A2AMock | FR-D01, FR-D03 |
| VectorMock | FR-E01–E05 |
| Search/rerank/moderation | FR-F01–F04 |
| Record/replay | FR-G01–G05 |
| Drift detection | FR-H01–H05 |
| Chaos | FR-I01–I04 |
| Embeddings/WebSockets/physics | FR-C03–C04, FR-E05 |
| Journal/metrics/CI/packaging | FR-J01–J04, FR-K01–K04 |

