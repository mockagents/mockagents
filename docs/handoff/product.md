# Product overview

MockAgents makes integration behavior reproducible without requiring a real model call for every test. Teams declare YAML/JSON fixtures, run a local Go service, and configure the application under test to use that service. The important result is evidence that the application handles a known response or failure: choosing the right tool, parsing a stream, preserving a conversation, retrying a rate limit, or rejecting planted bad output.

It does not evaluate whether a real model reasons well. Canned text, deterministic embeddings, search fixtures, and simulated tool outputs do not establish production answer quality. Keep real-provider contract checks and model evaluations alongside fixture tests; see [evals versus tests](../EVALS_VS_TESTS.md).

## Supported building blocks

| Definition / interface | Product capability | Boundary |
| --- | --- | --- |
| `Agent` | Scenario matching, content templates, tool calls, semantic faults, strict tools, streaming, chaos | A mock response policy; no autonomous planning or arbitrary function execution |
| `Pipeline` | Sequential, parallel, or conditional graph execution over named agents | In-process orchestration; graph traversal is not a general workflow scheduler |
| `TestSuite` | Multi-turn fixture execution and assertions, CLI reports | Exercises engine/pipeline behavior; does not alone test provider wire serialization |
| `MCPServer` | Tools, resources, prompts, completions, notifications, HTTP/stdio | Separate mock MCP listener/process |
| `A2AServer` | Agent Card, message/task JSON-RPC and streaming | Separate mock A2A task store, not the pipeline executor |
| `VectorCollection` | Startup collection/points for Qdrant, Pinecone, Chroma profiles | In-memory vector mock with bounded dimensions, points, and queries |
| `SearchService` | Tavily search fixtures and service fault profiles | Fixture matching, not live web retrieval |
| Cassettes | Record real responses, replay matching requests, convert selected fixtures | Recording/record-on-miss can use the network and real credentials |

The five accepted `Agent.spec.protocol` values are `openai-chat-completions`, `anthropic-messages`, `google-gemini`, `ollama-chat`, and `bedrock-converse`. Other HTTP surfaces are adapters or auxiliary services, not additional Agent kind values. The [route catalog](reference/routes.md) lists Chat Completions, Responses, Conversations, embeddings, moderation, Files/Batches, Azure paths, Realtime, search/rerank, and vector operations. Support is the implemented subset of each protocol, not every feature of its upstream service.

## Personas and journeys

| Persona | Starting problem | Journey | Success evidence |
| --- | --- | --- | --- |
| Application developer | SDK/tool loop breaks on an edge case | Add fixture → validate → start server → redirect SDK → run application test | Response/tool IDs, arguments, and final application state meet assertions |
| Test engineer | Intermittent upstream failures make CI unreliable | Capture or author scenario → choose deterministic fault → isolate server/session → run suite | Repeatable pass/fail and useful trajectory/report |
| Platform / DevEx engineer | Teams need a shared mock catalog | Configure tenancy → issue scoped keys → provide fixtures and quotas → inspect logs | Correct visibility, management roles, and per-tenant usage |
| Agent workflow developer | Multi-agent routing or error recovery regresses | Define pipeline → run with known input → assert node/tool trajectory | Expected ordered node list and explicit partial failure result |
| Operator | Service is alive but unusable | Compare health/readiness → inspect fixture loading/store status → investigate logs | Readiness recovers after the actual dependency/config issue is fixed |

## End-to-end workflows

For an SDK integration, start with [minimal-agent.yaml](../../examples/minimal-agent.yaml), run `mockagents validate examples/minimal-agent.yaml`, then `mockagents start --agents-dir examples`. Point the provider SDK at the corresponding local base URL and select a model from `GET /v1/models`. Inspect the management catalog and interaction logs to confirm which scenario answered. Assert application behavior in the application's test runner; do not treat a successful HTTP status alone as proof.

For a pipeline, load the referenced agents and [research pipeline](../../examples/research-pipeline.yaml), POST an input to `/api/v1/pipelines/research-pipeline/run`, and inspect each returned node. A missing downstream dependency can leave successful earlier nodes in the `422` response. Declarative [research suite](../../examples/research-suite.yaml) provides fixture-level trajectory assertions.

For a provider regression, record intentionally using `mockagents record`, review/redact the cassette, then use `mockagents replay --strict --cassette <file>`. Strict mode rejects upstream configuration and returns `503` on misses. Non-strict replay defaults to `404` misses; recording modes can explicitly forward misses. This choice determines whether the test is offline.

## Expectations and acceptance criteria

Fixtures should be reviewable, versioned, and validated with their cross-document references. Tests should control sessions, random/time helpers, and fault probabilities. IDs, timestamps, random tool failures, and real elapsed latency are not byte-stable just because response text is fixed.

The service should start locally without an external database or C compiler. Shared deployments require explicit ownership, storage, and network decisions. There is no published availability or latency SLO asserted by this handoff. Use [benchmarks](../benchmarks/README.md) to measure a workload and [testing](testing.md) to verify its failure paths. The console is a separate Next.js application, and cost displays are estimates derived from mock usage, not invoices or measured savings.

Implementation evidence: [definition types](../../internal/types/), [validators](../../internal/config/), [adapter registry](../../internal/adapter/registry.go), [runner](../../internal/runner/runner.go), and [CLI commands](../../cmd/mockagents/).
