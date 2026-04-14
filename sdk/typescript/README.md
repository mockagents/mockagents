# mockagents (TypeScript SDK)

TypeScript / JavaScript SDK for [MockAgents](https://github.com/mockagents/mockagents) —
spin up mock AI agents, point your OpenAI / Anthropic / LangChain / Vercel AI
SDK code at them, and write deterministic integration tests without burning
real LLM tokens.

## Install

```bash
npm install mockagents
```

Requires Node.js **18 or later** (uses the built-in `fetch`). The
`mockagents` Go binary must be on your `PATH` or at `./mockagents` — install it
with `go install github.com/mockagents/mockagents/cmd/mockagents@latest`
or build it from the repo with `make build`.

## Quick start

```ts
import { MockAgentServer, Scenario, runScenario, expect } from "mockagents";

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

## API surface

| Export | Purpose |
| --- | --- |
| `MockAgentServer` | Spawns the Go binary, picks a free port, polls `/api/v1/health`. |
| `MockAgentClient` | `fetch`-based client for `/v1/chat/completions`, `/v1/messages`, and the management API. |
| `Scenario`, `runScenario` | Declarative multi-turn scripts with automatic session scoping. |
| `expect(target)` | Fluent assertion helper: `toHaveToolCall`, `toHaveResponseContaining`, `toHaveFinishReason`, `toHaveStatusCode`, `toHaveLatencyLessThan`, `toHaveToolCallCount`. |
| `adapters.chatOpenAI(server)` | Returns a `@langchain/openai` `ChatOpenAI` pointed at the mock. |
| `adapters.chatAnthropic(server)` | Returns a `@langchain/anthropic` `ChatAnthropic` pointed at the mock. |
| `adapters.patchEnv(server)` | Temporarily sets `OPENAI_BASE_URL` / `ANTHROPIC_BASE_URL` for LangGraph-style frameworks. |
| `adapters.mockOpenAIProvider(server)` | Returns a `@ai-sdk/openai` provider pointed at the mock. |

Framework adapters are lazy-imported — installing `mockagents` does **not**
pull in LangChain or Vercel AI SDK. They raise a descriptive error on first
use when the optional peer is missing.

## Development

```bash
npm install       # one-time
npm test          # run vitest
npm run build     # tsc -> dist/
npm run typecheck # tsc --noEmit
```

## Known v1 limitations

- Streaming (SSE) is not yet wrapped. Call the `/v1/chat/completions` endpoint
  directly with `stream: true` if you need it — parity with the Python SDK
  streaming helpers is on the roadmap.
- `MockAgentServer` has no built-in hot-reload flag yet; use the management
  API (`POST /api/v1/agents/:name/reload`) directly for now.
