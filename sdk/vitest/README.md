# @mockagents/vitest

Auto-spawn [MockAgents](https://github.com/mockagents/mockagents) in your
**Vitest** (or **Jest**) suite and redirect the OpenAI / Anthropic / Gemini SDKs
at it — so your existing application code runs against a deterministic mock,
with no token spend and no code changes.

```bash
npm install -D @mockagents/vitest @mockagents/sdk
```

Requires Node.js **18+**. The `mockagents` Go binary must be on your `PATH`, at
`./mockagents`, or pointed to by `MOCKAGENTS_BIN` — install it with
`go install github.com/mockagents/mockagents/cmd/mockagents@latest` or build it
from the repo with `make build`.

## Quick start (Vitest) — green in 5 lines

```ts
import { setupMockAgents } from "@mockagents/vitest";
import { expect, test } from "vitest";

const mock = setupMockAgents({ agentsDir: "./agents" });

test("my agent calls the model", async () => {
  // OPENAI_BASE_URL now points at the mock — real SDK code "just works":
  const { OpenAI } = await import("openai");
  const client = new OpenAI();
  const res = await client.chat.completions.create({
    model: "gpt-4o",
    messages: [{ role: "user", content: "hello" }],
  });
  expect(res.choices[0].message.content).toBeTruthy();
});
```

`setupMockAgents()` registers a `beforeAll` that starts **one** server for the
file (on an auto-selected free port) and patches the provider env vars, and an
`afterAll` that restores the env and stops the server. Access the running server
through the returned handle: `mock.url`, `mock.server`, `mock.client`.

### What gets patched

| Env var | Set to |
| --- | --- |
| `OPENAI_BASE_URL` | `<mock-url>/v1` |
| `ANTHROPIC_BASE_URL` | `<mock-url>` |
| `GOOGLE_GEMINI_BASE_URL` | `<mock-url>` |
| `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` / `GEMINI_API_KEY` / `GOOGLE_API_KEY` | `mock-key` (dummy) |

> **Gemini caveat:** the base-URL redirect is honored only by the newer
> `@google/genai` client (and the Vercel AI SDK's Google provider). The legacy
> `google-generativeai` package has no base-URL env var.

## Using the built-in client

The handle exposes a [`MockAgentClient`](../typescript/README.md) bound to the
running server, so you can drive the mock directly without any provider SDK:

```ts
const mock = setupMockAgents({ agentsDir: "./agents" });

test("weather tool routes correctly", async () => {
  const res = await mock.client.chat([{ role: "user", content: "weather in NYC?" }], {
    model: "gpt-4o",
  });
  expect(res.statusCode).toBe(200);
});
```

## Fixture style

If you prefer Vitest's fixture injection over reaching through the handle,
`mockagentsFixture()` produces a `test` with `mockagents` and `mockagentsClient`
fixtures:

```ts
import { setupMockAgents, mockagentsFixture } from "@mockagents/vitest";
import { expect } from "vitest";

const mock = setupMockAgents({ agentsDir: "./agents" });
const test = mockagentsFixture(mock);

test("injected client", async ({ mockagentsClient }) => {
  const res = await mockagentsClient.chat([{ role: "user", content: "hi" }]);
  expect(res.content).toBeTruthy();
});
```

## Jest

The same ergonomics are available for Jest from the `/jest` subpath, wired to
Jest's global `beforeAll` / `afterAll`:

```ts
import { setupMockAgents } from "@mockagents/vitest/jest";

const mock = setupMockAgents({ agentsDir: "./agents" });

test("my agent", async () => {
  const res = await mock.client.chat([{ role: "user", content: "hi" }]);
  expect(res.content).toBeTruthy();
});
```

## Options

`setupMockAgents(options)` accepts every
[`MockAgentServerOptions`](../typescript/README.md) field plus:

| Option | Default | Description |
| --- | --- | --- |
| `agentsDir` | `"./agents"` | Directory of agent YAML definitions. |
| `port` | `0` (auto) | Port to listen on; `0` picks a free one. |
| `binaryPath` | auto-detect | Path to the `mockagents` binary. Honors `MOCKAGENTS_BIN`. |
| `logLevel` | `"warn"` | Server log level. |
| `patchEnv` | `true` | Patch the provider base-URL / key env vars. Set `false` to use the client only. |
| `env` | — | Extra env vars to set for the suite (merged after, and overriding, the provider patch). Restored afterwards. |
| `startTimeoutMs` | `10000` | Health-check timeout for server startup. |

## License

Apache-2.0
