# API reference

The [route catalog](reference/routes.md) enumerates every built-in HTTP mount found in main-server adapters/management wiring and the standalone MCP/A2A commands. It includes exact method/path, handler and source. The [field dictionary](reference/model-fields.md) provides exact named request/response fields. Use [OpenAPI](../api-spec.yaml) for its documented management/provider schemas and [management guide](../../site/docs/guides/management-api.md) for additional examples. [Contract coverage](../api-contracts.md) checks 53 OpenAPI operations and 65 exact source-bound exclusions; excluded endpoints remain supported through their documented compatibility profiles.

The main listener defaults to `http://127.0.0.1:8080`. MCP HTTP defaults to `127.0.0.1:8081`; A2A defaults to `:8083` on all interfaces and has no bind-address flag in this baseline. See the [listener matrix](deployment.md#listener-defaults). GET ServeMux patterns accept HEAD as well. Individual routes may be absent in an embedded server: tenancy/key routes need tenancy wiring, audit needs its store, costs need interaction/pricing wiring, pipeline routes need their registry, quota routes need the enforcer, and SSO routes need valid SSO configuration. `GET /api/v1/identity` reports capabilities derived from mounted routes and the actual caller's role.

## Authentication and tenant scope

Single-tenant mode is unauthenticated. With `MOCKAGENTS_MULTI_TENANT=1`, non-exempt routes require a valid MockAgents API key or SSO session. Accepted key headers are Bearer Authorization, `X-Api-Key`, then Azure `api-key`; session cookies and session-token Bearer credentials are supported. `/api/v1/health` and `/api/v1/ready` are public probes; auth login/callback/logout perform their own flow checks.

Provider adapter paths are auth-exempt for SDK compatibility. A valid MockAgents credential attaches a principal and scopes agent/resource access; a missing or invalid credential may continue anonymously and use the global namespace. The optional `/v1/engines/process` test route is also open and unmetered. Do not use provider HTTP success to validate a key or assume multi-tenant mode protects the entire listener from anonymous traffic.

Responses, Conversations, Files and Batches use tenant-scoped stores. A foreign or unknown Responses previous_response_id returns 404; anonymous history occupies a separate namespace. store:false disables standalone Responses history retention. Referenced Conversation items still append independently. History remains process-local with a global 1,024-entry FIFO bound. See [closure evidence](../reviews/2026-09-18-application-actions.md).

The principal's tenant ID is authoritative. Caller-controlled tenant headers/body fields do not grant ownership. A role floor is necessary but does not replace resource ownership checks. Roles ascend `viewer < editor < admin < platform`; platform is bootstrap-only, not assignable by normal key APIs. `/metrics` is platform-only in multi-tenant mode because its labels describe process-wide activity.

## Management operations

All paths below are relative to the main listener. Exact route floors, including the distinction between `roleOpen` and viewer, are in the [catalog](reference/routes.md) and [policy table](../../internal/server/route_authz.go).

| Operation family | Request / response | Important statuses and constraints |
| --- | --- | --- |
| GET health / ready | Health includes status/version/uptime/catalog information; readiness names checks | Health 200; readiness 200/503 (fixtures, log store, draining) |
| GET identity | `IdentityResponse`: mode, authenticated, role (null locally), capabilities, tenant/key IDs, server version | Does not expose plaintext credentials; authenticated in multi-tenant mode |
| GET agents, GET agent | Summary array / full `AgentDefinition`; detail carries ETag and revision headers | 404 when not visible; effective revision is not interchangeable with ETag |
| POST agents | YAML or JSON `AgentDefinition` → `AgentWriteResponse` receipt | Editor; 201, 409 duplicate; caller ownership stamped; 1 MiB cap |
| PUT agent | Definition with path/body name agreement; optional `If-Match` or create-only `If-None-Match: *` | Editor; 200 replace/201 create; 412 stale; no headers preserves unconditional compatibility |
| DELETE agent | Path name → deletion receipt | Editor; own bucket only; persisted file removed before unregister |
| POST agent reload | Existing name → reload receipt | Editor; reload source; parse/validation/file errors are explicit |
| GET logs / log ID | Filtered rows/detail with optional cost annotations | Tenant-scoped; `agent`, `session_id`, `session_prefix`, `since`, `until`, `limit`, `offset`; source distinguishes HTTP/pipeline |
| DELETE logs | Purge caller's scoped logs | Admin; destructive operation |
| GET logs/stream | SSE live feed with heartbeat/events | Optional broadcaster; clients must reconnect; no promise of lossless replay |
| GET logs/stream/metrics | Subscriber/broadcast diagnostic counters | Admin; process diagnostic surface |
| GET costs | Window, total requests/tokens/USD and model/agent groups | Viewer; bounded scan, estimates rather than billing |
| GET audit | Filtered audit event records | Admin; tenant visibility enforced |
| GET pipelines / pipeline | Summary array / `PipelineDefinition` plus ETag | Viewer; shared registry |
| POST pipeline run | `{input, session_id?}` → `PipelineResult` | Viewer; 400/413 malformed input, 404 missing, 422 partial failure, 503 unavailable executor |
| PUT pipeline | JSON definition with GET ETag in `If-Match` | Platform; existing only; 428 absent precondition, 412 stale, 422 invalid |
| POST config/validate | Raw YAML/JSON definition or `{"yaml":"definition text"}` → `{ok, kind, errors}` | Editor; 1 MiB cap; diagnostics include file/line/column/field/message/suggestion |
| GET/POST tenants | Collection / new tenant `{name}` | Platform; 409 conflicting name |
| DELETE tenant | Tenant path ID | Platform; dependent tenancy records cascade per Store |
| GET tenant keys | Key metadata array, never stored plaintext | Editor and ownership gate |
| POST tenant keys | Name/role → `NewAPIKeyResult` (`key`, one-time `plaintext`) | Admin and ownership gate; platform role cannot be minted here |
| PATCH key | Requested role → updated metadata | Admin; store-level target privilege checks |
| POST key rotate / tenant keys rotate | One-key or bulk replacement credentials | Admin; treat returned plaintext as one-time secret material |
| POST keys/me/rotate / burn | Self-service own credential lifecycle | Viewer; session/API-key distinctions enforced by handlers |
| DELETE key | Key path ID | Admin, ownership/target checks; invalidates credential |
| GET quota | Config + current usage | Viewer, own tenant |
| PUT tenant quota | Rate/spend settings | Platform; persisted caps; tenant admin cannot raise own cap |
| GET metrics | Prometheus text exposition | Platform in multi-tenant mode |
| GET auth/login, GET auth/callback, POST auth/logout | Redirect/cookie flow | Only mounted with configured OIDC; callback state/identity checks |

Canonical field shapes for these operations live in [handlers](../../internal/server/), [tenancy types](../../internal/tenancy/types.go), [quota types](../../internal/quota/quota.go), and the OpenAPI components. General management failures use an `error` string; validation and pipeline execution add structured diagnostics/results. Do not parse human error text when a stable code exists.

Authentication/role middleware is an exception to the handler error string: it returns `{"error":{"type":"authentication_error","message":"..."}}` with 401/403, 429 after the failed-authentication budget, or 500 on an auth-store failure. Quota middleware uses `{"error":{"type":"rate_limit_exceeded|spend_quota_exceeded","message":"..."}}` with 429/402 even on non-OpenAI provider paths. Clients must handle the middleware envelope as well as the selected handler's envelope. Evidence: [authentication](../../internal/tenancy/middleware.go), [quota](../../internal/server/quota_middleware.go).

For log queries, `fields=meta` removes body strings after cost annotation; fetch a specific log to retrieve its captured bodies. Other `fields` values return the full row. Validation returns HTTP 200 with `ok: false` and an `errors` array for invalid definitions, including malformed YAML/JSON and empty raw input. An explicitly empty `yaml` wrapper returns 400; body read/size failures return 400/413. A successful report may serialize `errors` as `null`, so use `ok` rather than assuming an array is always present. The HTTP response does not include the internal report's lint warnings, and this single-document endpoint does not check references against the loaded registry. See [ValidateHandler](../../internal/server/validate_handler.go) and [ValidateBytes](../../internal/config/validate_bytes.go).

For platform administration of another tenant's flat key routes (`/api/v1/keys/{id}` and its rotate path), supply `?tenant=<tenant-id>`. Without it, lookup uses the platform key's own tenant; non-platform callers cannot select a foreign tenant this way. Quota PUT persists settings and updates the receiving process only; other running replicas do not refresh their override maps automatically. Evidence: [key scoping](../../internal/server/tenancy_handlers.go), [quota writes](../../internal/server/quota_handlers.go).

## Provider requests and responses

The route catalog expands each CRUD operation below, including all item subpaths. Request fields listed here are the principal contract; consult its linked handler and model dictionary for optional fields and supported subsets.

| Family | Input → output | Implementation / edge cases |
| --- | --- | --- |
| OpenAI Chat Completions | `model`, `messages`, optional tools/tool_choice/stream/response_format → choices/message/tool_calls/usage | [openai.go](../../internal/adapter/openai.go); tool arguments are JSON strings; SSE optional usage and finish handling |
| OpenAI Models | GET → model list derived from visible agents | Same handler; tenant scope applies |
| Anthropic Messages | `model`, `messages`, `max_tokens`, optional system/tools/stream/thinking → content blocks, stop_reason and usage | [anthropic.go](../../internal/adapter/anthropic.go); tool_use/tool_result, images, thinking/cache behavior |
| Anthropic count_tokens | Messages body → input_tokens | Engine-free synthetic count |
| Gemini | `/v1beta/models/{model}:generateContent` or `:streamGenerateContent`; contents/parts/tools → candidates/content/usageMetadata | [gemini.go](../../internal/adapter/gemini.go); suffix dispatch is inside `{modelmethod}` |
| Ollama | `/api/chat`: model/messages/stream/tools → message and timing/count fields | [ollama.go](../../internal/adapter/ollama.go); streaming is NDJSON |
| Bedrock Converse | `/model/{modelId}/converse` or `/converse-stream`: messages/system/toolConfig → output/stopReason/usage | [bedrock.go](../../internal/adapter/bedrock.go); stream uses Bedrock event framing |
| Azure OpenAI | Deployment chat/embedding paths or `/openai/v1/...` | [azure.go](../../internal/adapter/azure.go); delegates to shared OpenAI implementations |
| Responses | `model`, `input`, optional previous_response_id/conversation/tools/stream → response output items, usage, IDs | [responses.go](../../internal/adapter/responses.go); shared conversation store; does not mount every upstream Responses CRUD route |
| Conversations | Create/update with metadata/items; list/create/get/delete items | [conversations.go](../../internal/adapter/conversations.go); process-local state and tenant scoping |
| Embeddings | model/input, optional dimensions/encoding_format → indexed embedding data and usage | [embeddings.go](../../internal/adapter/embeddings.go); synthetic vectors, float/base64 output |
| Moderations | input and optional model → category flags/scores/applied input types | [moderations.go](../../internal/adapter/moderations.go); deterministic mock logic and service faults |
| Files | Multipart upload file/purpose; list/retrieve/content/delete | [files.go](../../internal/adapter/files.go); byte and retention limits, process-local store |
| OpenAI Batches | input_file_id, endpoint, completion_window, optional metadata; list/retrieve/cancel | [batches.go](../../internal/adapter/batches.go); dispatches JSONL items to supported chat/embedding/Responses handlers and writes output/error files |
| Anthropic Batches | Inline requests with custom_id/params; list/retrieve/cancel/delete/results | [anthropic_batches.go](../../internal/adapter/anthropic_batches.go); results are JSONL |
| Realtime | Client-secret/session creation, then WebSocket `/v1/realtime` with model/session config and events | [realtime.go](../../internal/adapter/realtime.go), [event engine](../../internal/realtime/); strict event mode optional |
| Qdrant | Create/get/delete collections; upsert/fetch/delete/search points | [qdrant.go](../../internal/adapter/qdrant.go); vector/payload filters are a subset |
| Pinecone | Index-prefixed upsert/query/fetch/delete/stats | [pinecone.go](../../internal/adapter/pinecone.go); namespace and provider profile mapping |
| Chroma | v2 tenant/database collection CRUD/count; add/upsert/get/query/delete/count points; heartbeat | [chroma.go](../../internal/adapter/chroma.go); explicit embedding arrays; path identifiers are not authentication |
| Cohere rerank | model/query/documents, optional top_n → indexed relevance_score results and meta | [cohere_rerank.go](../../internal/adapter/cohere_rerank.go); 1–1000 string documents; token-overlap score |
| Tavily search | query plus depth/results/domain/date options → query/answer/images/results/usage/request_id | [tavily_search.go](../../internal/adapter/tavily_search.go); max_results defaults 5, valid 1–20; configured results, not network search |
| Generic engine (opt-in) | Neutral `InboundRequest` → neutral `Response` or supported stream | Main server test endpoint; not a public provider compatibility contract |

Some accepted fields are compatibility placeholders rather than simulated capabilities. For example, Tavily's decoded `include_answer`/`include_raw_content` fields do not by themselves prove all upstream conditional-output semantics. Exact fidelity is established by handler behavior and conformance tests, not by the presence of a struct field.

## Errors, headers and streaming

HTTP quota/capture covers Chat Completions, Responses, embeddings, Anthropic Messages, Gemini generate/streamGenerate, Azure chat/embeddings, Ollama chat and Bedrock Converse/ConverseStream. Capture/metrics additionally include moderation and the optional engine endpoint. Vector/search/resource-management paths remain outside this classifier. SSE, Ollama NDJSON and AWS eventstream bodies are not buffered; streamed token spend remains uncounted. Non-streaming Ollama/Bedrock usage is extracted for configured pricing. Realtime and pipeline interactions retain their dedicated hooks. See [contract scope](../api-contracts.md).

Provider error envelopes differ: OpenAI-family uses an `error` object, Anthropic its typed error envelope, Gemini its status/code structure, Cohere a message, and Tavily a detail/error object. Typical statuses are 400 invalid request/schema, 404 unresolved model/resource, 413 body limit, 429 injected/rate quota, and 5xx simulated/server failure. Strict-tool request errors map to the provider's 400 representation. Some deliberately planted failures are successful HTTP responses with refusal/finish flags or malformed/truncated payloads.

`X-Request-Id` identifies a main-server request. Supported adapters use `X-Session-Id` for engine continuity and stamp diagnostic scenario/agent/fault/strict/image/hallucination headers where implemented. Do not require every transport to emit every header. Stream clients must parse that protocol's framing (SSE, NDJSON, WebSocket or event stream) and detect missing terminators; a stream cannot reliably change its already-sent HTTP status after a mid-stream failure.

## MCP and A2A RPC

MCP uses JSON-RPC envelopes `{jsonrpc:"2.0", id, method, params}` over stdio or HTTP. Dispatch supports initialize/initialized notification, ping, tools/list/call, resources/list/read/subscribe/unsubscribe, prompts/list/get, completion/complete and logging/setLevel. Sampling/roots are client-directed operations with test helper plumbing, not general server tools. Streamable HTTP has session lifecycle requirements; use the [MCP guide](../../site/docs/guides/mcp.md) and [server](../../internal/mcp/server.go). RPC protocol errors and a tool result with `isError` are different contracts.

A2A exposes discovery at `/.well-known/agent-card.json` and its legacy alias, plus POST `/` for message/send, message/stream, tasks/get and tasks/cancel. Parameters and responses use [A2A models](../../internal/a2a/server.go): a send can return Task or Message; streaming emits task/artifact/status events. Unsupported methods return JSON-RPC method errors. Agent Card defaults advertise the implemented JSONRPC profile. A2A tasks and engine conversation sessions are unrelated stores.

## Copyable requests

The following Bash examples assume `mockagents start --agents-dir examples` and the supplied fixtures. Bash `\` line continuations do not work in PowerShell; use the PowerShell example below there.

```bash
curl http://127.0.0.1:8080/api/v1/identity
curl http://127.0.0.1:8080/v1/models
curl -H 'Content-Type: application/json' \
  -d '{"input":"Research deterministic agent testing","session_id":"handoff-1"}' \
  http://127.0.0.1:8080/api/v1/pipelines/research-pipeline/run
```

For a chat call, replace `MODEL_FROM_LIST` with a loaded model:

```bash
curl -H 'Content-Type: application/json' -H 'X-Session-Id: handoff-chat-1' \
  -d '{"model":"MODEL_FROM_LIST","messages":[{"role":"user","content":"hello"}]}' \
  http://127.0.0.1:8080/v1/chat/completions
```

```powershell
$body = @{ input = 'Research deterministic agent testing'; session_id = 'handoff-1' } | ConvertTo-Json
Invoke-RestMethod -Method Post -Uri 'http://127.0.0.1:8080/api/v1/pipelines/research-pipeline/run' -ContentType 'application/json' -Body $body
```

In multi-tenant mode add `Authorization: Bearer <MockAgents-key>` to management requests and to provider requests that should use that principal. For safe agent editing, GET the definition, retain its **ETag**, then PUT the modified complete document with that entire header value, including quotes, as `If-Match`. On 412, fetch current content and reconcile rather than blindly retrying. A `persisted: false` create/update receipt means a restart will not retain the edit; the delete receipt uses the same type but is not a saved definition.
