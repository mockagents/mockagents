# Migrating from AIMock

MockAgents can convert the core AIMock LLM fixture format into a validated Agent
definition in one command:

```bash
mockagents convert aimock fixtures/support.json \
  --name support \
  --model gpt-4o \
  -o agents/support.yaml

mockagents validate agents/support.yaml
mockagents start --agents-dir agents
```

The command refuses to overwrite an existing file unless `--force` is supplied.
Use `-o -` to inspect the generated YAML without writing a file.

## What converts

| AIMock fixture field | MockAgents field | Behavior |
|---|---|---|
| `match.userMessage: "text"` | `match.content_contains` | Preserved |
| `match.turnIndex` | `match.turn_number` | Preserved |
| `response.content` | `response.content` | Preserved |
| `response.refusal` | `response.refusal` | Preserved |
| `response.toolCalls` | `response.tool_calls` | Names and argument objects preserved |
| `response.finishReason` | `response.finish_reason` | Preserved |

AIMock-only matchers such as `toolName`, `toolCallId`, `context`, and
`hasToolResult` do not have a semantics-preserving Agent matcher today. Those
fixtures are printed as `skip:` warnings. The converter never weakens them into
catch-all scenarios, because that could make a test pass against the wrong
response.

If every fixture is unsupported, conversion fails without creating an output.
The generated document runs through the same validator as `mockagents validate`,
so invalid names, protocols, and scenario shapes fail before the file is written.

## Point your client at MockAgents

For OpenAI-compatible clients, replace AIMock's URL with:

```bash
OPENAI_BASE_URL=http://127.0.0.1:8080/v1
OPENAI_API_KEY=mock
```

The application code and SDK stay unchanged. Review any skipped fixtures, then
port their matcher intent explicitly or keep them in the old suite until an
equivalent MockAgents matcher exists.
