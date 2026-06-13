# Responses API demo — OpenAI Agents SDK against MockAgents

This demo runs the **OpenAI Agents SDK** (`openai-agents`) against MockAgents on
its **default transport, the Responses API** (`POST /v1/responses`) — the
surface MockAgents added in A-01.

> **Contrast with [`../customer-support-agent`](../customer-support-agent):**
> that demo calls `set_default_openai_api("chat_completions")` because the
> Agents SDK's *default* path — the Responses API — did not exist in MockAgents.
> It does now, so this demo leaves the SDK on its default and talks Responses.
> The agent code (`app/agent.py`) is otherwise identical in spirit: tools +
> instructions + a run loop that doesn't know it's hitting a mock.

## What it shows

- **Agents SDK on the Responses API** — `Runner.run(...)` driving multi-turn
  tool loops over `/v1/responses` (`app/run_demo.py`).
- **Streaming** — `Runner.run_streamed(...)` consuming the `response.*` event
  ladder as SDK stream events and printing token deltas (`app/streaming_demo.py`).
- **Stateful `previous_response_id`** — chaining turns by id with the raw
  OpenAI client; MockAgents replays the prior turn from its response store
  (`app/previous_response_demo.py`).
- **No Python** — a curl-only `scripts/smoke.sh` exercising the same surface.

## Run it

1. **Build / install MockAgents** (from the repo root):

   ```bash
   make build       # produces ./mockagents
   ```

2. **Start the server with this demo's fixtures:**

   ```bash
   ./mockagents start demo/responses-api-agent/mockagents
   ```

3. **Install deps and run the demos** (in another shell):

   ```bash
   cd demo/responses-api-agent
   python -m venv .venv && source .venv/bin/activate
   pip install -r requirements.txt
   cp .env.example .env            # optional; defaults already point at :8080

   python -m app.run_demo                  # multi-turn tool loops
   python -m app.streaming_demo            # streamed token deltas
   python -m app.previous_response_demo    # stateful previous_response_id
   ```

   Or, with no Python at all:

   ```bash
   ./scripts/smoke.sh
   ```

## How the tool loop terminates

MockAgents matches on the **latest user message** and keys turn state off the
`X-Session-Id` header. The tool-call scenarios are gated on `turn_number: 1`;
turn 2 falls through to a resolution scenario. The demo sends a fresh
`X-Session-Id` per conversation (`app/mock_setup.new_conversation_client`) so
the turn counter advances and each loop ends cleanly. See
`mockagents/responses-agent.yaml` for the full fixture.

## Files

| Path | Purpose |
|---|---|
| `mockagents/responses-agent.yaml` | The MockAgents fixture (model `gpt-4o`, tools, scenarios, streaming). |
| `app/mock_setup.py` | Points the SDK at MockAgents on the **Responses** API + per-conversation session ids. |
| `app/agent.py` | Plain Agents SDK agent (tools + instructions). |
| `app/run_demo.py` | Three conversations through `Runner.run`. |
| `app/streaming_demo.py` | Streamed deltas via `Runner.run_streamed`. |
| `app/previous_response_demo.py` | `previous_response_id` chaining via the raw client. |
| `scripts/smoke.sh` | Curl-only smoke test of `/v1/responses`. |
