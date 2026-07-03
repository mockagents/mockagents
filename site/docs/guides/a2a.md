# Mocking A2A Agents

The [Agent2Agent (A2A) protocol](https://a2a-protocol.org) lets agents from
different vendors discover and call each other: a public **Agent Card**, a
JSON-RPC endpoint for `message/send`, and a **Task** lifecycle with streaming
updates. If your system *calls* remote A2A agents, testing means standing up a
real peer agent — slow, stateful, and often not yours to run.

MockAgents mocks the A2A **server** side: a `kind: A2AServer` document declares
the agent card and canned, match-based replies, and `mockagents a2a` serves the
card, `message/send`, `message/stream` (SSE), `tasks/get`, and `tasks/cancel`.

## Quickstart

[`examples/a2a-server.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/a2a-server.yaml):

```yaml
apiVersion: mockagents/v1
kind: A2AServer
metadata:
  name: weather-a2a
spec:
  card:
    name: Weather Agent
    description: A mock A2A agent that answers weather questions.
    version: 1.0.0
    defaultInputModes: [text/plain]
    defaultOutputModes: [text/plain]
    skills:
      - id: forecast
        name: Get forecast
        description: Returns a canned weather forecast.
        tags: [weather]
        examples: ["What's the weather in Paris?"]
  responses:
    - match: weather
      text: "It's sunny and 22°C."
      data:                       # optional structured `data` Part next to the text
        temperature_c: 22
        condition: sunny
    - match: rain
      text: "Light rain is expected this afternoon."
    - default: true
      text: "I can only help with the weather."
```

Serve it (default port **8083**):

```bash
mockagents a2a --agents-dir examples --server weather-a2a
```

`--server` is only needed when more than one `A2AServer` document is loaded.

Fetch the Agent Card:

```bash
curl http://localhost:8083/.well-known/agent-card.json
```

The card is served at the current well-known path **and** the older
`/.well-known/agent.json` alias, with spec-required fields defaulted for you:
`protocolVersion: "0.3.0"`, `preferredTransport: "JSONRPC"`, `url` (the request
origin), `version: "0.0.0"`, input/output modes `[text/plain]`, and
`capabilities.streaming: true` (the mock always serves `message/stream`).

Send a message (JSON-RPC 2.0 on `POST /`):

```bash
curl -X POST http://localhost:8083/ -H 'Content-Type: application/json' -d '{
  "jsonrpc": "2.0", "id": 1, "method": "message/send",
  "params": {"message": {"role": "user", "messageId": "m1",
    "parts": [{"kind": "text", "text": "what is the weather in Paris?"}]}}}'
```

The result is a **Task** — `status.state: "completed"`, one artifact named
`response` whose parts are the matched text (plus the structured `data` part,
because the `weather` response declares one), and a `history` of the user and
agent messages. Then poll or cancel it using the `result.id` from the send:

```bash
curl -X POST http://localhost:8083/ -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tasks/get","params":{"id":"<result.id>"}}'
curl -X POST http://localhost:8083/ -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":3,"method":"tasks/cancel","params":{"id":"<result.id>"}}'
```

## How matching works

Responses are evaluated in order; the first entry whose `match` string is a
**substring** of the incoming message's text wins, else the `default: true`
entry. Matching is case-sensitive and reads only the `text` parts of the
message (`file`/`data` parts are kept in history but not matched).

Per-response options:

| Field | Effect |
|---|---|
| `text` | The reply — becomes the artifact and the final status message. |
| `state` | Terminal task state to report (default `completed`; also `failed`, `rejected`, `canceled`, or non-terminal states like `input-required`). |
| `as_message: true` | `message/send` returns a bare `Message` instead of a Task (nothing stored). `message/stream` still yields a Task. |
| `data` | Emit a structured `data` Part alongside the text Part. |

## Streaming (`message/stream`)

`message/stream` answers over SSE with the standard four-event sequence, each
frame a JSON-RPC `result`:

1. the initial `Task` (`status.state: "working"`),
2. a `status-update` (`working`, `final: false`),
3. an `artifact-update` carrying the reply parts (`lastChunk: true`),
4. the final `status-update` with the terminal state and agent message
   (`final: true`).

```bash
curl -N -X POST http://localhost:8083/ -H 'Content-Type: application/json' -d '{
  "jsonrpc": "2.0", "id": 1, "method": "message/stream",
  "params": {"message": {"role": "user",
    "parts": [{"kind": "text", "text": "weather?"}]}}}'
```

The terminal task is persisted, so a later `tasks/get` on its id is consistent
with what the stream reported.

## Error codes

Standard JSON-RPC codes plus the A2A-specific ones:

| Code | Meaning |
|---|---|
| `-32700` / `-32600` / `-32601` / `-32602` / `-32603` | parse / invalid request / method not found / invalid params / internal |
| `-32001` | task not found (`tasks/get` / `tasks/cancel` with an unknown id) |
| `-32002` | task not cancelable (already in a terminal state) |

## CLI reference

```bash
mockagents a2a [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--port`, `-p` | `8083` | HTTP port |
| `--server` | | Name of the `A2AServer` to serve (required when multiple are loaded) |
| `--agents-dir` | `./agents` | Directory containing definitions (global flag) |

`GET /healthz` returns `ok`.

## Troubleshooting

- **Everything falls through to the default reply** — matching is
  **case-sensitive substring** on the text parts only. `match: Weather` won't
  match "what's the weather"; use lowercase fragments.
- **`-32002` on cancel** — the task already completed; the mock's tasks
  complete synchronously with `message/send`, so cancel is only meaningful for
  tasks you gave a non-terminal `state` (e.g. `working`, `input-required`).
- **Client rejects the card** — validate with `mockagents validate`; note the
  A2A spec requires `skills[].tags`, which the mock normalizes to `[]` if
  omitted.
