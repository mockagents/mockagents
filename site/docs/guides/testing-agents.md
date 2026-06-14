# Testing AI Agents

The hardest part of testing an LLM app isn't the model — it's everything *around*
it: did your agent pick the **right tool** with the **right arguments**? Did it
handle the tool result correctly? Did it call the right **MCP server**? A live
model answers differently every run, so those assertions flake.

MockAgents makes the model deterministic so you can test the logic around it —
fast, free, offline. This guide has two runnable cookbooks:

1. [Testing agent tool-calls](#cookbook-1-testing-agent-tool-calls)
2. [Mocking an MCP server](#cookbook-2-mocking-an-mcp-server)

---

## Cookbook 1: Testing agent tool-calls

**Goal:** assert that a given user message makes your agent call a specific tool
with specific arguments — every time.

### The agent

[`examples/tool-routing-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/tool-routing-agent.yaml)
routes "weather" questions to `get_weather` and "order" questions to
`search_orders`, deterministically:

```yaml
spec:
  protocol: openai-chat-completions
  model: gpt-4o
  behavior:
    scenarios:
      - name: weather-route
        match: { content_contains: "weather" }
        response:
          content: "Checking the weather for you."
          tool_calls:
            - name: get_weather
              arguments: { city: "London" }
      - name: order-route
        match: { content_contains: "order" }
        response:
          content: "Looking up your order."
          tool_calls:
            - name: search_orders
              arguments: { order_id: "ORD-42" }
      - name: default
        response: { content: "How can I help you today?" }
```

### Option A — a declarative TestSuite (no code)

[`examples/tool-routing-suite.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/tool-routing-suite.yaml)
asserts the routing with `tool_call` (tool **and** arguments) and
`scenario_matched` (which code path fired — not just the text):

```yaml
spec:
  target: { agent: tool-router }
  cases:
    - name: weather-question-calls-get_weather
      steps:
        - role: user
          content: "what's the weather in London?"
      assertions:
        - type: tool_call
          tool: get_weather
          arguments: { city: "London" }
        - type: scenario_matched
          value: weather-route
```

Run it (JUnit output drops straight into CI):

```console
$ mockagents test --agents-dir examples examples/tool-routing-suite.yaml
Suite: tool-router-suite (agent:tool-router)
✓   PASS  weather-question-calls-get_weather (0s)
✓   PASS  order-question-calls-search_orders (0s)
✓   PASS  smalltalk-calls-no-tool (0s)
  3 passed, 0 failed in 0s

# CI:
$ mockagents test --agents-dir examples examples/tool-routing-suite.yaml --format junit > report.xml
```

### Option B — pytest, against your real application code

The `mockagents` pytest fixture (shipped with `pip install mockagents`) points
the OpenAI SDK at the mock with zero changes to your code:

```python
def test_weather_question_routes_to_get_weather(mockagents):
    from openai import OpenAI
    resp = OpenAI().chat.completions.create(
        model="gpt-4o",
        messages=[{"role": "user", "content": "what's the weather in London?"}],
    )
    call = resp.choices[0].message.tool_calls[0]
    assert call.function.name == "get_weather"
    import json
    assert json.loads(call.function.arguments) == {"city": "London"}
```

```console
$ pytest --mockagents-agents-dir examples
```

Same agent definition, two ways to assert it — no token cost, no flakiness, runs
offline in milliseconds.

---

## Cookbook 2: Mocking an MCP server

Agents increasingly call **Model Context Protocol (MCP)** servers for tools and
resources. MockAgents mocks the *server* side so you can test your MCP *client*
(or an agent that uses one) without standing up the real thing — something most
mocking tools don't do.

### The MCP server

[`examples/weather-mcp.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/weather-mcp.yaml)
declares a `get_forecast` tool, a resource, and a prompt:

```yaml
apiVersion: mockagents/v1
kind: MCPServer
metadata: { name: weather-mcp }
spec:
  capabilities: { tools: true, resources: true, prompts: true }
  tools:
    - name: get_forecast
      inputSchema:
        type: object
        properties: { city: { type: string } }
        required: [city]
      responses:
        - match: { city: tokyo }
          content: [{ type: text, text: "Tokyo: sunny, 22C" }]
        - default: true
          content: [{ type: text, text: "Unknown city: overcast, 15C (default)" }]
```

### Serve it

```bash
# stdio (clients that spawn the server as a subprocess)
mockagents mcp --transport stdio --agents-dir examples --server weather-mcp

# or HTTP — Streamable HTTP transport on a single /mcp endpoint
mockagents mcp --transport http --port 8081 --agents-dir examples
```

### Drive it (JSON-RPC 2.0)

Piping line-delimited JSON-RPC into the stdio transport — exactly what an MCP
client does — returns deterministic results:

```console
$ printf '%s\n' \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
    '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
    '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_forecast","arguments":{"city":"tokyo"}}}' \
  | mockagents mcp --transport stdio --agents-dir examples --server weather-mcp

{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-11-25","serverInfo":{"name":"weather-mcp","version":"mock"},"capabilities":{"prompts":{},"resources":{"subscribe":true},"tools":{}}}}
{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"get_forecast","description":"Return a canned weather forecast for a city.","inputSchema":{"properties":{"city":{"type":"string"}},"required":["city"],"type":"object"}}]}}
{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"Tokyo: sunny, 22C"}]}}
```

Over HTTP, the `/mcp` endpoint speaks the current MCP **Streamable HTTP**
transport: `POST` your JSON-RPC bodies to `http://localhost:8081/mcp` (the
server returns `application/json`, or an SSE stream when you send
`Accept: application/json, text/event-stream`), open a `GET` on the same URL for
the resumable server→client event stream (`Last-Event-ID` replays missed
events), and `DELETE` it to end the session. The first `initialize` response
carries an `Mcp-Session-Id` header that subsequent requests must echo; the
`Origin` and `MCP-Protocol-Version` headers are validated. A plain
POST-JSON-only transport (no sessions) also remains at `/mcp/rpc`.

The Python/TypeScript/Go SDKs additionally ship an `McpClient` for the v0.3
bidirectional flow (`sampling/createMessage`, `roots/list`) over the
`GET /mcp/events` + `POST /mcp/response` SSE channel.

---

## Why this is the right layer

Don't test the model — mock it, and deterministically test the agent logic that
surrounds it (tool routing, argument extraction, result handling, MCP calls).
Pair MockAgents with an eval tool (e.g. promptfoo) that grades *output quality*:
MockAgents is the fast, free, offline lower layer of the testing pyramid that
gates your CI; evals are the slower, model-in-the-loop upper layer.
