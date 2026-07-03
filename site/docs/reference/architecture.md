# Architecture

This page is the reference architecture for MockAgents: how the pieces fit
together, what each package owns, and the exact request path for the
features people ask about most (chat completions, Realtime, MCP). If you're
here to *use* MockAgents rather than contribute to it, the
[Quickstart](../getting-started/quickstart.md) and the [Guides](../guides/testing-agents.md)
are a better starting point — come back here when you want to understand
*why* something behaves the way it does, or when you're contributing code.

MockAgents is a Go server that impersonates several LLM-provider wire
protocols — OpenAI Chat Completions, Responses, Conversations, Embeddings,
Moderations, Files, Batches; Anthropic Messages (+ Batches); Gemini
`generateContent`; OpenAI Realtime over WebSocket; MCP (Streamable HTTP +
stdio); and A2A — in front of one protocol-neutral **engine** that matches a
request to a scenario declared in YAML and returns a canned response,
simulated tool call, or SSE stream. No network call ever reaches a real
provider unless you're using record/replay against one on purpose.

## Contents

- [System context](#system-context)
- [Containers and components](#containers-and-components)
- [Packages (`internal/`)](#packages-internal)
- [How a request becomes a response](#how-a-request-becomes-a-response)
- [Request-flow sequence: POST /v1/chat/completions](#request-flow-sequence-post-v1chatcompletions)
- [Realtime: WebSocket session with server VAD](#realtime-websocket-session-with-server-vad)
- [MCP: Streamable HTTP transport](#mcp-streamable-http-transport)
- [Cross-cutting: chaos and strict-tools](#cross-cutting-chaos-and-strict-tools)
- [Data and deployment view](#data-and-deployment-view)
- [Design rules](#design-rules)

## System context

```mermaid
flowchart LR
    dev["Developer / CI job<br/>writes YAML agents,<br/>points an app's SDK base_url<br/>at MockAgents"]
    sdkApp["Application under test<br/>(official OpenAI / Anthropic / Google SDK,<br/>LangChain, an MCP client, or an A2A client — unmodified)"]
    gui["MockAgents GUI<br/>catalog, pipeline DAG, live logs,<br/>costs, audit, editor, tenant admin"]
    ci["CI pipeline<br/>make test / mockagents test /<br/>mockagents contract"]

    subgraph boundary["MockAgents"]
        server["MockAgents server<br/>Go binary: net/http server + engine"]
    end

    realProvider["Real provider API<br/>OpenAI / Anthropic / Gemini"]

    dev -->|"defines agents, runs CLI"| server
    sdkApp -->|"HTTPS/WS, provider wire protocol<br/>chat/messages/generateContent/realtime/mcp/a2a"| server
    gui -->|"REST + SSE, /api/v1/*<br/>Bearer or session cookie"| server
    ci -->|"spawns in-process or as a subprocess"| server
    server -.->|"record mode only:<br/>proxies + captures a cassette"| realProvider
```

Replay mode never contacts `realProvider` — a recorded cassette answers
instead.

The point of MockAgents is that `sdkApp` never knows it isn't talking to a
real provider: base URL and API key are the only things that change. The GUI,
CI runner, and record/replay proxy are auxiliary consumers of the same
server.

## Containers and components

```mermaid
flowchart TB
    subgraph client["Clients"]
        SDKs["Client SDKs / frameworks<br/>(OpenAI/Anthropic/Google SDKs,<br/>MCP client, A2A client, WS client)"]
        GUI["gui/ (Next.js 15)"]
        CLI["cmd/mockagents (Cobra CLI)"]
    end

    subgraph srv["internal/server"]
        MW["middleware.go<br/>observability → RequestContext →<br/>Recovery → StructuredLogger → CORS →<br/>MaxBodySize → RealtimeBrowserAuth →<br/>tenancy.AuthMiddleware →<br/>WithPrincipalTenantScope → InteractionCapture →<br/>QuotaEnforce"]
        ROUTES["server.go registerRoutes<br/>mux: /api/v1/* via mountManaged<br/>+ route_authz.go role floors"]
        LOGWORK["log_worker.go / log_broadcaster.go<br/>bounded async writer pool + SSE fan-out"]
    end

    subgraph ad["internal/adapter"]
        OAI["openai.go / anthropic.go / gemini.go<br/>azure.go / realtime.go"]
        CONV["conversations.go / responses.go<br/>batches.go / embeddings.go / files.go"]
        CHAOSA["chaos.go / strict.go<br/>ChaosError, StrictToolError → wire errors"]
    end

    subgraph eng["internal/engine"]
        REG["agent_registry.go<br/>byModel index, *ForTenant"]
        MATCH["scenario_matcher.go"]
        GEN["response_generator.go"]
        TOOL["tool_processor.go / tool_validator.go"]
        CHAOSE["chaos.go — ChaosInjector.Before/After"]
        STRICT["strict.go — StrictToolsFor"]
        PIPE["pipeline.go / pipeline_registry.go"]
        STATE["state/ — session history"]
    end

    subgraph side["Side systems"]
        TEN["internal/tenancy<br/>Store (SQLite/Postgres), RBAC, auth_cache"]
        AUDIT["internal/audit — append-only log"]
        QUOTA["internal/quota — rate + spend Enforcer"]
        OIDC["internal/oidcauth + server/oidc_handlers.go"]
        PRICING["internal/pricing"]
        OBS["internal/observability — OTel"]
        MCP["internal/mcp + internal/mcpadmin"]
        RT["internal/realtime"]
        A2A["internal/a2a"]
        REC["internal/recording"]
        TSCHEMA["internal/toolschema"]
        STORAGE["internal/storage — SQLite interaction log"]
    end

    SDKs -->|HTTP/WS| MW
    GUI -->|REST/SSE, Bearer or cookie| MW
    CLI --> ROUTES
    MW --> ROUTES
    ROUTES --> OAI
    ROUTES --> CONV
    ROUTES --> RT
    CLI -.->|"own listener: mockagents mcp"| MCP
    CLI -.->|"own listener: mockagents a2a"| A2A
    OAI --> CHAOSA
    OAI -->|engine.ProcessRequestContext| REG
    REG --> MATCH --> GEN --> TOOL
    GEN --> TSCHEMA
    TOOL --> TSCHEMA
    CHAOSE -.->|before/after| REG
    STRICT -.-> REG
    PIPE --> REG
    REG --> STATE
    CHAOSA -->|translate| CHAOSE
    CHAOSA -->|translate| STRICT
    MW --> LOGWORK --> STORAGE
    MW --> TEN
    MW --> QUOTA
    ROUTES --> AUDIT
    ROUTES --> OIDC
    LOGWORK --> PRICING
    RT --> REG
    RT --> QUOTA
    RT --> LOGWORK
    MCP --> REG
    MCP --> TSCHEMA
    A2A -.->|own package, not in adapter.Registry| eng
    TEN -.->|allowed import direction| eng
```

**Import-direction constraints** (enforced by convention + code review, not a
linter rule):

- `tenancy` may import `engine`; `engine` never imports `tenancy`. The engine
  reads/writes the tenant id through `engine.WithTenantID` /
  `engine.TenantIDFromContext` (`internal/engine/reqmeta.go`) so it stays
  agnostic of how a caller authenticated.
- `audit.Recorder` takes a principal-extraction **function**
  (`principalToActor` in `internal/server/server.go`) instead of importing
  `tenancy` directly, for the same reason.
- `engine` never imports a wire-format package — `internal/adapter/*` and
  `internal/streaming/*` are the only packages that know what OpenAI/
  Anthropic/Gemini JSON looks like. `engine.Response` / `engine.InboundRequest`
  are the neutral boundary type.
- `internal/a2a` and `internal/mcp` are **not** registered through
  `adapter.DefaultRegistry` — they mount their own routes directly in
  `cmd/mockagents` (A2A) or run as their own process (MCP isn't wired into
  the main HTTP server; it's its own listener started by `mockagents mcp`).

## Packages (`internal/`)

| Package | Role |
|---|---|
| `adapter/` | Wire-format translators. `openai.go`, `anthropic.go`, `gemini.go`, `azure.go` convert provider JSON ↔ engine types; `conversations.go`/`responses.go`/`responses_stream.go` mock the OpenAI Conversations + Responses APIs; `batches.go`/`anthropic_batches.go` mock async batch endpoints; `embeddings.go`, `moderations.go`, `files.go`, `structured_output.go` cover the smaller surfaces; `realtime.go` bridges a WebSocket connection into `internal/realtime`. `registry.go` is the extension seam (`Adapter` interface + `DefaultRegistry`). `strict.go`/`chaos.go` translate `engine.StrictToolError`/`engine.ChaosError` into each provider's wire error shape. |
| `engine/` | The core, provider-agnostic. `engine.go`'s `ProcessRequestContext` orchestrates: chaos pre-check → strict-tools request validation → scenario match/generate/tool-loop inside a session turn → strict tool_choice forcing → tool processing → chaos post-latency (see [walkthrough](#how-a-request-becomes-a-response) below). `agent_registry.go` looks up an agent (by-model index + `*ForTenant` visibility). `scenario_matcher.go` picks a scenario, `response_generator.go` produces content. `tool_processor.go` handles simulated tool calls. `chaos.go` is the fault-injection seam. `strict.go` is the strict-tools seam. `pipeline.go`/`pipeline_registry.go` run a `kind: Pipeline` document (sequential/parallel/graph over multiple agents) as a distinct concept from a single `Agent`. |
| `toolschema/` | JSON-Schema-subset validator used to check simulated tool-call arguments against a tool's declared `inputSchema`, plus a stricter OpenAI-structured-outputs-subset checker used when an agent opts into `strict: true` function schemas. Consumed by the engine (strict-tools + tool validation) and by `internal/mcp` for `tools/call` argument validation. |
| `server/` | `net/http` server, middleware, and route handlers for the LLM + management APIs. `route_authz.go` is the single role-floor table + `mountManaged` chokepoint for every `/api/v1` route. `log_worker.go`/`log_broadcaster.go` own async logging + SSE fan-out. `quota_middleware.go` enforces per-tenant rate/spend limits. `realtime_wiring.go` wires quota + logging hooks into the Realtime adapter (which never passes through the HTTP middleware chain — see the [Realtime section](#realtime-websocket-session-with-server-vad)). |
| `tenancy/` | Multi-tenant store + bcrypt API keys + RBAC middleware. `Store` is an interface with two impls: `SQLiteStore` (default) and `PostgresStore`, selected at startup by `MOCKAGENTS_TENANCY_DSN`. `middleware.go`'s `AuthMiddleware` is dual-auth: API key (`Authorization`/`X-Api-Key`/Azure `api-key`) or session cookie (`mockagents_session`) resolve to the same `Principal`. RBAC roles are ordered `viewer < editor < admin < platform`; `platform` (cross-tenant operator) can only be minted by the CLI bootstrap path, never through the API. |
| `audit/` | Append-only audit log, SQLite-backed. Twelve event kinds covering tenant/key/agent/pipeline lifecycle plus `auth.denied`. |
| `quota/` | Per-tenant rate + monthly-spend enforcement. A token bucket handles rate (429 + `Retry-After`); spend is tracked in a shared ledger row so the monthly cap is accurate across replicas, with a 5-second local cache to avoid a store round trip per request. |
| `oidcauth/` | OIDC relying-party seam for SSO login, wrapping `coreos/go-oidc` behind a small interface so the callback handler is unit-testable with a fake provider. |
| `pricing/` | Per-model cost table + usage extractor, used for cost dashboards and quota spend accounting. |
| `mcp/` | JSON-RPC 2.0 dispatch for `kind: MCPServer` documents, with three transports: Streamable HTTP (session-scoped, POST dispatch in JSON or SSE mode, one resumable GET stream per session), stdio (line-delimited frames), and a bidirectional server-initiated channel for sampling/roots requests. See the [MCP sequence](#mcp-streamable-http-transport). |
| `mcpadmin/` | A separate concern from `mcp`'s bidirectional/sampling surface: it re-exposes the agent management write API (create/get/put/delete/validate/list agents) as MCP tools, so an MCP client can manage MockAgents' own agent catalog. |
| `realtime/` | Server-side state machine for the OpenAI Realtime mock: session/event handling, server voice-activity detection (an energy-threshold detector), and deadline-based response pacing with barge-in and idle-timeout. See the [Realtime sequence](#realtime-websocket-session-with-server-vad). |
| `a2a/` | Mocks the A2A (Agent-to-Agent) protocol for `kind: A2AServer` documents: agent-card discovery, a JSON-RPC surface (`message/send`, `message/stream`, `tasks/get`, `tasks/cancel`), and task lifecycle with real SSE streaming. |
| `recording/` | Cassette format + record/replay handlers, including SSE streams. |
| `streaming/` | SSE chunking used when a chat/messages request sets `stream: true`. Supports a deterministic fixed-seed pacing model (TTFT + tokens-per-sec + jitter) and, when an agent sets `ttft_p50_ms`/`itl_p50_ms`, a per-stream-seeded lognormal "load-target" sampler, plus mid-stream fault injection. |
| `storage/` | SQLite interaction logging (pure-Go, no cgo). Default DB file `.mockagents.db`. `MOCKAGENTS_LOG_BODIES` controls response-body capture depth; `MOCKAGENTS_LOG_MAX_ROWS` bounds the table via a background pruner. |
| `config/` | YAML/JSON loader + validator. Splits a directory's files by top-level `kind` into `Agent`, `Pipeline`, `TestSuite`, `MCPServer`, `A2AServer` documents. `chaos_presets.go` expands a named `chaos.preset` into a `ChaosConfig`. The schema lives at `schema/mockagents-v1-agent.json`. |
| `types/` | Domain types shared across packages. Changes here ripple widely. |

Outside `internal/`: `cmd/mockagents/` (Cobra CLI), `gui/` (Next.js console),
`sdk/{python,typescript,go}/`, `deploy/` (Helm chart + GitHub/GitLab CI
templates).

## How a request becomes a response

This is the real call order inside `engine.Engine.ProcessRequestContext`,
which every protocol adapter calls after translating its wire request into
an `engine.InboundRequest`:

1. Start a tracing span if enabled.
2. Resolve the agent for the caller's tenant (name → model → single-agent
   fallback for anonymous callers).
3. Cheap context-cancellation bail-out before doing any real work.
4. **Chaos pre-check**: rate-limit check, then HTTP-error/timeout injection,
   then a connection-layer fault, in that order — any of these can return
   early, before matching or generation ever run.
5. Extract the latest user message; reject an empty turn (tolerant of a turn
   that's purely a tool result).
6. **Strict-tools request validation**: round-trip tool-call-id validation,
   then `tool_choice` name validation, then per-function strict JSON-schema
   validation. In enforce mode, a violation returns an error immediately; in
   warn mode it's collected and the request proceeds.
7. The session runs the turn: scenario match → generate content → tool-loop
   convergence guard (drops an identical tool call re-issued after its
   result — this is what makes the simulated agent loop actually terminate)
   → `tool_choice: "none"` suppression → strict tool_choice forcing → tool
   call resolution.
8. Any collected strict-tools warnings are attached to the response.
9. **Chaos post-latency**: sleeps for the configured latency distribution —
   only *after* all real work, including tool processing, is done.

The adapter then translates the response (or a typed chaos/strict error)
back to the wire shape, decides JSON vs SSE from `stream: true`, and —
outside all of that — the server asynchronously logs the interaction.

## Request-flow sequence: POST /v1/chat/completions

```mermaid
sequenceDiagram
    autonumber
    participant Client
    participant OTel as observability.HTTPMiddleware
    participant RC as RequestContext
    participant Rec as Recovery
    participant Log as StructuredLogger
    participant CORS
    participant MaxBody as MaxBodySize
    participant Auth as tenancy.AuthMiddleware
    participant Scope as WithPrincipalTenantScope
    participant Cap as InteractionCapture
    participant Quota as QuotaEnforce
    participant Mux as http.ServeMux
    participant OAI as adapter.OpenAIHandler
    participant Eng as engine.ProcessRequestContext
    participant Worker as LogWorker (async)

    Client->>OTel: POST /v1/chat/completions
    OTel->>RC: stamp X-Request-Id, extract bearer key
    RC->>Rec: (panic → 500 recovery wraps everything inside)
    Rec->>Log: (logs method/path/status/duration on return)
    Log->>CORS: apply CORS headers
    CORS->>MaxBody: wrap body in http.MaxBytesReader
    MaxBody->>Auth: best-effort principal (route is intentionally open)
    Note over Auth: API key (Authorization/X-Api-Key/api-key)<br/>or mockagents_session cookie.<br/>Invalid/absent credential proceeds anonymously —<br/>this route never 401s.
    Auth->>Scope: attach Principal.TenantID to context
    Scope->>Cap: wrap ResponseWriter to capture status+body
    Cap->>Quota: (only if tenant is non-empty)
    Quota->>Quota: AllowRequest (token bucket) / CheckSpend
    alt over rate or spend cap
        Quota-->>Client: 429 + Retry-After, or 402
        Note over Cap: still captured — logging wraps<br/>outside quota, not after it
    else within limits
        Quota->>Mux: route match
        Mux->>OAI: HandleChatCompletions
        OAI->>OAI: decode + validate request
        OAI->>Eng: ProcessRequestContext(ctx, inbound)
        Eng-->>OAI: Response, or a typed chaos/strict error
        alt chaos/strict error
            OAI-->>Client: provider-shaped error
        else stream:true
            OAI->>Client: SSE stream, chunked per StreamingConfig
        else
            OAI-->>Client: 200 JSON
        end
    end
    Cap->>Worker: Submit(InteractionLog) — non-blocking
    Note over Worker: response already fully sent to Client.<br/>SQLite write + SSE broadcast to<br/>/api/v1/logs/stream happen after, in one<br/>of a small fixed pool of goroutines.
```

Two things worth calling out because they contradict a plausible-sounding
assumption:

- **Management routes are gated differently than LLM routes.** `/api/v1/*`
  routes require a valid credential plus a per-route role floor. The LLM
  routes (`/v1/chat/completions`, `/v1/messages`, `/v1/realtime`, etc.) are
  intentionally open even in multi-tenant mode — the middleware still
  resolves a principal if a valid credential is present (so tenant-scoped
  agent resolution and quota still work), but it never rejects the request
  for lacking one, because these routes carry the caller's own (ignored)
  provider API key.
- **Middleware order is auth → tenant-scope → capture → quota**, not
  "auth → quota → logging → capture" — capture wraps *outside* quota
  deliberately, so a request rejected by quota (429/402) is still logged.

## Realtime: WebSocket session with server VAD

```mermaid
sequenceDiagram
    autonumber
    participant Client
    participant Adapter as adapter.RealtimeHandler
    participant Sess as realtime.Session
    participant VAD as realtime.vad
    participant Pace as realtime.pace (Tick)
    participant Quota as CheckQuota hook
    participant Log as OnResponse hook

    Client->>Adapter: WS connect (+ optional ephemeral key)
    Adapter->>Sess: NewSession, SeedConfig from minted key
    Sess-->>Client: session.created, conversation.created

    Client->>Sess: session.update (turn_detection, voice, ...)
    Sess->>Sess: validate, applyConfig, refreshVAD
    Sess-->>Client: session.updated

    loop audio streaming
        Client->>Sess: input_audio_buffer.append (audio chunk)
        Sess->>VAD: vadAppend (energy vs threshold)
        VAD-->>Client: input_audio_buffer.speech_started
        opt a response is in flight (barge-in)
            VAD->>Pace: cancelInflight("turn_detected")
            Pace-->>Client: response.done (status: cancelled)
        end
    end
    VAD->>VAD: silence duration exceeded → end of turn
    VAD-->>Client: input_audio_buffer.speech_stopped
    VAD->>Sess: synthetic input_audio_buffer.commit
    Sess-->>Client: input_audio_buffer.committed,<br/>conversation.item.added/.done (user turn)

    alt create_response != false
        Sess->>Sess: createResponse (auto-triggered)
    else client-triggered
        Client->>Sess: response.create
    end
    Sess->>Quota: CheckQuota(tenantID)
    Sess->>Log: OnResponse(tenant, model, tokens, resp) — at generation time,<br/>before the paced ladder (a barge-in cancel does not undo it)
    Log->>Log: accrue spend + submit interaction log
    Sess-->>Client: response.created, rate_limits.updated
    Sess->>Pace: beginPacedResponse(ladder)
    loop Session.Tick, one event per pace interval
        Pace-->>Client: output_item.added / content_part.added /<br/>output_text.delta or output_audio(+transcript).delta
    end
    Pace-->>Client: response.done

    opt no activity for idle_timeout_ms
        Sess->>Sess: idle timer fires once
        Sess-->>Client: input_audio_buffer.timeout_triggered
        Sess->>Sess: [silence] placeholder turn, then createResponse
    end
```

Notes worth knowing if you're building against this:

- The VAD is a **synchronous energy-threshold detector**, not a real
  acoustic model. `semantic_vad`'s `eagerness` is approximated by mapping to
  a fixed silence window rather than any semantic understanding of speech.
- **Barge-in** (the model gets interrupted mid-response) and **client-
  initiated cancel** both funnel through the same cancellation path, with a
  different reason that changes what happens to a queued auto-response.
- **Pacing here is a flat constant-interval drain**, not the TTFT/ITL
  lognormal model used by `streaming/pacing.go` for chat/messages SSE.
  Unifying the two is an open follow-on, not a hidden feature.
- **Ephemeral keys are cosmetic.** Minting one validates the session payload
  shape, but the WebSocket itself never actually requires a valid key to
  connect. Keep that in mind if you're testing auth-failure handling.
- Quota and logging integrate through **hooks on the adapter**, not the HTTP
  middleware chain — a long-lived WebSocket never passes back through the
  same per-message capture/quota path a normal request does.

## MCP: Streamable HTTP transport

```mermaid
sequenceDiagram
    autonumber
    participant Client
    participant H as mcp.StreamableHTTPHandler
    participant Srv as mcp.Server
    participant Sessions as sessionManager

    Client->>H: POST /mcp, method=initialize
    H->>Srv: Handle(req)
    Srv-->>H: result (no error)
    H->>Sessions: create() — 128-bit hex id
    H-->>Client: 200 + header Mcp-Session-Id

    Client->>H: POST /mcp (Mcp-Session-Id, MCP-Protocol-Version)
    H->>H: validate Origin (403 if disallowed), protocol version (400),<br/>session id (400 missing / 404 unknown)
    H->>Srv: Handle(req) — dispatch
    alt request had no id (notification/response) — Handle returns nil
        H-->>Client: 202 Accepted (empty body)
    else Accept: text/event-stream
        H-->>Client: SSE - pending notifications, then the JSON-RPC<br/>response as the final message event, stream closes
    else
        H-->>Client: 200 application/json
    end

    Client->>H: GET /mcp (Accept: text/event-stream, optional Last-Event-Id)
    H->>Sessions: subscribe(after) — 409 if a subscriber already exists
    Sessions-->>Client: replay buffered events, then live events, heartbeat comments

    par out-of-band notification
        Srv->>Sessions: broadcast
        Sessions-->>Client: event: message (over the GET stream)
    end

    Client->>H: DELETE /mcp (Mcp-Session-Id)
    H->>Sessions: delete()
```

Bidirectional (server → client) requests are a **separate mechanism** from
the notification stream above: the server can mint a numeric id, enqueue an
outbound request to the session's single SSE subscriber over `GET
/mcp/events`, and block until `POST /mcp/response` delivers a matching
reply. This backs sampling (`sampling/createMessage`) and roots
(`roots/list`) requests initiated by the mock server itself — it is
unrelated to the `internal/mcpadmin` package, which instead exposes
MockAgents' own agent-management CRUD as MCP tools. stdio is line-delimited
JSON-RPC over stdin/stdout with a frame size cap; by construction it writes
nothing but JSON-RPC response/notification frames to stdout.

## Cross-cutting: chaos and strict-tools

```mermaid
flowchart LR
    subgraph yaml["Agent YAML"]
        C1["spec.chaos: {preset | latency | errors | connection}"]
        C2["spec.behavior.strict_tools: {ids, tool_choice, schemas}"]
    end
    subgraph cfgload["internal/config"]
        PRESET["chaos_presets.go<br/>expands preset → ChaosConfig,<br/>only filling nil sections"]
    end
    subgraph resolve["Knob resolution (both features)"]
        R1["1. YAML value, if set"]
        R2["2. Env var<br/>(MOCKAGENTS_STRICT_TOOLS for strict-tools;<br/>chaos has no env fallback)"]
        R3["3. Off"]
    end
    subgraph engineg["internal/engine"]
        CB["ChaosInjector.Before<br/>rate-limit → error/timeout → connection fault"]
        CA["ChaosInjector.After<br/>latency only, post-generation"]
        ST["StrictToolsFor<br/>ids → tool_choice → schemas"]
    end
    subgraph typed["Typed errors"]
        CE["*engine.ChaosError"]
        SE["*engine.StrictToolError"]
    end
    subgraph wire["internal/adapter"]
        AC["chaos.go: per-provider status/code/Retry-After body,<br/>or a connection-level fault"]
        AS["strict.go: per-provider 400 body"]
    end

    C1 --> PRESET --> R1
    C2 --> R1
    R1 --> R2 --> R3
    R3 --> CB & CA & ST
    CB --> CE
    CA --> CE
    ST -->|enforce| SE
    ST -->|warn| WARN["X-Mockagents-Strict-Violation header,<br/>request still succeeds"]
    CE --> AC
    SE --> AS
```

Chaos presets (`server-down`, `rate-limited`, `access-denied`,
`unauthorized`, `flaky`, `slow`, `connection-reset`) only fill config fields
the author left unset, so an explicit override in the YAML always wins over
the preset's defaults. `flaky` uses a fail-first-N-then-recover pattern —
useful for testing retry logic without randomness. Strict-tools has three
independently-togglable dimensions (round-trip tool IDs, `tool_choice`
enforcement, and per-function JSON-schema strictness); each can be forced
off in YAML even when the top-level knob is on.

## Data and deployment view

```mermaid
flowchart TB
    subgraph proc["mockagents process (single binary)"]
        BIN["cmd/mockagents<br/>CGO_ENABLED=0, pure-Go SQLite"]
    end
    subgraph localfiles["Local files (single-tenant default)"]
        DB1[(".mockagents.db<br/>interaction logs, WAL")]
        AGENTS["agents dir (mounted /agents)<br/>YAML: Agent/Pipeline/TestSuite/<br/>MCPServer/A2AServer"]
    end
    subgraph tenantstore["Tenancy store (multi-tenant mode)"]
        SQLITE2[(".mockagents-tenancy.db<br/>single-conn serialized")]
        PG[("Postgres<br/>MOCKAGENTS_TENANCY_DSN set")]
    end
    subgraph auditdb["Audit store"]
        DB3[("audit SQLite, WAL")]
    end

    BIN --> DB1
    BIN --> AGENTS
    BIN -->|MOCKAGENTS_TENANCY_DSN unset| SQLITE2
    BIN -->|MOCKAGENTS_TENANCY_DSN set| PG
    BIN --> DB3

    subgraph docker["Docker"]
        IMG["golang:1.26-alpine build →<br/>alpine:3.19 runtime, non-root user"]
        VOL1["/agents volume (ro)"]
        VOL2["/data volume"]
    end
    IMG -.-> BIN

    subgraph helm["Helm chart (k8s)"]
        HPA["optional HPA (autoscaling/v2)"]
        PDB["optional PodDisruptionBudget"]
        NP["optional NetworkPolicy (off by default)"]
        SM["optional ServiceMonitor<br/>(Prometheus Operator)"]
    end
    helm -.-> docker
```

Single-tenant mode (the default) needs no external services: agent YAML is
read from disk, interaction logs go to `.mockagents.db`. Multi-tenant mode
adds the tenancy store — SQLite by default, or Postgres when configured —
plus, optionally, an audit store and a quota enforcer backed by the same
tenancy store. Nothing here requires cgo; that's what keeps the Alpine
multi-stage Docker image and cross-compiled release binaries simple.

## Design rules

- **No cgo.** SQLite is `modernc.org/sqlite` so cross-compilation stays
  simple. (Side effect: `go test -race` is unavailable on all platforms.)
- **Import direction.** `tenancy` may import `engine`, never the reverse.
  This keeps the engine cycle-free and provider/tenant-agnostic.
- **One authorization chokepoint.** Every `/api/v1` management route goes
  through a single role-floor table that panics at startup on a route with
  no entry — an ungated route can't slip in.
- **The LLM/engine surface is intentionally open**, even in multi-tenant
  mode — see the [request-flow notes](#request-flow-sequence-post-v1chatcompletions)
  above. That's a different gate from the management-API role floors.
- **The agent YAML schema is authoritative** — see the
  [YAML Schema guide](../guides/yaml-schema.md) and the
  [Agent Definition reference](agent-definition.md).
