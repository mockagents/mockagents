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
> | `TestSuite` | Declarative test cases (`tool_call`, `response_contains`, `scenario_matched`, `latency_ms_lt`) run by `mockagents test` |
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
      content: "Hello!"         # Required. Response text (supports templates).
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
  chunk_size: 4       # Tokens per SSE chunk. Default: 4.
  chunk_delay_ms: 50  # Milliseconds between chunks. Default: 50.
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

## JSON Schema

The full JSON Schema for agent definitions is available at [`schema/mockagents-v1-agent.json`](https://github.com/mockagents/mockagents/blob/main/schema/mockagents-v1-agent.json).
