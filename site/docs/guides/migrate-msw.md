# Migrating from Mock Service Worker

[Mock Service Worker (MSW)](https://mswjs.io/) is a general HTTP and GraphQL
interceptor for browsers and Node.js. MockAgents is a local protocol server for
AI integrations. Move LLM/provider behavior to MockAgents; keep MSW for your
application's ordinary REST and GraphQL dependencies.

This split lets the real OpenAI, Anthropic, Gemini, Ollama, or Bedrock SDK parse
the response. Your test no longer owns provider-specific envelopes, stream
frames, usage fields, tool-call IDs, or error shapes.

## Before: an OpenAI handler in MSW

Modern MSW handlers use `http` and `HttpResponse`:

```ts
import { http, HttpResponse } from "msw";

export const handlers = [
  http.post("https://api.openai.com/v1/chat/completions", async ({ request }) => {
    const body = (await request.json()) as {
      messages: Array<{ role: string; content: string }>;
    };
    const last = body.messages.at(-1)?.content ?? "";

    if (last.includes("refund")) {
      return HttpResponse.json({
        id: "chatcmpl-test",
        object: "chat.completion",
        created: 0,
        model: "gpt-4o",
        choices: [{
          index: 0,
          message: { role: "assistant", content: "Refunds take 5 days." },
          finish_reason: "stop",
        }],
        usage: { prompt_tokens: 4, completion_tokens: 5, total_tokens: 9 },
      });
    }

    return HttpResponse.json({ error: { message: "No handler" } }, { status: 503 });
  }),
];
```

The test is maintaining an OpenAI response implementation. Streaming, tools,
strict request validation, and retries multiply that code.

## After: a declarative MockAgents scenario

Create `agents/support.yaml`:

```yaml
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: support
spec:
  protocol: openai-chat-completions
  model: gpt-4o
  behavior:
    scenarios:
      - name: refund-policy
        match:
          content_contains: refund
        response:
          content: Refunds take 5 days.
```

Start MockAgents and point the existing SDK at it:

```bash
mockagents validate agents/support.yaml
mockagents start --agents-dir agents --port 8080
```

```ts
import OpenAI from "openai";

const client = new OpenAI({
  apiKey: "mock",
  baseURL: "http://127.0.0.1:8080/v1",
});
```

The application still calls the official SDK. Only its base URL changes.

## Test lifecycle

For Vitest or Jest, `setupMockAgents()` starts one static-binary server for the
test file, exports the provider base URLs, then stops it and restores the
environment after the suite:

```ts
import { setupMockAgents } from "@mockagents/vitest";

setupMockAgents({ agentsDir: "./agents" });
```

Keep the MSW server/worker active for non-AI handlers. Remove only provider
handlers whose base URLs now point to MockAgents. This supports incremental
migration instead of a flag day.

## Common handler translations

| MSW behavior | MockAgents representation |
|---|---|
| Inspect the last user message | `match.content_contains` or `content_regex` |
| Branch by conversation step | `match.turn_number` or ordered scenarios |
| Return a tool call | `response.tool_calls` plus `spec.tools` |
| Return HTTP 429/5xx | `behavior.chaos.errors` or a named chaos preset |
| Delay a response | `behavior.chaos.latency` |
| Stream tokens | `behavior.streaming` (TTFT, pacing, jitter, truncation) |
| Override one test | Use a scenario pack or a test-specific agents directory |
| Assert what was called | Use MockAgents trajectory/journal assertions |

Tool-call example:

```yaml
spec:
  protocol: openai-chat-completions
  tools:
    - name: get_order
      parameters:
        type: object
        properties:
          order_id: { type: string }
        required: [order_id]
  behavior:
    scenarios:
      - name: inspect-order
        match: { content_contains: order }
        response:
          tool_calls:
            - name: get_order
              arguments: { order_id: ORD-123 }
```

## Per-test errors and overrides

MSW commonly uses `server.use(...)` inside a test. In MockAgents, put the
variant in a small scenario pack or test-only agents directory and start the
fixture with that path. For broadly reusable failures, keep named agents such
as `rate-limited`, `flaky-then-healthy`, or `connection-reset` and select the
one the test needs.

```yaml
behavior:
  chaos:
    preset: rate-limited
  scenarios:
    - name: unreachable
      response: { content: never returned }
```

Chaos is deterministic when you configure a seed. Narrower request, sequence,
fixture, operation, and service controls override global defaults.

## What should stay in MSW

Keep MSW when the dependency is ordinary HTTP/GraphQL, when browser-level
service-worker interception is itself under test, or when a handler executes
arbitrary JavaScript side effects. MockAgents intentionally favors bounded,
reviewable, deterministic fixtures over general request-handler code.

For an unsupported AI endpoint, keep that one MSW handler while migrating the
supported routes. Do not replace a precise handler with a broad MockAgents
catch-all merely to finish the migration.

## Completion checklist

1. Validate every generated or handwritten Agent file.
2. Change provider SDK base URLs before constructing the clients.
3. Remove migrated provider handlers from the MSW handler list.
4. Run tests with network egress blocked to prove no real provider is called.
5. Keep MSW for the remaining application REST/GraphQL mocks.
