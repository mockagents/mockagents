# @mockagents/sdk (TypeScript SDK)

TypeScript / JavaScript SDK for [MockAgents](https://github.com/mockagents/mockagents) —
spin up mock AI agents, point your OpenAI / Anthropic / LangChain / Vercel AI
SDK code at them, and write deterministic integration tests without burning
real LLM tokens.

## Install

```bash
npm install @mockagents/sdk
```

Requires Node.js **18 or later** (uses the built-in `fetch`). The
`mockagents` Go binary must be on your `PATH` or at `./mockagents` — install it
with `go install github.com/mockagents/mockagents/cmd/mockagents@latest`
or build it from the repo with `make build`.

## Quick start

```ts
import { MockAgentServer, Scenario, runScenario, expect } from "@mockagents/sdk";

const server = new MockAgentServer({ agentsDir: "./agents" });
await server.start();

try {
  const client = server.client();
  const result = await runScenario(
    client,
    new Scenario({
      name: "order-lookup",
      steps: [{ role: "user", content: "where is my order?" }],
    }),
  );

  expect(result)
    .toHaveResponseContaining("shipped")
    .toHaveToolCall("lookup_order", { order_id: "ORD-1" })
    .toHaveLatencyLessThan(1000);
} finally {
  await server.stop();
}
```

## Trajectory assertions

Assert the *shape* of what the agent did, not just what it said:

```ts
expect(result)
  .toHaveToolCallSequence(["search", "rerank", "summarize"])  // exact order
  .toHaveToolCallCount(3)                                     // total calls
  .toHaveToolCallCount(1, "search");                          // calls to one tool
```

These mirror the `tool_call_sequence` and `tool_call_count` assertions of
`kind: TestSuite` YAML, so a check reads the same whether you run it through
`mockagents test` or from your own suite. Both read the **aggregate across every
turn** of a scenario — a multi-turn trajectory is every call it made, in order.
`toHaveToolCallSequence` is full equality, not a subsequence: an unexpected extra
call fails it.

The two-argument `toHaveToolCallCount(n, name)` narrows to a single tool. That is
an SDK-only convenience with no YAML equivalent — use the one-argument form when
you want a check that transfers to a YAML suite.

> `node_sequence` (ordered pipeline nodes) is **not** available here. A
> `kind: Pipeline` can only be executed by the in-process CLI runner, so an HTTP
> client cannot produce the node trajectory — see
> [#33](https://github.com/mockagents/mockagents/issues/33). It remains YAML-only.

## API surface

| Export | Purpose |
| --- | --- |
| `MockAgentServer` | Spawns the Go binary, picks a free port, polls `/api/v1/health`. |
| `MockAgentClient` | `fetch`-based client for `/v1/chat/completions`, `/v1/messages`, and the management API. |
| `Scenario`, `runScenario` | Declarative multi-turn scripts with automatic session scoping. |
| `expect(target)` | Fluent assertion helper: `toHaveToolCall`, `toHaveResponseContaining`, `toHaveFinishReason`, `toHaveStatusCode`, `toHaveLatencyLessThan`, plus the trajectory assertions `toHaveToolCallCount` and `toHaveToolCallSequence` (see below). |
| `adapters.chatOpenAI(server)` | Returns a `@langchain/openai` `ChatOpenAI` pointed at the mock. |
| `adapters.chatAnthropic(server)` | Returns a `@langchain/anthropic` `ChatAnthropic` pointed at the mock. |
| `adapters.patchEnv(server)` | Temporarily sets `OPENAI_BASE_URL` / `ANTHROPIC_BASE_URL` for LangGraph-style frameworks. |
| `adapters.mockOpenAIProvider(server)` | Returns a `@ai-sdk/openai` provider pointed at the mock. |

Framework adapters are lazy-imported — installing `@mockagents/sdk` does **not**
pull in LangChain or Vercel AI SDK. They raise a descriptive error on first
use when the optional peer is missing.

## Development

```bash
npm install       # one-time
npm test          # run vitest
npm run build     # tsc -> dist/
npm run typecheck # tsc --noEmit
```

## Known limitations

- `MockAgentServer` has no built-in hot-reload flag yet; use the management
  API (`POST /api/v1/agents/:name/reload`) directly for now.

Streaming **is** wrapped: `iterStream` yields protocol-agnostic `StreamChunk`
objects for both OpenAI and Anthropic SSE — see the
[TypeScript SDK guide](https://mockagents.github.io/mockagents/sdk/typescript-sdk/).
