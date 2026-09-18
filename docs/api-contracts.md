# API contract scope and verification

`docs/api-spec.yaml` describes management, core model generation, embedding,
moderation, search, and rerank operations. Supplemental wire schemas are generated
from Go types plus reviewed validation overlays in `tools/contractcheck`.
`go run ./tools/contractcheck -write` regenerates them; the command without `-write`
and its tests reject stale schemas. JSON Schema does not express every semantic
rule (Go regular-expression validity, tenant ownership, cross-document references,
model-specific dimensions, or dates relative to the current clock); runtime
validators remain authoritative for those rules.

`go run ./tools/handoffcatalog -check` compares source mounts with OpenAPI methods
and the **exact** entries in [api-exclusions.json](api-exclusions.json). It rejects
new unmapped mounts, stale exclusions, duplicate operation IDs, and spec-only
routes. Excluded operations are supported by the application; exclusion means
that this OpenAPI document is not their complete wire specification. They are
never represented by an empty success schema to inflate coverage.

| Excluded profile | Contract and executable evidence |
| --- | --- |
| Conversations | [Provider integration guide](handoff/integrations.md), [handler](../internal/adapter/conversations.go), [tests](../internal/adapter/conversations_test.go). Tenant-scoped items; polymorphic message/function items; process-local lifetime. |
| Files and OpenAI batches | [Files](../internal/adapter/files.go), [batches](../internal/adapter/batches.go), [integration guide](handoff/integrations.md). Multipart upload and JSONL input/output; synchronous mock job execution. |
| Anthropic batches and token count | [Batch handler](../internal/adapter/anthropic_batches.go), [Anthropic handler](../internal/adapter/anthropic.go). Inline batch request arrays, JSONL results, estimated counts. |
| Realtime | [Realtime implementation](../internal/adapter/realtime.go), [integration guide](handoff/integrations.md). WebSocket events, bootstrap endpoints, text-only mock profile. |
| Qdrant, Pinecone, Chroma | [Vector guide](handoff/integrations.md), [Qdrant](../internal/adapter/qdrant.go), [Pinecone](../internal/adapter/pinecone.go), [Chroma](../internal/adapter/chroma.go). SDK fixture profiles and filter validation, not exhaustive upstream products. |
| MCP | [MCP guide](handoff/tools.md), [listener](../cmd/mockagents/mcp.go). Separate JSON-RPC transport, HTTP/SSE helpers and stdio. |
| A2A | [A2A guide](handoff/integrations.md), [listener](../cmd/mockagents/a2a.go). Separate card discovery and JSON-RPC message/task lifecycle. |
| Internal engine diagnostic | [API guide](handoff/api.md), [server](../internal/server/server.go). Optional engine-native endpoint; not a stable provider API. |

Authentication is conditional on deployment mode. Management routes use the
principal and role floor from `internal/server/route_authz.go`; provider endpoints
allow anonymous SDK traffic and attach tenant scope only for valid credentials.
The standalone MCP/A2A listeners do not inherit that policy. Request body bounds
on the main server also apply to provider requests; intentional chaos may violate
normal response schemas or terminate the connection.

Ollama and Bedrock share the normal LLM quota/log/metric classifier. Anonymous
requests bypass tenant quotas. SSE, NDJSON, and AWS eventstream responses are
marked streaming and their bodies are not buffered; streamed token spend remains
outside the current body-derived accounting model. Non-streaming Ollama usage is
read from `prompt_eval_count`/`eval_count`; Bedrock uses camel-case usage fields
and resolves the model from the request path. Configured prices determine spend.
