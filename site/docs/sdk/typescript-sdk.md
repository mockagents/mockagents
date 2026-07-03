# TypeScript SDK Guide

```bash
npm install @mockagents/sdk
```

ESM package, Node 18+ (uses the built-in `fetch`). The server manager expects
the `mockagents` Go binary on `PATH` (or set `MOCKAGENTS_BIN`).

## MockAgentServer

Manages the MockAgents Go binary as a subprocess.

```ts
import { MockAgentServer } from "@mockagents/sdk";

const server = new MockAgentServer({ agentsDir: "./agents" });
await server.start();
try {
  const client = server.client();
  // ... use client
} finally {
  await server.stop();
}
```

**Options:**

| Option | Default | Description |
|--------|---------|-------------|
| `agentsDir` | `./agents` | Agent YAML directory |
| `port` | `0` (auto) | Server port. 0 = auto-select a free port. |
| `binaryPath` | auto-detect | Path to the `mockagents` binary (`MOCKAGENTS_BIN` honored) |
| `logLevel` | `warn` | Server log level |

`server.url`, `server.isRunning`, and `server.getLogs()` are available for
diagnostics; `findFreePort()` and `findBinary()` are exported as free
functions.

## MockAgentClient

HTTP client for the mock server, supporting the OpenAI and Anthropic
protocols.

### OpenAI Chat Completions

```ts
import { MockAgentClient } from "@mockagents/sdk";

const client = new MockAgentClient({ baseUrl: "http://localhost:8080" });

const response = await client.chat(
  [{ role: "user", content: "hello" }],
  { model: "gpt-4o" },
);
console.log(response.content);       // "Hello!"
console.log(response.finishReason);  // "stop"
console.log(response.usage?.totalTokens);
console.log(response.toolCalls);     // []
```

`ChatOptions`: `model`, `sessionId` (sent as `X-Session-Id`), `tools`,
`toolChoice`, `temperature`, `maxTokens`, `extra`.

### Anthropic Messages

```ts
const message = await client.message(
  [{ role: "user", content: "hello" }],
  { model: "claude-3-5-sonnet-latest", system: "You are helpful." },
);
console.log(message.content);
```

### Streaming

Raw per-protocol streams, or the protocol-agnostic `iterStream` that yields
normalized `StreamChunk`s:

```ts
// Raw OpenAI deltas
for await (const chunk of client.chatStream(
  [{ role: "user", content: "hello" }], { model: "gpt-4o" },
)) {
  const delta = (chunk as any).choices?.[0]?.delta;
  if (delta?.content) process.stdout.write(delta.content);
}

// Protocol-agnostic — same loop works for openai and anthropic
for await (const chunk of client.iterStream(
  [{ role: "user", content: "hello" }],
  { protocol: "anthropic", model: "claude-3-5-sonnet-latest" },
)) {
  process.stdout.write(chunk.text);
  if (chunk.finished) console.log("\nfinish:", chunk.finishReason);
}
```

```ts
interface StreamChunk {
  text: string;
  toolCallDelta?: [number, string, string]; // [index, name, argumentsFragment]
  finishReason: string;
  finished: boolean;
  raw: unknown;
}
```

`messageStream()` is the raw Anthropic-event equivalent of `chatStream()`.

### Management

```ts
await client.health();              // { status: "ok", ... }
await client.listAgents();          // AgentSummary[]
await client.getAgent("my-agent");  // full agent definition
await client.reloadAgent("my-agent");
```

## Scenarios

```ts
import { Scenario, runScenario } from "@mockagents/sdk";

const result = await runScenario(client, new Scenario({
  name: "order-lookup",
  steps: [{ role: "user", content: "where is my order?" }],
}));

console.log(result.lastContent);      // final response text
console.log(result.totalLatencyMs);
console.log(result.responses.length); // one ChatResponse per user step
```

Only `user` steps trigger a request; `assistant`/`system` steps are context.
Each scenario gets a stable `sessionId` by default, so `turn_number` matching
works across steps.

## Assertions

Chainable `expect()` that throws `AssertionError` — works inside any test
runner (the SDK's own tests use Vitest):

```ts
import { expect } from "@mockagents/sdk";

expect(result)
  .toHaveResponseContaining("shipped")
  .toHaveToolCall("lookup_order", { order_id: "ORD-1" })
  .toHaveFinishReason("stop")
  .toHaveStatusCode(200)
  .toHaveToolCallCount(1)
  .toHaveLatencyLessThan(1000);
```

## Vitest / Jest integration

```ts
import { beforeAll, afterAll, test } from "vitest";
import { MockAgentServer, expect as maExpect } from "@mockagents/sdk";

let server: MockAgentServer;

beforeAll(async () => {
  server = new MockAgentServer({ agentsDir: "./agents" });
  await server.start();
});
afterAll(async () => { await server.stop(); });

test("greeting", async () => {
  const response = await server.client().chat(
    [{ role: "user", content: "hello" }], { model: "gpt-4o" },
  );
  maExpect(response).toHaveResponseContaining("Hello");
});
```

## Framework adapters & MCP

- `@mockagents/sdk/adapters` provides factories that point LangChain.js / the
  Vercel AI SDK at the mock (see [Drop-in Recipes](../guides/drop-in-recipes.md)
  for the raw base-URL equivalents).
- `McpClient` speaks the mock's bidirectional MCP channel
  (`GET /mcp/events` + `POST /mcp/response`) for testing server-initiated
  `sampling/createMessage` / `roots/list` flows — see the
  [MCP guide](../guides/mcp.md).
- For Vitest/Jest test bootstrap that auto-spawns the server and redirects the
  provider SDKs, see the separate `@mockagents/vitest` helper package.
