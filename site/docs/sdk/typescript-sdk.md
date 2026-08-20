# TypeScript SDK Guide

```bash
npm install -D @mockagents/sdk @mockagents/vitest
```

ESM package, Node 18+ (uses the built-in `fetch`). The server manager expects
the `mockagents` Go binary on `PATH` (or set `MOCKAGENTS_BIN`).

## Start here: one line of setup

`@mockagents/vitest` registers the `beforeAll`/`afterAll` for you: one server
per test file on a free port, provider env vars patched and restored. Works in
Jest too, from the `/jest` subpath.

```ts
import { setupMockAgents } from "@mockagents/vitest";
import { expect, test } from "vitest";

const mock = setupMockAgents({ agentsDir: "./agents" });

test("greeting", async () => {
  const { OpenAI } = await import("openai");   // your real app code, unchanged:
  const reply = await new OpenAI().chat.completions.create({
    model: "gpt-4o",
    messages: [{ role: "user", content: "hello" }],
  });
  expect(reply.choices[0].message.content).toContain("How can I help");
});
```

`OPENAI_BASE_URL`, `ANTHROPIC_BASE_URL`, `GOOGLE_GEMINI_BASE_URL` and dummy API
keys are set for the file and restored afterwards. The handle exposes
`mock.url`, `mock.server`, and `mock.client`.

### Assert the trajectory

```ts
import { Scenario, expect as expectAgent, runScenario } from "@mockagents/sdk";
import { setupMockAgents } from "@mockagents/vitest";
import { test } from "vitest";

const mock = setupMockAgents({ agentsDir: "./agents" });

test("support flow", async () => {
  const result = await runScenario(mock.client, new Scenario({
    name: "support",
    steps: [
      { role: "user", content: "what's the weather in London?" },
      { role: "user", content: "and where is my order?" },
    ],
  }));
  expectAgent(result)
    .toHaveToolCallSequence(["get_weather", "search_orders"])
    .toHaveToolCallCount(2)
    .toHaveToolCall("get_weather", { city: "London" });
});
```

Wrong tool, wrong order, one call too many — three bugs a text assertion cannot
see. See [Assertions](#assertions) for the exact semantics.

See the [`@mockagents/vitest` README](https://github.com/mockagents/mockagents/blob/main/sdk/vitest/README.md)
for the full option list and the fixture-injection style.

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
  .toHaveToolCall("lookup_order", { order_id: "ORD-1" })  // args are a PARTIAL match
  .toHaveFinishReason("stop")
  .toHaveStatusCode(200)
  .toHaveLatencyLessThan(1000);

// Trajectory — the ordered shape of what the agent did.
expect(result)
  .toHaveToolCallSequence(["search", "summarize"])  // full equality, not a subsequence
  .toHaveToolCallCount(3)                           // total across all turns
  .toHaveToolCallCount(2, "search");                // narrowed to one tool (SDK-only)
```

### Trajectory semantics

`toHaveToolCallSequence` and the one-argument `toHaveToolCallCount` match the
`tool_call_sequence` / `tool_call_count` assertions in `kind: TestSuite` YAML
and the Python SDK, so a check means the same thing in all three places:

- They read **every turn** of a `ScenarioResult`, in invocation order — a
  multi-turn trajectory is every call the agent made, not just the last
  response's calls. (Outcome assertions like `toHaveResponseContaining` read
  the **final** turn.)
- The sequence is **full equality**, not a subsequence: an unexpected extra
  call fails it. A silent extra retrieval is a bug worth failing on.
- The two-argument `toHaveToolCallCount(n, name)` has **no YAML equivalent** —
  use the one-argument form when you want a check that transfers.

`node_sequence`, the pipeline-trajectory assertion, is YAML-only: pipelines have
no HTTP execution surface for an SDK to drive yet
([#33](https://github.com/mockagents/mockagents/issues/33)).

## Wiring the lifecycle by hand

`setupMockAgents()` (see [the top of this page](#start-here-one-line-of-setup))
is the short way. Do it manually when you want a different lifetime, several
servers at once, or no dependency on `@mockagents/vitest`:

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
