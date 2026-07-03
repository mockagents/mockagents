# Agent Definition Reference

Complete reference for all fields in a MockAgents agent definition YAML file
(`kind: Agent`). The authoritative machine-readable schema is
[`schema/mockagents-v1-agent.json`](https://github.com/mockagents/mockagents/blob/main/schema/mockagents-v1-agent.json).

See the [YAML Schema Guide](../guides/yaml-schema.md) for practical usage
examples.

## Required Fields

| Path | Type | Description |
|------|------|-------------|
| `apiVersion` | string | Must be `mockagents/v1` |
| `kind` | string | Must be `Agent` |
| `metadata.name` | string | Kebab-case, 1-63 characters |
| `spec.protocol` | string | `openai-chat-completions`, `anthropic-messages`, or `google-gemini` |
| `spec.behavior.scenarios` | array | At least one scenario required |

## Full Field Reference

| Path | Type | Default | Description |
|------|------|---------|-------------|
| `metadata.description` | string | `""` | Human-readable description |
| `metadata.tags` | string[] | `[]` | Categorization labels |
| `spec.model` | string | `mock-agent` | Model name matched/reported in API responses |
| `spec.systemPrompt` | string | `""` | System prompt text |
| `spec.tools[].name` | string | | Snake_case tool name (unique per agent) |
| `spec.tools[].description` | string | `""` | Tool description |
| `spec.tools[].parameters` | object | | JSON Schema for tool inputs |
| `spec.tools[].responses` | array | | Match-based response rules (consumed by the test runner and MCP; see the [YAML guide](../guides/yaml-schema.md#spectools)) |
| `spec.tools[].validate` | boolean | `false` | Validate parameters against schema |
| `spec.tools[].error_rate` | float | `0.0` | Random error injection rate (0.0-1.0) |

### Scenarios

| Path | Type | Default | Description |
|------|------|---------|-------------|
| `scenarios[].name` | string | | Scenario identifier (unique per agent) |
| `scenarios[].match` | object | | Match conditions (omit for default scenario) |
| `scenarios[].match.content_contains` | string | | Case-insensitive substring match |
| `scenarios[].match.content_regex` | string | | Regex pattern match |
| `scenarios[].match.turn_number` | int | | Conversation turn (1-indexed) |
| `scenarios[].match.has_image` | boolean | | Match when the latest user turn has (or lacks) an image part |
| `scenarios[].response.content` | string | | Response text (supports Go templates). At least one of content / refusal / tool_calls is required |
| `scenarios[].response.refusal` | string | | Emit an assistant refusal instead of content |
| `scenarios[].response.finish_reason` | string | | Override the finish/stop reason (e.g. `length`, `content_filter`) |
| `scenarios[].response.tool_calls` | array | | Tool calls to simulate (`name`, `arguments`) |
| `scenarios[].response.tool_calls[].raw_arguments` | string | | Emit verbatim as the tool-call arguments (e.g. malformed JSON). OpenAI only |
| `scenarios[].response.hallucination` | object | | Mark the response as a planted hallucination fixture (`type`, `ground_truth`, `note`) — see [Hallucination Testing](../guides/hallucination-testing.md) |
| `scenarios[].response.metadata` | object | | Custom key-value metadata |

Paths above are relative to `spec.behavior.`.

### Streaming

| Path | Type | Default | Description |
|------|------|---------|-------------|
| `streaming.enabled` | boolean | `false` | Enable SSE streaming |
| `streaming.chunk_size` | int | `4` | Words per SSE chunk |
| `streaming.chunk_delay_ms` | int | `50` | Delay between chunks (ms) |
| `streaming.ttft_ms` | int | | Time-to-first-token delay |
| `streaming.tokens_per_sec` | number | | Paced output rate (overrides `chunk_delay_ms`) |
| `streaming.jitter_ms` | int | | Deterministic ± jitter per inter-chunk delay |
| `streaming.ttft_p50_ms` / `ttft_p95_ms` | int | | TTFT percentiles — lognormal sampling ([load testing](../guides/load-testing.md)) |
| `streaming.itl_p50_ms` / `itl_p95_ms` | int | | Inter-token latency percentiles |
| `streaming.truncate_after_chunks` | int | | End the stream after N chunks, no finish frame / `[DONE]` |
| `streaming.malformed` | boolean | `false` | Emit one invalid-JSON SSE frame, then end |

### Chaos

| Path | Type | Description |
|------|------|-------------|
| `chaos.enabled` | boolean | Defaults to `true` when any sub-section is present |
| `chaos.preset` | string | `server-down`, `rate-limited`, `access-denied`, `unauthorized`, `flaky`, `slow`, `connection-reset` |
| `chaos.latency` | object | `distribution` (`fixed`/`uniform`/`normal`), `min_ms`, `max_ms`, `mean_ms`, `stddev_ms` |
| `chaos.errors` | object | `rate`, `status_code`, `status_codes`, `message`, `timeout`, `timeout_ms`, `fail_first` |
| `chaos.rate_limit` | object | `requests` per `window_ms` (rolling window, 429 + `Retry-After`) |
| `chaos.connection` | object | `mode` (`reset`/`empty`/`random`), `rate`, `fail_first` — TCP-level faults |

See the [Chaos guide](../guides/chaos.md).

### Strict tools

| Path | Type | Description |
|------|------|-------------|
| `strict_tools.level` | string | `off` (default) / `warn` / `strict` |
| `strict_tools.ids` | boolean | Round-trip tool-id validation |
| `strict_tools.tool_choice` | boolean | `required`/named forcing + parallel-call cap |
| `strict_tools.schemas` | boolean | `strict: true` function-schema validation |

Fleet default via `MOCKAGENTS_STRICT_TOOLS`; the agent block overrides it. See
the [Strict Tools guide](../guides/strict-tools.md).

## Validation Rules

- `metadata.name`: Must match `^[a-z0-9]+(-[a-z0-9]+)*$`, max 63 chars
- `spec.tools[].name`: Must match `^[a-z][a-z0-9_]*$`, unique within agent
- `spec.behavior.scenarios`: At least one required; names must be unique
- `match.content_contains` and `match.content_regex` are mutually exclusive
- Tool parameter schemas must include a `type` field
- Scenario `response.tool_calls` must reference defined tools
- A response needs at least one of `content`, `refusal`, or `tool_calls`

`mockagents validate` applies these rules (plus non-fatal lint warnings —
upgrade them to errors with `--strict`); `POST /api/v1/config/validate` runs
the same validator server-side.
