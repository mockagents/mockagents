# YAML Agent Definition Schema

Agent definitions are YAML files that configure mock agent behavior.

> This page documents `kind: Agent`. MockAgents loads four document kinds from
> `--agents-dir`, each with its own schema under
> [`schema/`](https://github.com/mockagents/mockagents/tree/main/schema):
>
> | `kind` | Purpose |
> |--------|---------|
> | `Agent` | A single mock LLM agent (this page) |
> | `Pipeline` | Multi-agent topology — `sequential`, `parallel`, or `graph` with conditional edges |
> | `TestSuite` | Declarative test cases (`tool_call`, `no_tool_call`, `response_contains`, `response_matches`, `scenario_matched`, `refusal`, `latency_ms_lt`, `tool_call_count`, `tool_call_sequence`, `node_sequence`) run by `mockagents test` |
> | `MCPServer` | A mock Model Context Protocol server (tools, resources, prompts) served by `mockagents mcp` |
>
> All four validate under `mockagents validate` and `POST /api/v1/config/validate`.

## Top-Level Structure

```yaml
apiVersion: mockagents/v1    # Required. Schema version.
kind: Agent                   # Required. Resource type.
metadata:                     # Required. Identifying information.
  name: my-agent              # Required. Kebab-case, max 63 chars.
  description: "..."          # Optional.
  tags: [tag1, tag2]          # Optional.
spec:                         # Required. Agent specification.
  protocol: openai-chat-completions  # Required.
  model: gpt-4o              # Optional. Default: "mock-agent".
  systemPrompt: "..."        # Optional. System prompt text.
  tools: [...]               # Optional. Tool definitions.
  behavior:                   # Required.
    scenarios: [...]          # Required. At least one scenario.
    streaming: {...}          # Optional. Streaming config.
    chaos: {...}              # Optional. Fault injection.
```

## `metadata`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique agent name. Lowercase kebab-case, 1-63 chars. |
| `description` | string | No | Human-readable description. |
| `tags` | string[] | No | Labels for categorization. |

## `spec.protocol`

| Value | Description |
|-------|-------------|
| `openai-chat-completions` | OpenAI Chat Completions API format |
| `anthropic-messages` | Anthropic Messages API format |
| `google-gemini` | Google Gemini `generateContent` API format |

## `spec.tools`

```yaml
tools:
  - name: lookup_order         # Required. Snake_case.
    description: "..."         # Optional.
    validate: false            # Optional. Validate params against schema.
    error_rate: 0.0            # Optional. Random error injection rate (0.0-1.0).
    parameters:                # Optional. JSON Schema for inputs.
      type: object
      properties:
        order_id: { type: string }
      required: [order_id]
    responses:                 # Optional. Match-based response rules.
      - match:                 # Match criteria (exact parameter values).
          order_id: "ORD-123"
        response:              # Success response body.
          status: shipped
      - match:
          order_id: "INVALID"
        error:                 # Error response.
          code: NOT_FOUND
          message: "Order not found"
      - default: true          # Fallback when no match.
        response:
          status: processing
```

### Tool Response Matching

- **First match wins** — rules evaluated in definition order
- **Exact match** — all specified keys must match exactly
- **Unspecified parameters ignored** — extra args in the call don't prevent matching
- **Default** — rule with `default: true` used when nothing else matches

## `spec.behavior.scenarios`

```yaml
scenarios:
  - name: greeting              # Required. Scenario identifier.
    match:                      # Optional. Omit for default scenario.
      content_contains: "hello" # Case-insensitive substring.
      content_regex: "..."      # Regex pattern. Named groups available in templates.
      turn_number: 1            # Match on conversation turn (1-indexed).
    response:                   # Required.
      content: "Hello!"         # One of content / refusal / tool_calls (supports templates).
      tool_calls:               # Optional. Tool calls to simulate.
        - name: search
          arguments: { q: "test" }
      metadata: { key: value }  # Optional. Custom metadata.
```

### Match Rules

| Rule | Description |
|------|-------------|
| `content_contains` | Case-insensitive substring match on user message |
| `content_regex` | Regex match. Named captures available as `{{ index .Match "name" }}` |
| `turn_number` | Matches specific conversation turn (1-indexed) |

All rules use **AND logic** — all specified conditions must be true.

### Template Expressions

Use Go template syntax in `response.content`:

| Function | Example | Output |
|----------|---------|--------|
| `{{ .Message }}` | | User's message |
| `{{ .TurnNumber }}` | | Current turn number |
| `{{ .SessionID }}` | | Session identifier |
| `{{ now }}` | `{{ now }}` | Current UTC time (RFC3339) |
| `{{ timestamp }}` | `{{ timestamp }}` | Unix timestamp |
| `{{ uuid }}` | `{{ uuid }}` | UUID v4 |
| `{{ random_int 1 100 }}` | `{{ random_int 1 100 }}` | Random integer |
| `{{ random_float 0.0 1.0 }}` | `{{ random_float 0.0 1.0 }}` | Random float |
| `{{ random_string 8 }}` | `{{ random_string 8 }}` | Random alphanumeric |
| `{{ random_choice "a" "b" }}` | `{{ random_choice "a" "b" }}` | Random selection |
| `{{ date_offset 3 "days" }}` | `{{ date_offset 3 "days" }}` | Date offset |
| `{{ fake_name }}` | `{{ fake_name }}` | Random person name |
| `{{ fake_email }}` | `{{ fake_email }}` | Random email |
| `{{ fake_phone }}` | `{{ fake_phone }}` | Random phone, e.g. `(415) 555-0182` |
| `{{ fake_company }}` | `{{ fake_company }}` | Random company, e.g. `Acme Inc` |
| `{{ fake_username }}` | `{{ fake_username }}` | Random username, e.g. `alice_smith42` |
| `{{ upper "text" }}` | `{{ upper "hello" }}` | HELLO |
| `{{ lower "TEXT" }}` | `{{ lower "HELLO" }}` | hello |
| `{{ title "text" }}` | `{{ title "hello world" }}` | Hello World |
| `{{ to_json .Agent.Metadata }}` | | JSON-encoded value |

## `spec.behavior.streaming`

```yaml
streaming:
  enabled: true       # Default: false
  chunk_size: 4       # Words per SSE chunk. Default: 4.
  chunk_delay_ms: 50  # Milliseconds between chunks. Default: 50.

  # Stream-timing physics + mid-stream fault injection (optional):
  ttft_ms: 200             # Time-to-first-token: delay before the first chunk.
  tokens_per_sec: 20       # Pace chunks at this word rate (overrides chunk_delay_ms).
  jitter_ms: 30            # Deterministic +/- jitter (ms) per inter-chunk delay.
  truncate_after_chunks: 3 # End the stream after N chunks, no finish frame / [DONE].
  malformed: true          # Emit one invalid-JSON SSE frame, then end (parser fault).
```

## `spec.behavior.chaos`

```yaml
chaos:
  latency:
    min_ms: 100
    max_ms: 500
  errors:
    rate: 0.1          # 10% of requests return errors
    status_code: 500
```

Injected errors are rendered in each provider's **own** error envelope, so a
client SDK reacts as it would against the real API:

| status | OpenAI (`error.type` / `code`) | Anthropic (`error.type`) | Gemini (`error.status`) |
|---|---|---|---|
| 401 | `invalid_request_error` / `invalid_api_key` | `authentication_error` | `UNAUTHENTICATED` |
| 403 | `invalid_request_error` | `permission_error` | `PERMISSION_DENIED` |
| 429 | `requests` / `rate_limit_exceeded` | `rate_limit_error` | `RESOURCE_EXHAUSTED` |
| 503 | `server_error` | `overloaded_error` | `UNAVAILABLE` |

Any injected **429 carries a `Retry-After` header** (the configured rate-limit
hint, or a 1s default) so retry/backoff code has something to read.

### Named presets

`chaos.preset` is a one-line shorthand for a common failure mode — it expands at
load time into the concrete sub-sections, and any sub-section you set explicitly
overrides the preset:

```yaml
chaos:
  preset: rate-limited   # every request -> 429 + Retry-After
```

| preset | effect |
|---|---|
| `server-down` | every request → 503 (overloaded) |
| `rate-limited` | every request → 429 (+ `Retry-After`) |
| `access-denied` | every request → 403 (permission denied) |
| `unauthorized` | every request → 401 (bad credentials) |
| `flaky` | fail the first 2 requests (503), then recover |
| `slow` | add 2–5s of latency to every response |
| `connection-reset` | every request resets the TCP connection (RST) |

See [`examples/access-denied-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/access-denied-agent.yaml).

### Stateful "flaky then healthy" trigger (`fail_first`)

For testing client **retry / backoff / circuit-breaker** logic, fail the first N
requests deterministically and then recover — instead of a random `rate`:

```yaml
chaos:
  errors:
    fail_first: 2      # requests 1 and 2 fail; request 3+ succeeds
    status_code: 503   # overloaded_error — the #1 production retry case
    message: "temporarily overloaded, please retry"
```

`fail_first` takes precedence over `rate` (the first N always fail), composes
with `timeout: true` (the first N time out, then recover), and the per-agent
counter resets when the server restarts. See
[`examples/flaky-then-healthy-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/flaky-then-healthy-agent.yaml).

### Connection-layer faults (`chaos.connection`)

HTTP-status errors return a well-formed error *response*. To exercise client
**transport-error** handling — the cases an error body can't reach — fault the
TCP connection itself, before any HTTP response is written:

```yaml
chaos:
  connection:
    mode: reset      # reset | empty | random
    rate: 1.0        # or fail_first: N
```

| `mode` | What the client sees | Simulates |
|---|---|---|
| `reset` (alias `peer-reset`) | "connection reset by peer" (TCP RST) | a server/LB dropping the connection |
| `empty` | empty reply / unexpected EOF | a half-open connection closed with no response |
| `random` (aliases `random-then-close`, `garbage`) | a malformed/unparseable response | a corrupt proxy or protocol downgrade |

Triggered by `rate` or `fail_first` exactly like `chaos.errors`. The server
hijacks the connection to deliver the fault; over HTTP/2 (where hijacking isn't
available) it falls back to a `502`. The `connection-reset` preset is shorthand
for `{mode: reset, rate: 1.0}`. See
[`examples/connection-fault-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/connection-fault-agent.yaml).

## Semantic error modes

Beyond HTTP/chaos faults, a **scenario response** can plant a *well-formed but
wrong* output — where real agent code actually breaks (FB-03):

```yaml
scenarios:
  - name: truncated
    match: { content_contains: "summary" }
    response:
      content: "The answer got cut o"
      finish_reason: length            # OpenAI finish_reason / Anthropic max_tokens / Gemini MAX_TOKENS

  - name: refusal
    match: { content_contains: "hack" }
    response:
      refusal: "I can't help with that."   # OpenAI message.refusal; refusal-as-content elsewhere

  - name: bad-tool-args
    match: { content_contains: "weather" }
    response:
      content: "Looking that up."
      tool_calls:
        - name: get_weather
          raw_arguments: '{"city":'     # emitted verbatim — malformed JSON to break your parser
```

- `finish_reason` — override the emitted finish/stop reason; use `length` to
  simulate a truncated response, `content_filter` for a filtered one.
- `refusal` — emit an assistant refusal instead of content.
- `raw_arguments` — emit a tool call's arguments string verbatim (malformed or
  schema-violating), instead of marshaling `arguments`. OpenAI only.

A response is valid with **any** of `content`, `refusal`, or `tool_calls`. See
[`examples/semantic-errors-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/semantic-errors-agent.yaml).

## JSON Schema

The full JSON Schema for agent definitions is available at [`schema/mockagents-v1-agent.json`](https://github.com/mockagents/mockagents/blob/main/schema/mockagents-v1-agent.json).
