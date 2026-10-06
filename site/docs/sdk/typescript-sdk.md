# TypeScript SDK Guide

```bash
npm install -D @mockagents/sdk @mockagents/vitest
```

ESM-only package, Node 18+ (uses the built-in `fetch`); there is no CommonJS
build, so load it with `import`, not `require()`. The server manager expects
the `mockagents` Go binary on `PATH` or at `./mockagents`, or pointed to by
`MOCKAGENTS_BINARY` / `MOCKAGENTS_BIN`.

## Start here: one line of setup

`@mockagents/vitest` registers the `beforeAll`/`afterAll` for you: one server
per test file on a free port, provider env vars patched and restored. Works in
Jest too, from the `/jest` subpath, with Jest running in ESM mode (see the
helper's README).

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
| `binaryPath` | auto-detect | Path to the `mockagents` binary (see below) |
| `logLevel` | `warn` | Server log level |

`server.url` (always `http://127.0.0.1:<port>`, matching the binary's IPv4-only
bind), `server.isRunning`, and `server.getLogs()` are available for
diagnostics; `findFreePort()` and `findBinary()` are exported as free
functions. `start()` fails as soon as the child process exits before becoming
healthy, with its logs in the error, rather than waiting out the timeout.

Binary discovery: `MOCKAGENTS_BINARY`, then `MOCKAGENTS_BIN` (the same names,
in the same order, as the Python and Go SDKs and `npx mockagents`), then
`./mockagents` in the working directory, then `PATH`. Parent directories are
not searched; point an env var at a monorepo build instead.

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
`toolChoice`, `temperature`, `maxTokens`, `extra`, plus `signal` (an
`AbortSignal`, honored by every call) and the stream-only `idleTimeoutMs` and
`failOnStreamFault` (see [Streaming](#streaming)).

Against a multi-tenant server, pass `apiKey` to the constructor. It is sent as
`Authorization: Bearer <key>` on **every** request (chat, messages, streams and
management calls) and as `X-Api-Key` on Anthropic calls. A missing key does
not fail there: the LLM endpoints accept anonymous callers and route them to a
different (global) agent.

### Anthropic Messages

```ts
const message = await client.message(
  [{ role: "user", content: "hello" }],
  { model: "claude-sonnet-4-20250514", system: "You are helpful." },
);
console.log(message.content);
```

The default model is `DEFAULT_ANTHROPIC_MODEL` (`claude-sonnet-4-20250514`),
the same in all three SDKs.

### Tool calls and round trips

Each `ToolCall` carries `arguments` (the parsed object), `rawArguments` (the
exact wire text) and `argumentsValid`. Malformed or non-object arguments, as
produced by a `raw_arguments` fault fixture, leave `arguments` as `{}` with
`argumentsValid: false`, so they are visible instead of looking like a call
with no arguments.

`ChatMessage` accepts assistant `tool_calls` and `tool_call_id` on `tool`
turns (and a content-parts array), so a tool round trip can be replayed, which
strict-tools id validation requires:

```ts
import { toAssistantMessage } from "@mockagents/sdk";

const first = await client.chat(history, { tools });
history.push(toAssistantMessage(first));
for (const call of first.toolCalls) {
  history.push({ role: "tool", tool_call_id: call.id, content: JSON.stringify(runTool(call)) });
}
const second = await client.chat(history, { tools });
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
  { protocol: "anthropic", model: "claude-sonnet-4-20250514" },
)) {
  process.stdout.write(chunk.text);
  if (chunk.finished) console.log("\nfinish:", chunk.finishReason);
}
```

Every streaming method returns the async generator plus a live `stats`
object, so injected stream faults are visible rather than looking like a
normal completion:

```ts
const stream = client.iterStream(messages);
for await (const chunk of stream) { /* ... */ }
stream.stats; // { completed, truncated, malformedFrames }
```

`truncated` means the body ended without `[DONE]` / `message_stop` (a
`streaming.truncateAfter` fault); `malformedFrames` counts skipped non-JSON
frames (a `streaming.malformed` fault). Pass `failOnStreamFault: true` to get a
`StreamError` at the end of such a stream instead.

Cancellation: pass `signal` to abort a request or an in-flight stream, and
`idleTimeoutMs` to fail with `StreamError` (`reason: "idle_timeout"`) when no
bytes arrive for that long after the headers. The idle timeout is off by
default, because a paced mock stream may pause legitimately.

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
  .toHaveToolCall("lookup_order", { order_id: "ORD-1" })  // args are a PARTIAL match;
                                                          // a key the call omitted never matches, even null
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

Pipeline trajectories are typed and use the same exact-order rule:

```ts
const result = await client.runPipeline("research", "summarize the evidence");
maExpect(result).toHaveNodeSequence(["plan", "research", "write"]);
```

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
