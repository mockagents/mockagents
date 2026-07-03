# Strict Tools: Fail Like Production

By default MockAgents is *lenient*: it accepts tool round-trips the real APIs
would reject, ignores `tool_choice`, and doesn't validate `strict: true`
function schemas. That's convenient while authoring fixtures — and dangerous in
CI, where a bug the real API would 400 sails through green.

`strict_tools` closes that gap. Turn it on and the mock enforces the same
request-side rules production enforces, returning **the provider's real 400
bodies** — so your test suite fails where production would fail.

> The YAML block itself is documented in the
> [YAML schema guide](yaml-schema.md#specbehaviorstrict_tools); this page
> covers what each check enforces, the warn/strict semantics, and a CI
> migration recipe.

## Turn it on

Per agent:

```yaml
spec:
  behavior:
    strict_tools:
      level: strict        # off (default) | warn | strict
```

Or fleet-wide, with no YAML changes:

```bash
MOCKAGENTS_STRICT_TOOLS=strict mockagents start --agents-dir ./agents
```

Precedence: an agent's `strict_tools` block **overrides** the env var; the env
var applies to agents without a block; with neither, everything is `off`.
Inside the block, `level` fills the per-dimension booleans you leave unset, a
boolean set to `false` excludes that one check, and a block without a `level`
implies `strict`:

```yaml
strict_tools:
  level: strict
  ids: true           # round-trip tool-id validation
  tool_choice: true   # required/named forcing + parallel cap
  schemas: false      # ...but skip strict:true schema validation
```

A ready-made agent ships at
[`examples/strict-tools-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/strict-tools-agent.yaml).

## What each dimension enforces

### `ids` — round-trip tool-id validation

Real APIs require every tool result to answer a tool call the assistant
actually issued, and every issued call to be answered. Strict mode checks the
conversation history for:

- **orphan** — a `role: "tool"` / `tool_result` message with no preceding
  assistant tool call;
- **unknown id** — a tool result referencing a `tool_call_id` /
  `tool_use_id` / `call_id` the assistant never issued;
- **unanswered** — assistant tool calls that no later message answered (a
  trailing assistant turn awaiting your answer is exempt).

Each provider blames the side it really blames: OpenAI lists the unanswered
call ids (`param: "messages"`), Anthropic points at the unexpected
`tool_use_id` in the `tool_result` block, and Gemini (which has no ids)
reports function call/response count parity.

### `tool_choice` — forcing and the parallel cap

Under `tool_choice: "required"` or a named function, a real API **always**
returns a tool call. Strict mode makes the mock behave the same way:

- A named `tool_choice` not present in the request's `tools[]` → the real 400
  (`Tool choice '<name>' not found in 'tools' parameter.`).
- If the matched scenario didn't emit the forced call, strict **synthesizes**
  it (arguments `{}`) and drops the text content — like the real API, you get
  a tool call, not prose.
- Forced calls on the OpenAI surfaces report `finish_reason: "stop"` — that
  is what the real API returns for forced calls (not `"tool_calls"`); code
  that switches on `finish_reason == "tool_calls"` misses forced calls in
  production, and now misses them against the mock too.
- `parallel_tool_calls: false` caps the response to a single tool call.
- `tool_choice: "none"` is honored everywhere (no tool calls emitted),
  independent of this knob.

### `schemas` — `strict: true` function schemas

Functions declared with `strict: true` must use the structured-outputs schema
subset (e.g. `additionalProperties: false`, all properties required). Strict
mode validates them **at request time** and returns the real
`Invalid schema for function '<name>': ...` 400, so a schema the real API
would reject never silently "works" in tests.

## `off` / `warn` / `strict` semantics

| Level | Checks run | On violation |
|---|---|---|
| `off` *(default)* | no | — |
| `warn` | yes | Request **succeeds**; violations are logged and joined (`"; "`) into one `X-Mockagents-Strict-Violation` response header |
| `strict` | yes | Request fails with the provider's real **400** body |

The warn header is only present when a violation occurred, which makes it a
clean CI signal:

```python
resp = httpx.post(f"{base}/v1/chat/completions", json=payload)
assert "x-mockagents-strict-violation" not in resp.headers, \
    resp.headers.get("x-mockagents-strict-violation")
```

## Error shapes per provider (strict mode)

All violations return HTTP 400 in the provider's own envelope:

| Provider | Envelope |
|---|---|
| OpenAI (Chat Completions + Responses) | `{"error": {"type": "invalid_request_error", "message": ..., "param": ...}}` |
| Anthropic | `{"type": "error", "error": {"type": "invalid_request_error", "message": ...}}` |
| Gemini | `{"error": {"code": 400, "message": ..., "status": "INVALID_ARGUMENT"}}` |

The messages mirror the real APIs verbatim — e.g. OpenAI's
`An assistant message with 'tool_calls' must be followed by tool messages
responding to each 'tool_call_id'. ...`, so error-handling code (and error
snapshot tests) sees production strings.

## Migration recipe for CI

Rolling strict tools onto an existing suite without a big-bang breakage:

1. **Baseline in warn mode.** Set `MOCKAGENTS_STRICT_TOOLS=warn` in CI. Nothing
   fails yet; violations surface as the header.
2. **Assert on the header's absence** in a shared test helper (snippet above),
   or grep your mock's logs. Fix the violations it names — usually a dropped
   `tool_call_id`, a tool result sent before the call, or a `strict: true`
   schema using unsupported keywords.
3. **Flip to strict.** Set `MOCKAGENTS_STRICT_TOOLS=strict` fleet-wide, or
   promote agent-by-agent with per-agent blocks (the block overrides the env,
   so you can hold a stubborn agent at `warn` while the fleet is `strict`).
4. **Keep it on.** New fixtures now fail in CI exactly where production would
   fail.

!!! note "Not the same knob as MCP argument validation"
    MCP `tools/call` argument validation is a separate mechanism that is **on
    by default** for `kind: MCPServer` documents (opt out with
    `spec.strictArgs: false`) — see the [MCP guide](mcp.md#argument-validation-strictargs).
    `strict_tools` governs the LLM HTTP protocols only.

## Troubleshooting

- **400s appear only in CI, not locally** — CI likely sets
  `MOCKAGENTS_STRICT_TOOLS`; run locally with the same value to reproduce.
- **"Tool choice not found in 'tools'"** — the check keys on the *request's*
  `tools[]`, not the agent YAML; make sure your client actually sends the tool
  it forces.
- **A forced call returns empty arguments `{}`** — that's the synthesized
  call (your scenario didn't emit the forced tool). Add a scenario that emits
  the call with realistic arguments if your code parses them.
- **Warn header never shows up** — it's only set when a violation occurred and
  the level is `warn`; in `strict` mode you get the 400 instead.
