# Mocking MCP Servers

Agents increasingly get their tools, resources, and prompts from **Model
Context Protocol (MCP)** servers. MockAgents mocks the *server* side of that
protocol, so you can test an MCP **client** — or an agent that uses one —
deterministically, offline, without standing up the real integration.

A `kind: MCPServer` YAML document declares tools (with match-based canned
results), resources, prompts, and completions; `mockagents mcp` serves it over
the current MCP **Streamable HTTP** transport or **stdio**. The official MCP
SDKs and Claude Desktop connect to it unchanged.

> For a compact end-to-end walkthrough, see
> [Testing AI Agents → Cookbook 2](testing-agents.md#cookbook-2-mocking-an-mcp-server).
> This page is the full reference.

## Quickstart

[`examples/weather-mcp.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/weather-mcp.yaml):

```yaml
apiVersion: mockagents/v1
kind: MCPServer
metadata:
  name: weather-mcp
spec:
  capabilities: { tools: true, resources: true, prompts: true }

  tools:
    - name: get_forecast
      description: Return a canned weather forecast for a city.
      inputSchema:
        type: object
        properties: { city: { type: string } }
        required: [city]
      responses:
        - match: { city: tokyo }
          content: [{ type: text, text: "Tokyo: sunny, 22C" }]
        - match: { city: london }
          content: [{ type: text, text: "London: drizzle, 14C" }]
        - default: true
          content: [{ type: text, text: "Unknown city: overcast, 15C (default)" }]

  resources:
    - uri: mock://weather/today
      name: Today's Forecast
      mimeType: text/plain
      text: "Global outlook: mostly clear."

  prompts:
    - name: greet
      arguments: [{ name: name, required: true }]
      messages:
        - role: user
          content: { type: text, text: "Hello {{name}}, want a forecast?" }
```

Serve it:

```bash
# Streamable HTTP on http://127.0.0.1:8081/mcp
mockagents mcp --transport http --port 8081 --agents-dir examples

# stdio, for clients that spawn the server as a subprocess
mockagents mcp --transport stdio --agents-dir examples --server weather-mcp
```

`--server` is only required when more than one `kind: MCPServer` document is
loaded from `--agents-dir`.

!!! note "Bound to 127.0.0.1 by default"
    The HTTP transport binds `127.0.0.1`, per the MCP spec's guidance for local
    servers (it's the primary DNS-rebinding defense). Pass `--bind 0.0.0.0` to
    expose it beyond the host — e.g. inside a container whose port is mapped
    out. Non-loopback `Origin` headers are rejected (the allowlist is a
    library-level field, not currently configurable from the CLI).

## The YAML surface (`kind: MCPServer`)

All fields live under `spec` (schema:
[`schema/mockagents-v1-mcpserver.json`](https://github.com/mockagents/mockagents/blob/main/schema/mockagents-v1-mcpserver.json)):

| Field | Description |
|---|---|
| `protocolVersion` | Version echoed by `initialize`. Default `2025-11-25`. |
| `strictArgs` | Validate `tools/call` arguments against `inputSchema`. **Default `true`** — set `false` to accept anything. |
| `capabilities` | Booleans `tools` / `resources` / `prompts` / `logging` advertised on `initialize` (sections with content are advertised automatically). |
| `tools[]` | `name` (required), `description`, `inputSchema` (JSON Schema), `responses[]`. |
| `resources[]` | `uri` (required), `name`, `description`, `mimeType`, and `text` **or** `blob` (base64). |
| `prompts[]` | `name` (required), `description`, `arguments[]`, `messages[]` — `{{arg}}` placeholders expand on `prompts/get`. |
| `completions[]` | Autocomplete catalog for `completion/complete`: `argName` + `values` (required), optional `refType` (`ref/prompt` / `ref/resource`) and `refName` filters. |

Tool `responses[]` use the same first-match-wins pattern as agent tools: each
entry has a `match` (argument values that must be a subset of the call's
arguments), or `default: true` as the fallback; `content` is a list of MCP
content blocks (`text`, `image`, `audio`, or embedded `resource`), and
`isError: true` turns the entry into a tool *execution* error result.

### Argument validation (`strictArgs`)

`tools/call` arguments are validated against the tool's `inputSchema` **by
default** — the MCP spec requires servers to validate inputs. A violation
comes back the spec-correct way: not a JSON-RPC error, but a result with
`isError: true` and a text block like
`invalid arguments for tool "get_forecast": missing required parameter "city"`.
Structural problems (malformed params, unknown tool name) are JSON-RPC
`-32602` errors. To accept any arguments:

```yaml
spec:
  strictArgs: false
```

## Transports

### Streamable HTTP (`/mcp`)

The single `/mcp` endpoint implements the current MCP Streamable HTTP
transport (protocol revision `2025-11-25`, with negotiation down to
`2024-11-05`):

- **POST** a JSON-RPC message. Requests are answered with `application/json` —
  or as a short SSE stream when you send `Accept: application/json,
  text/event-stream`. Notifications get `202 Accepted`.
- The `initialize` response mints an **`Mcp-Session-Id`** header that all
  subsequent requests must echo (missing → 400, unknown → 404). Send
  `MCP-Protocol-Version` on post-initialize requests.
- **GET** the same URL (with `Accept: text/event-stream`) for the resumable
  server→client event stream; reconnect with `Last-Event-ID` to replay missed
  events. One GET stream per session — a second concurrent one gets **409**.
- **DELETE** ends the session.

Drive it by hand:

```bash
curl -s -D - http://127.0.0.1:8081/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}'
# note the Mcp-Session-Id response header, then:
curl -s http://127.0.0.1:8081/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -H 'Mcp-Session-Id: <id from above>' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_forecast","arguments":{"city":"tokyo"}}}'
```

A legacy plain POST-JSON transport (no sessions) also remains at `/mcp/rpc`,
and `GET /healthz` returns `ok`.

### stdio

`--transport stdio` speaks newline-delimited JSON-RPC on stdin/stdout — the
transport Claude Desktop and most local MCP clients use to spawn servers.
Nothing but JSON-RPC is ever written to stdout (diagnostics go to stderr), and
an oversized frame (>10 MiB) returns a parse error without killing the
session.

```console
$ printf '%s\n' \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
    '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_forecast","arguments":{"city":"tokyo"}}}' \
  | mockagents mcp --transport stdio --agents-dir examples --server weather-mcp
```

## Connecting real clients

### Official MCP SDK (Python)

```python
# pip install mcp
import asyncio
from mcp import ClientSession
from mcp.client.streamable_http import streamablehttp_client

async def main():
    async with streamablehttp_client("http://127.0.0.1:8081/mcp") as (read, write, _):
        async with ClientSession(read, write) as session:
            await session.initialize()
            result = await session.call_tool("get_forecast", {"city": "tokyo"})
            print(result.content[0].text)   # "Tokyo: sunny, 22C"

asyncio.run(main())
```

### Claude Desktop (stdio)

```json
{
  "mcpServers": {
    "weather-mcp": {
      "command": "mockagents",
      "args": ["mcp", "--transport", "stdio",
               "--server", "weather-mcp",
               "--agents-dir", "/absolute/path/to/agents"]
    }
  }
}
```

Use an absolute path for `mockagents` if it isn't on `PATH`; `--server` can be
dropped when the directory contains exactly one `MCPServer` document.

## Supported protocol methods

`initialize` (with protocol-version negotiation), `notifications/initialized`,
`ping`, `tools/list`, `tools/call`, `resources/list`, `resources/read`,
`resources/subscribe`, `resources/unsubscribe`, `prompts/list`, `prompts/get`,
`completion/complete` (against `spec.completions`, case-insensitive prefix,
capped at 100 values), and `logging/setLevel`. Unknown methods return
`-32601`; JSON-RPC batch arrays return `-32600` (batching was removed from the
MCP spec in 2025-06-18).

Server-initiated calls (`sampling/createMessage`, `roots/list`) and
notifications flow through a bidirectional channel: clients subscribe to
`GET /mcp/events` (SSE) and POST replies to `/mcp/response`. Test harnesses
can trigger the outbound side directly with `POST /mcp/sample`,
`POST /mcp/roots`, or push a notification to all live sessions with
`POST /mcp/notify`.

## Manage agents over MCP (`--manage`)

`--manage` adds six built-in tools backed by the
[agent write API](management-api.md), so an MCP-capable assistant can create
and edit your mock fixtures conversationally:

| Tool | Does |
|---|---|
| `list_agents` | List served agents (name, model, protocol, scenario count) |
| `get_agent` | Return an agent's canonical YAML |
| `validate_agent` | Validate a definition without persisting |
| `create_agent` | Create a new agent (fails if the name exists) — serves immediately |
| `put_agent` | Create-or-replace — serves immediately |
| `delete_agent` | Stop serving and remove the persisted file |

It composes with a declarative server's own tools, and also works with **no**
`MCPServer` document at all (`mockagents mcp --manage --agents-dir ./agents`
serves a synthetic `mockagents-admin` server).

## Troubleshooting

- **400 "missing Mcp-Session-Id" / 404 "no valid session"** — every
  post-`initialize` POST must echo the session header from the `initialize`
  response. If the server restarted, re-initialize.
- **409 on GET /mcp** — the session already has an open event stream; each
  session gets exactly one.
- **403 Forbidden** — your client sent a non-loopback `Origin`. Local clients
  are fine; browser-hosted ones need the server exposed deliberately.
- **Client can't reach the server from another machine/container** — the
  default bind is `127.0.0.1`; start with `--bind 0.0.0.0`.
- **Tool calls fail with `isError: true` "invalid arguments"** — argument
  validation is on by default; fix the arguments or set
  `spec.strictArgs: false`.
