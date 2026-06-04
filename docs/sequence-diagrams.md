# MockAgents — Sequence Diagrams

This document contains Mermaid sequence diagrams for the major flows in the
MockAgents platform — a Go core engine with OpenAI/Anthropic adapters, a mock
MCP server, a multi-tenant control plane, and three language SDKs. Flows 1–11
cover the foundation (CLI, LLM request/response, tools, SDK lifecycle,
management API); flows 12–15 cover the control-plane and real-time surfaces
added in v0.2/v0.3.

---

## 1. CLI: `mockagents init`

Scaffolds a new MockAgents project directory with the default folder structure and configuration file.

```mermaid
sequenceDiagram
    actor User
    participant CLI as CLI (mockagents)
    participant FS as Filesystem

    User->>CLI: mockagents init [project-name]
    CLI->>CLI: Parse flags & resolve project path
    CLI->>FS: Check if directory already exists
    alt Directory already exists
        FS-->>CLI: Exists
        CLI-->>User: Error: directory already exists
    else Directory does not exist
        FS-->>CLI: Not found
        CLI->>FS: Create project root directory
        CLI->>FS: Create agents/ directory
        CLI->>FS: Create tests/ directory
        CLI->>FS: Write mockagents.yaml (default config)
        CLI->>FS: Write agents/example-agent.yaml (sample agent def)
        CLI->>FS: Write tests/example-test.yaml (sample test)
        FS-->>CLI: All files written
        CLI-->>User: Success: project scaffolded at ./project-name
        CLI-->>User: Print next-steps guide
    end
```

---

## 2. CLI: `mockagents start`

Loads the project configuration, validates all agent definitions, starts the HTTP mock server with protocol adapter routes, and prints the server URL.

```mermaid
sequenceDiagram
    actor User
    participant CLI as CLI (mockagents)
    participant Config as ConfigLoader
    participant Validator as AgentValidator
    participant Server as HTTP Server
    participant OpenAI as OpenAI Adapter
    participant Anthropic as Anthropic Adapter
    participant Mgmt as Management API

    User->>CLI: mockagents start [--port 8080]
    CLI->>Config: Load mockagents.yaml
    Config->>Config: Parse YAML, resolve paths
    alt Config file missing or invalid
        Config-->>CLI: Error details
        CLI-->>User: Error: invalid configuration
    else Config valid
        Config-->>CLI: Config object
        CLI->>Validator: Validate all agent definitions in agents/
        loop For each agent YAML file
            Validator->>Validator: Parse YAML
            Validator->>Validator: Validate against JSON Schema
        end
        alt Validation errors found
            Validator-->>CLI: List of errors
            CLI-->>User: Error: agent validation failed (details)
        else All agents valid
            Validator-->>CLI: Agent definitions loaded
            CLI->>Server: Initialize HTTP server on configured port
            Server->>OpenAI: Register POST /v1/chat/completions
            Server->>Anthropic: Register POST /v1/messages
            Server->>Mgmt: Register GET /api/v1/agents
            Server->>Mgmt: Register GET /api/v1/health
            Server->>Mgmt: Register GET /api/v1/logs
            Server-->>CLI: Server started
            CLI-->>User: Mock server running at http://localhost:8080
            CLI-->>User: Print registered routes summary
        end
    end
```

---

## 3. CLI: `mockagents validate`

Loads all agent YAML files from the project and validates them against the expected JSON Schema, reporting any errors or confirming success.

```mermaid
sequenceDiagram
    actor User
    participant CLI as CLI (mockagents)
    participant FS as Filesystem
    participant Validator as SchemaValidator

    User->>CLI: mockagents validate [--dir agents/]
    CLI->>FS: Scan directory for *.yaml / *.yml files
    FS-->>CLI: List of agent definition files

    alt No files found
        CLI-->>User: Warning: no agent files found
    else Files found
        loop For each agent file
            CLI->>FS: Read file contents
            FS-->>CLI: Raw YAML
            CLI->>Validator: Parse YAML
            alt YAML parse error
                Validator-->>CLI: Syntax error (file, line, message)
            else YAML valid
                Validator->>Validator: Validate against agent JSON Schema
                alt Schema validation fails
                    Validator-->>CLI: Schema errors (field, constraint, message)
                else Schema valid
                    Validator-->>CLI: OK
                end
            end
        end
        CLI->>CLI: Aggregate results
        alt Any errors present
            CLI-->>User: Validation FAILED — list errors per file
        else All files valid
            CLI-->>User: Validation PASSED — N agent(s) valid
        end
    end
```

---

## 4. OpenAI Non-Streaming Request

A client application sends a standard (non-streaming) chat completion request. The mock engine matches a scenario, generates a response, optionally processes tool calls, and returns the OpenAI-format JSON.

```mermaid
sequenceDiagram
    participant Client as Client App
    participant Server as HTTP Server
    participant Adapter as OpenAI Adapter
    participant Engine as Mock Engine
    participant State as State Store
    participant Matcher as ScenarioMatcher
    participant RespGen as ResponseGenerator
    participant ToolProc as ToolCallProcessor
    participant Logger as InteractionLogger

    Client->>Server: POST /v1/chat/completions<br/>(stream: false)
    Server->>Adapter: Route to OpenAI handler
    Adapter->>Adapter: Parse request (model, messages, tools)
    Adapter->>Engine: ProcessRequest(parsed request)
    Engine->>Engine: Identify agent from model field
    Engine->>State: GetOrCreateSession(agent, conversation_id)
    State-->>Engine: Session (with history)
    Engine->>Matcher: Match(messages, agent scenarios)
    Matcher->>Matcher: Evaluate pattern rules & conditions
    Matcher-->>Engine: Matched scenario
    Engine->>RespGen: Generate(scenario, session)
    RespGen-->>Engine: Raw response (content + optional tool_calls)

    opt Tool calls present in response
        Engine->>ToolProc: ResolveToolCalls(tool_calls, agent tool defs)
        ToolProc->>ToolProc: Build tool_call objects with IDs & arguments
        ToolProc-->>Engine: Resolved tool_calls
    end

    Engine->>State: AppendInteraction(session, request, response)
    Engine-->>Adapter: EngineResponse
    Adapter->>Adapter: Format as OpenAI ChatCompletion JSON<br/>(id, object, model, choices, usage)
    Adapter->>Logger: Log(request, response, latency)
    Logger->>Logger: Write to SQLite
    Adapter-->>Server: HTTP 200 + JSON body
    Server-->>Client: OpenAI-compatible JSON response
```

---

## 5. OpenAI Streaming Request

Same processing pipeline as non-streaming, but the response is delivered as Server-Sent Events (SSE) with chunked deltas.

```mermaid
sequenceDiagram
    participant Client as Client App
    participant Server as HTTP Server
    participant Adapter as OpenAI Adapter
    participant Engine as Mock Engine
    participant State as State Store
    participant Matcher as ScenarioMatcher
    participant RespGen as ResponseGenerator
    participant Logger as InteractionLogger

    Client->>Server: POST /v1/chat/completions<br/>(stream: true)
    Server->>Adapter: Route to OpenAI handler
    Adapter->>Adapter: Parse request (model, messages, tools)
    Adapter->>Engine: ProcessRequest(parsed request)
    Engine->>State: GetOrCreateSession(agent, conversation_id)
    State-->>Engine: Session
    Engine->>Matcher: Match(messages, agent scenarios)
    Matcher-->>Engine: Matched scenario
    Engine->>RespGen: Generate(scenario, session)
    RespGen-->>Engine: Full response content
    Engine->>State: AppendInteraction(session, request, response)
    Engine-->>Adapter: EngineResponse

    Adapter->>Client: HTTP 200 (Content-Type: text/event-stream)

    Adapter->>Adapter: Tokenize response into chunks

    opt Tool calls present
        Adapter->>Adapter: Prepare tool_call delta chunks
    end

    loop For each content chunk
        Adapter->>Client: data: {"choices":[{"delta":{"content":"token"}}]}
        Adapter->>Adapter: Apply simulated latency between chunks
    end

    opt Tool call chunks
        loop For each tool_call delta
            Adapter->>Client: data: {"choices":[{"delta":{"tool_calls":[...]}}]}
        end
    end

    Adapter->>Client: data: [DONE]
    Adapter->>Logger: Log(request, full response, latency)
    Logger->>Logger: Write to SQLite
    Note over Client,Adapter: SSE connection closed
```

---

## 6. Anthropic Non-Streaming Request

A client sends a Messages API request. The Anthropic adapter translates between the Anthropic wire format (content blocks, tool_use blocks) and the internal engine representation.

```mermaid
sequenceDiagram
    participant Client as Client App
    participant Server as HTTP Server
    participant Adapter as Anthropic Adapter
    participant Engine as Mock Engine
    participant State as State Store
    participant Matcher as ScenarioMatcher
    participant RespGen as ResponseGenerator
    participant ToolProc as ToolCallProcessor
    participant Logger as InteractionLogger

    Client->>Server: POST /v1/messages<br/>(stream: false, x-api-key header)
    Server->>Adapter: Route to Anthropic handler
    Adapter->>Adapter: Parse request (model, messages, system, tools)
    Adapter->>Adapter: Normalize Anthropic content blocks to internal format
    Adapter->>Engine: ProcessRequest(parsed request)
    Engine->>State: GetOrCreateSession(agent, conversation_id)
    State-->>Engine: Session
    Engine->>Matcher: Match(messages, agent scenarios)
    Matcher-->>Engine: Matched scenario
    Engine->>RespGen: Generate(scenario, session)
    RespGen-->>Engine: Raw response

    opt Tool use in response
        Engine->>ToolProc: ResolveToolCalls(tool_calls, tool defs)
        ToolProc-->>Engine: Resolved tool_use blocks
    end

    Engine->>State: AppendInteraction(session, request, response)
    Engine-->>Adapter: EngineResponse

    Adapter->>Adapter: Format as Anthropic Messages response
    Note over Adapter: Build content array with<br/>text blocks and/or tool_use blocks
    Adapter->>Adapter: Set stop_reason (end_turn | tool_use)
    Adapter->>Adapter: Compute usage (input_tokens, output_tokens)
    Adapter->>Logger: Log(request, response, latency)
    Logger->>Logger: Write to SQLite
    Adapter-->>Server: HTTP 200 + JSON body
    Server-->>Client: Anthropic-compatible JSON response
```

---

## 7. Anthropic Streaming Request

The Anthropic streaming protocol uses distinct SSE event types to delimit message structure, content blocks, and deltas.

```mermaid
sequenceDiagram
    participant Client as Client App
    participant Server as HTTP Server
    participant Adapter as Anthropic Adapter
    participant Engine as Mock Engine
    participant State as State Store
    participant Matcher as ScenarioMatcher
    participant RespGen as ResponseGenerator
    participant Logger as InteractionLogger

    Client->>Server: POST /v1/messages<br/>(stream: true)
    Server->>Adapter: Route to Anthropic handler
    Adapter->>Adapter: Parse request
    Adapter->>Engine: ProcessRequest(parsed request)
    Engine->>State: GetOrCreateSession(agent, conversation_id)
    State-->>Engine: Session
    Engine->>Matcher: Match(messages, agent scenarios)
    Matcher-->>Engine: Matched scenario
    Engine->>RespGen: Generate(scenario, session)
    RespGen-->>Engine: Full response
    Engine->>State: AppendInteraction(session, request, response)
    Engine-->>Adapter: EngineResponse

    Adapter->>Client: HTTP 200 (Content-Type: text/event-stream)

    Adapter->>Client: event: message_start<br/>data: {"type":"message_start","message":{...}}

    loop For each content block (text or tool_use)
        Adapter->>Client: event: content_block_start<br/>data: {"type":"content_block_start","index":N,"content_block":{...}}

        alt Text block
            loop For each text chunk
                Adapter->>Client: event: content_block_delta<br/>data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"token"}}
                Adapter->>Adapter: Apply simulated inter-token latency
            end
        else Tool use block
            Adapter->>Client: event: content_block_delta<br/>data: {"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"..."}}
        end

        Adapter->>Client: event: content_block_stop<br/>data: {"type":"content_block_stop","index":N}
    end

    Adapter->>Client: event: message_delta<br/>data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{...}}

    Adapter->>Client: event: message_stop<br/>data: {"type":"message_stop"}

    Adapter->>Logger: Log(request, full response, latency)
    Logger->>Logger: Write to SQLite
    Note over Client,Adapter: SSE connection closed
```

---

## 8. Tool Call Flow (Detailed)

A multi-turn conversation where the mock engine generates a tool call response, the client sends back tool results, and the engine matches a follow-up scenario to produce the final answer.

```mermaid
sequenceDiagram
    participant Client as Client App
    participant Server as HTTP Server
    participant Adapter as Protocol Adapter
    participant Engine as Mock Engine
    participant State as State Store
    participant Matcher as ScenarioMatcher
    participant RespGen as ResponseGenerator
    participant ToolProc as ToolCallProcessor

    Note over Client,ToolProc: Turn 1 — Client sends initial request

    Client->>Server: POST request with user message<br/>("What's the weather in NYC?")
    Server->>Adapter: Route to appropriate adapter
    Adapter->>Engine: ProcessRequest(messages)
    Engine->>State: GetOrCreateSession(agent, conversation_id)
    State-->>Engine: Session (new or existing)
    Engine->>Matcher: Match(messages, scenarios)
    Matcher->>Matcher: Pattern matches tool-call trigger scenario
    Matcher-->>Engine: Scenario: "weather_tool_call"
    Engine->>RespGen: Generate(scenario, session)
    RespGen-->>Engine: Response with tool_calls
    Engine->>ToolProc: ResolveToolCalls(tool_calls, tool defs)
    ToolProc->>ToolProc: Build tool_call: get_weather(location="NYC")
    ToolProc-->>Engine: tool_call with ID "call_abc123"
    Engine->>State: Append assistant message with tool_calls
    Engine-->>Adapter: EngineResponse (stop_reason: tool_use)
    Adapter-->>Client: Response with tool_calls / tool_use block

    Note over Client,ToolProc: Turn 2 — Client sends tool results

    Client->>Server: POST request with tool result message<br/>(tool_call_id: "call_abc123", content: '{"temp":72,"condition":"sunny"}')
    Server->>Adapter: Route to adapter
    Adapter->>Engine: ProcessRequest(messages including tool result)
    Engine->>State: GetSession(conversation_id)
    State-->>Engine: Session (with tool_call history)
    Engine->>Matcher: Match(full conversation, scenarios)
    Matcher->>Matcher: Match on tool result presence + tool name
    Matcher-->>Engine: Scenario: "weather_final_response"
    Engine->>RespGen: Generate(scenario, session)
    RespGen->>RespGen: Interpolate tool result into response template
    RespGen-->>Engine: "It's currently 72°F and sunny in NYC."
    Engine->>State: Append assistant message
    Engine-->>Adapter: EngineResponse (stop_reason: end_turn)
    Adapter-->>Client: Final response with text content
```

---

## 9. Python SDK: MockAgentServer Lifecycle

Test code uses the Python SDK context manager to start the Go mock server binary as a subprocess, run test scenarios, and tear down cleanly.

```mermaid
sequenceDiagram
    actor TestCode as Test Code (Python)
    participant SDK as MockAgentServer (SDK)
    participant Proc as Go Binary (subprocess)
    participant Health as /api/v1/health
    participant MockAPI as Mock API Endpoints

    TestCode->>SDK: with MockAgentServer.from_config("mockagents.yaml") as server:
    SDK->>SDK: Load and parse config file
    SDK->>SDK: Resolve Go binary path
    SDK->>Proc: Start subprocess (mockagents start --port <random>)
    Proc->>Proc: Load config, validate agents, bind port

    loop Health check polling (with timeout)
        SDK->>Health: GET /api/v1/health
        alt Server not ready
            Health-->>SDK: Connection refused / error
            SDK->>SDK: Wait 100ms, retry
        else Server ready
            Health-->>SDK: 200 OK {"status": "healthy"}
        end
    end

    alt Health check timeout exceeded
        SDK->>Proc: Kill subprocess
        SDK-->>TestCode: Raise TimeoutError
    else Server healthy
        SDK-->>TestCode: Yield server (base_url, port, helpers)

        Note over TestCode,MockAPI: Test execution phase

        loop Test scenarios
            TestCode->>MockAPI: HTTP requests (e.g., POST /v1/chat/completions)
            MockAPI-->>TestCode: Mock responses
            TestCode->>TestCode: Assert response matches expectations
        end

        opt Retrieve interaction logs for assertions
            TestCode->>MockAPI: GET /api/v1/logs?agent=my-agent
            MockAPI-->>TestCode: Interaction log entries
            TestCode->>TestCode: Assert on logged interactions
        end

        Note over TestCode,MockAPI: Context manager exit (__exit__)

        TestCode->>SDK: Exit context manager
        SDK->>Proc: Send SIGTERM / terminate()
        SDK->>Proc: Wait for graceful shutdown (timeout)
        alt Process exits cleanly
            Proc-->>SDK: Exit code 0
        else Graceful shutdown timeout
            SDK->>Proc: Send SIGKILL / kill()
            Proc-->>SDK: Terminated
        end
        SDK->>SDK: Clean up temp files
    end
```

---

## 10. Management API: List Agents

A simple request to the management API that returns all currently loaded agent definitions.

```mermaid
sequenceDiagram
    participant Client as API Client
    participant Server as HTTP Server
    participant Handler as AgentsHandler
    participant Registry as Agent Registry

    Client->>Server: GET /api/v1/agents
    Server->>Handler: Route to list agents handler
    Handler->>Registry: GetAllAgents()
    Registry->>Registry: Read from in-memory agent map
    Registry-->>Handler: []AgentDefinition

    Handler->>Handler: Build JSON response array
    Note over Handler: For each agent: id, name,<br/>protocol, scenario count, status

    Handler-->>Server: HTTP 200 + JSON body
    Server-->>Client: {"agents": [{"id":"...","name":"...","protocol":"openai",...}, ...]}
```

---

## 11. Management API: Get Interaction Logs

Query recorded interaction logs from the SQLite store, optionally filtered by agent name, with pagination support.

```mermaid
sequenceDiagram
    participant Client as API Client
    participant Server as HTTP Server
    participant Handler as LogsHandler
    participant DB as SQLite (interactions)

    Client->>Server: GET /api/v1/logs?agent=my-agent&limit=50&offset=0
    Server->>Handler: Route to logs handler
    Handler->>Handler: Parse query parameters
    Handler->>Handler: Validate parameters

    alt Invalid parameters
        Handler-->>Server: HTTP 400 + error details
        Server-->>Client: {"error": "invalid parameter ..."}
    else Parameters valid
        Handler->>DB: SELECT * FROM interactions<br/>WHERE agent_name = ?<br/>ORDER BY timestamp DESC<br/>LIMIT ? OFFSET ?
        DB-->>Handler: Rows

        Handler->>Handler: Marshal rows into JSON response
        Note over Handler: Each log entry includes:<br/>timestamp, agent, request summary,<br/>response summary, latency_ms,<br/>matched_scenario, protocol

        Handler-->>Server: HTTP 200 + JSON body
        Server-->>Client: {"logs": [...], "total": N, "limit": 50, "offset": 0}
    end
```

---

## 12. Multi-tenant Auth + RBAC (management route)

How an authenticated management request resolves its principal, derives a tenant
scope, and passes the role floor — with a bcrypt-skipping auth cache and an
audit `auth.denied` event on rejection.

```mermaid
sequenceDiagram
    participant Client as API Client
    participant MW as Auth Middleware
    participant Cache as Auth Cache (TTL)
    participant Store as Tenancy Store (SQLite)
    participant Authz as Route Authz (role floors)
    participant Audit as Audit Recorder

    Client->>MW: Request + Authorization: Bearer mak_...
    MW->>Cache: Lookup hashed key
    alt Cache hit
        Cache-->>MW: Principal (tenant, key id, role)
    else Cache miss
        MW->>Store: Resolve key (bcrypt compare)
        Store-->>MW: Principal or not-found
        MW->>Cache: Store principal (TTL)
    end
    alt Auth fails (bad/unknown key, store error → fail closed)
        MW->>Audit: Record auth.denied (tenant, ip)
        MW-->>Client: 401 Unauthorized
    else Authenticated
        MW->>Authz: Check role >= route floor
        alt Role too low
            Authz->>Audit: Record auth.denied
            Authz-->>Client: 403 Forbidden
        else Authorized
            Authz->>Authz: Attach tenant scope to context
            Authz-->>Client: Proceed to handler (tenant-scoped)
        end
    end
```

---

## 13. MCP Bidirectional — Server-initiated Sampling

A server-initiated `sampling/createMessage` request flows to a subscribed client
over SSE; the client POSTs its reply, which is routed back to the in-process
caller via `DeliverResponse`.

```mermaid
sequenceDiagram
    participant Trigger as Admin / Test (POST /mcp/sample)
    participant Server as MCP Server
    participant Events as SSE /mcp/events
    participant Client as MCP Client
    participant Resp as POST /mcp/response

    Client->>Events: GET /mcp/events (subscribe)
    Events-->>Client: open SSE stream
    Trigger->>Server: POST /mcp/sample {messages}
    Server->>Server: Server.SendRequest (assign id, park caller)
    Server->>Events: enqueue sampling/createMessage (id)
    Events-->>Client: event: request (JSON-RPC)
    Client->>Client: produce sampled message
    Client->>Resp: POST /mcp/response {id, result}
    Resp->>Server: DeliverResponse(id, result)
    Server-->>Trigger: 200 + sampled result
```

---

## 14. Real-time Log Feed (SSE)

The GUI live feed subscribes to the broadcaster; each new interaction written by
the async log worker is fanned out to all subscribers sub-second.

```mermaid
sequenceDiagram
    participant GUI as Web Console (/logs?live=1)
    participant Proxy as GUI SSE proxy (same-origin)
    participant Stream as GET /api/v1/logs/stream
    participant BC as Log Broadcaster
    participant Worker as Async Log Worker
    participant DB as Interactions DB

    GUI->>Proxy: open EventSource
    Proxy->>Stream: GET (Bearer from HttpOnly cookie)
    Stream->>BC: SubscribeTenant(scope)
    BC-->>Stream: subscription (bounded buffer)
    Note over Worker,DB: a client LLM request completes
    Worker->>DB: INSERT interaction (LastInsertId)
    Worker->>BC: publish row (tenant-scoped)
    BC-->>Stream: row (or event: dropped on overflow)
    Stream-->>Proxy: SSE frame
    Proxy-->>GUI: append row / sticky drop badge
```

---

## 15. API-Key Rotation (in place)

`POST /api/v1/keys/{id}/rotate` regenerates a secret transactionally, preserving
id/name/role/tenant, flushing the auth cache, and emitting an audit event with
both prefixes.

```mermaid
sequenceDiagram
    participant Admin as Admin (Bearer)
    participant Handler as Rotate Handler
    participant Store as Tenancy Store
    participant Cache as Auth Cache
    participant Audit as Audit Recorder

    Admin->>Handler: POST /api/v1/keys/{id}/rotate
    Handler->>Handler: ensureOwnTenant(principal, key)
    Handler->>Store: RotateAPIKey(tenantID, keyID) [tx]
    Store->>Store: generate secret, bcrypt, UPDATE hash
    Store-->>Handler: {key meta, new plaintext, old/new prefix}
    Handler->>Cache: Flush(keyID)
    Handler->>Audit: Record api_key.rotated (old+new prefix)
    Handler-->>Admin: 200 + plaintext (shown once)
```
