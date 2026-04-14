# Agent Definition Reference

Complete reference for all fields in a MockAgents agent definition YAML file.

See [YAML Schema Guide](../guides/yaml-schema.md) for practical usage examples.

## Required Fields

| Path | Type | Description |
|------|------|-------------|
| `apiVersion` | string | Must be `mockagents/v1` |
| `kind` | string | Must be `Agent` |
| `metadata.name` | string | Kebab-case, 1-63 characters |
| `spec.protocol` | string | `openai-chat-completions` or `anthropic-messages` |
| `spec.behavior.scenarios` | array | At least one scenario required |

## Full Field Reference

| Path | Type | Default | Description |
|------|------|---------|-------------|
| `metadata.description` | string | `""` | Human-readable description |
| `metadata.tags` | string[] | `[]` | Categorization labels |
| `spec.model` | string | `mock-agent` | Model name in API responses |
| `spec.systemPrompt` | string | `""` | System prompt text |
| `spec.tools[].name` | string | | Snake_case tool name (unique per agent) |
| `spec.tools[].description` | string | `""` | Tool description |
| `spec.tools[].parameters` | object | | JSON Schema for tool inputs |
| `spec.tools[].responses` | array | | Match-based response rules |
| `spec.tools[].validate` | boolean | `false` | Validate parameters against schema |
| `spec.tools[].error_rate` | float | `0.0` | Random error injection rate (0.0-1.0) |
| `spec.behavior.scenarios[].name` | string | | Scenario identifier (unique per agent) |
| `spec.behavior.scenarios[].match` | object | | Match conditions (omit for default) |
| `spec.behavior.scenarios[].match.content_contains` | string | | Case-insensitive substring match |
| `spec.behavior.scenarios[].match.content_regex` | string | | Regex pattern match |
| `spec.behavior.scenarios[].match.turn_number` | int | | Conversation turn (1-indexed) |
| `spec.behavior.scenarios[].response.content` | string | | Response text (supports Go templates) |
| `spec.behavior.scenarios[].response.tool_calls` | array | | Tool calls to simulate |
| `spec.behavior.scenarios[].response.metadata` | object | | Custom key-value metadata |
| `spec.behavior.streaming.enabled` | boolean | `false` | Enable SSE streaming |
| `spec.behavior.streaming.chunk_size` | int | `4` | Tokens per SSE chunk |
| `spec.behavior.streaming.chunk_delay_ms` | int | `50` | Delay between chunks (ms) |

## Validation Rules

- `metadata.name`: Must match `^[a-z0-9]+(-[a-z0-9]+)*$`, max 63 chars
- `spec.tools[].name`: Must match `^[a-z][a-z0-9_]*$`, unique within agent
- `spec.behavior.scenarios`: At least one required; names must be unique
- `match.content_contains` and `match.content_regex` are mutually exclusive
- Tool parameter schemas must include a `type` field
- Scenario `response.tool_calls` must reference defined tools
