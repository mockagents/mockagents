# Tools and extension interfaces

There are three distinct tool surfaces: scenario-declared provider tool calls, MCP JSON-RPC tools, and developer tooling such as the CLI and SDK process managers. A name in an agent definition does not register a native executable or call a remote API.

## Agent tools

`ToolDefinition` has `name`, optional `description`, `parameters` (a JSON Schema object), `responses`, `validate`, and `error_rate`. A scenario emits a `ToolCallSpec` with `name`, structured `arguments`, and optionally `raw_arguments` for an intentionally malformed OpenAI-family argument string. See [tool types](../../internal/types/tool.go), [behavior types](../../internal/types/behavior.go), and [processor](../../internal/engine/tool_processor.go).

For each call, the processor resolves its exact tool name, optionally validates arguments, evaluates random error injection, then finds a response rule. Every key in a rule's `match` must be present and equal; extra incoming argument keys are ignored. Numeric representations compare tolerantly, but a numeric string is not automatically a number. The first matching non-default rule wins. A default rule is used after all match rules; if multiple defaults are authored, the processor retains the last. The Go authoring validator does not reject duplicate tool defaults. A rule with no match and no default is skipped. With no result, the fallback is `{"status":"ok"}`.

The JSON Schema and Go validator bound tool error_rate to 0–1; Go also rejects nonfinite values. CLI loading, validation preview, and agent PUT share the validator. See [validator](../../internal/config/validator.go), [Agent schema](../../schema/mockagents-v1-agent.json), and [HTTP boundary regressions](../../internal/server/agent_bounds_test.go).

| Failure | Internal tool result | Whole engine turn |
| --- | --- | --- |
| Unknown tool | `is_error: true`, `TOOL_NOT_FOUND` | Best-effort result retained and warning logged |
| Invalid parameters with validation enabled | `INVALID_PARAMETERS` | Error result; not automatically an HTTP failure |
| `error_rate` fires | `INJECTED_ERROR` | Error result |
| Authored error rule | Configured code/message | Error result |
| Strict request violation | `StrictToolError` before/around generation | Provider-shaped request rejection in strict mode |

One or two simulated calls run inline; larger lists use goroutines and return results in input order. These are CPU-only fixture resolutions. Random tool `error_rate` uses cryptographic randomness, independently of seeded chaos. For deterministic failure tests, use an authored error rule or rate 1.

The neutral response exposes `ToolCalls` and `ToolResults` for engine consumers and the suite runner. Provider adapters expose their own expected tool-call shape. The application under test ordinarily dispatches its own tools and sends the results back; MockAgents does not execute the application's function implementation.

## MCP tools and resources

`MCPServer` tools have `inputSchema` and response rules containing MCP content blocks and `isError`. The MCP handler validates supported input schema rules and returns protocol results. Resources and prompts are independently declared; completions provide configured suggestions. Supported content includes text, image, audio, and embedded resources. Embedded resource fields are flat in the authored model and nested under `resource` by its custom JSON serializer.

MCP runs via `mockagents mcp --transport http` or `--transport stdio`. HTTP defaults to loopback port 8081. The Streamable HTTP endpoint is `/mcp`; test/legacy helpers are separately mounted and listed in the [route catalog](reference/routes.md). Discovery capabilities and fixture configuration are documented in the [MCP guide](../../site/docs/guides/mcp.md).

`--manage` adds built-in agent-management tools backed by the management implementation. This listener does not inherit the main HTTP server's tenancy middleware. Non-loopback management requires `--allow-remote-manage`; enabling it is not an authentication mechanism. See [mcpadmin](../../internal/mcpadmin/) and [CLI wiring](../../cmd/mockagents/mcp.go).

## Tool schema and extensibility

[toolschema](../../internal/toolschema/) implements a supported JSON Schema subset and a separate strict OpenAI function-schema subset. These are not a general full-draft JSON Schema engine. Agent parameter validation, strict request validation, and MCP calls share this package where their contracts agree.

To add a fixture tool, declare its schema and responses, reference it from a scenario or expose it in an MCP definition, validate, and test successful and error outputs. To add validation semantics, update the common validator and both engine/MCP tests before claiming support. To add a provider, implement the compiled `Adapter` interface and register it; to change MCP/A2A RPC behavior, change their dedicated dispatchers. There is no generic runtime plugin discovery mechanism.

## Developer tooling map

| Tool | Purpose | Source |
| --- | --- | --- |
| `init`, `validate`, `start` | Scaffold, validate, and serve fixtures | [CLI](../../cmd/mockagents/) |
| `add`, `rm` | Runtime agent write API clients | [agent_cmd.go](../../cmd/mockagents/agent_cmd.go) |
| `test` | Run agent/pipeline suites and export reports | [test.go](../../cmd/mockagents/test.go) |
| `contract extract/diff` | Version consumer-visible fixture contracts | [contract](../../internal/contract/) |
| `drift` | Compare scrubbed SDK/provider/mock contracts | [drift](../../internal/drift/) |
| `record`, `replay`, `import`, `convert` | Capture, replay, import and migrate fixtures | [recording](../../internal/recording/), [conversion](../../internal/conversion/) |
| `logs` | Read the selected local interaction database | [logs.go](../../cmd/mockagents/logs.go) |
| `driftcheck`, `liquidcheck`, `doccheck` | Type/schema, docs parser, local path consistency | [tools](../../tools/) |
| `handoffcatalog` | Refresh source/model/route/dependency appendices | [generator](../../tools/handoffcatalog/main.go) |

Exact flags live in the [CLI reference](../../site/docs/guides/cli-reference.md) and command `--help`; [onboarding](onboarding.md) gives a minimal local workflow.
