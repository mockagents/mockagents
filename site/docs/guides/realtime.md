# Mocking the OpenAI Realtime API

Voice agents are the hardest LLM surface to test: the OpenAI Realtime API is a
WebSocket protocol with dozens of event types, server-side voice-activity
detection (VAD), barge-in, and a tool-calling loop — and every real run costs
audio-model tokens and returns something different.

MockAgents implements the Realtime API (GA event names) over a real WebSocket at
`GET /v1/realtime`, driven by the same `kind: Agent` scenarios you already use
for the HTTP endpoints. The official `openai` SDKs connect to it unchanged —
you get the full streamed response ladder (text, synthesized audio deltas,
transcripts, tool calls) with a deterministic transcript and zero cost.

## Quickstart

The repo ships a ready-made voice agent,
[`examples/realtime-voice-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/realtime-voice-agent.yaml)
(shown abridged below — the full file also declares a `get_weather` tool used
later in this guide).
It's a **normal agent** — there is no realtime-specific `kind` or field. The
WebSocket surface selects the agent by **model name** (default model:
`gpt-realtime`, overridable with `?model=`):

```yaml
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: realtime-voice-agent
spec:
  protocol: openai-chat-completions
  model: gpt-realtime          # the Realtime default model — matched on connect
  behavior:
    scenarios:
      - name: greeting
        match: { content_contains: "hello" }
        response:
          content: "Hi! Great to hear your voice — how can I help today?"
      - name: default
        match: { default: true }
        response:
          content: "I heard you loud and clear. What would you like to do next?"
```

Start the server and connect:

```bash
mockagents start --agents-dir examples
# WebSocket URL: ws://localhost:8080/v1/realtime
```

=== "openai SDK (Python)"

    ```python
    # pip install "openai[realtime]"
    from openai import OpenAI

    client = OpenAI(
        api_key="mock",
        websocket_base_url="ws://localhost:8080/v1",
    )
    with client.realtime.connect(model="gpt-realtime") as conn:
        # A default GA session streams AUDIO output (audio deltas + an audio
        # transcript). Switch to text output so the text-delta filter below
        # actually fires:
        conn.session.update(session={"type": "realtime", "output_modalities": ["text"]})
        conn.conversation.item.create(item={
            "type": "message", "role": "user",
            "content": [{"type": "input_text", "text": "hello"}],
        })
        conn.response.create()
        for event in conn:
            if event.type == "response.output_text.delta":
                print(event.delta, end="")
            if event.type == "response.done":
                break
    ```

=== "Raw WebSocket (any language)"

    Connect to `ws://localhost:8080/v1/realtime?model=gpt-realtime` (offer the
    `realtime` subprotocol). The server greets you, then you drive it with
    client events — one JSON object per text frame:

    ```
    < {"type":"session.created", ...}
    < {"type":"conversation.created", ...}
    > {"type":"session.update","session":{"type":"realtime","output_modalities":["text"]}}
    < {"type":"session.updated", ...}
    > {"type":"conversation.item.create","item":{"type":"message","role":"user",
       "content":[{"type":"input_text","text":"hello"}]}}
    > {"type":"response.create"}
    < {"type":"response.created", ...}
    < {"type":"response.output_text.delta","delta":"Hi! ", ...}
    < ...
    < {"type":"response.done", ...}
    ```

    Without the `session.update`, a default GA session streams audio output
    instead: `response.output_audio.delta` + `response.output_audio_transcript.delta`
    (there is no `output_text.delta` on an audio-modality response).

Every server event carries a unique `event_id`, and responses stream the GA
event ladder: `response.created` → `response.output_item.added` →
`response.content_part.added` → `response.output_text.delta` /
`response.output_audio.delta` / `response.output_audio_transcript.delta` →
the matching `*.done` events → `response.done` (plus `rate_limits.updated`).

!!! note "Audio has no speech-to-text"
    The mock has no STT model. Audio committed through the input buffer always
    transcribes to the fixed placeholder **`[audio input]`** — so voice turns
    land on whichever scenario matches that text (usually your `default`
    scenario). Match real content by sending text items, or design your default
    scenario as the "voice answer".

## Server VAD / turn detection

Turn detection is **on by default** (matching the GA API), with the standard
defaults:

```json
{"type": "server_vad", "threshold": 0.5, "prefix_padding_ms": 300,
 "silence_duration_ms": 500, "create_response": true, "interrupt_response": true}
```

Stream base64 PCM16 audio (24 kHz mono) with `input_audio_buffer.append` and
the mock's energy detector does the rest:

- **Speech start** — when audio energy crosses the threshold, you get
  `input_audio_buffer.speech_started` (with `audio_start_ms` backed off by
  `prefix_padding_ms`).
- **Turn end** — after `silence_duration_ms` of quiet:
  `input_audio_buffer.speech_stopped`, an automatic commit
  (`input_audio_buffer.committed` + the conversation item), and — unless
  `create_response: false` — an automatic model response.
- **Barge-in** — if you start speaking while a response is streaming and
  `interrupt_response` isn't `false`, the in-flight response is cancelled:
  remaining deltas are dropped and `response.done` reports
  `status: "cancelled"` with reason `turn_detected`.

Configure it via `session.update` exactly as with the real API:

```json
{"type": "session.update", "session": {"audio": {"input": {"turn_detection": {
  "type": "server_vad", "threshold": 0.6, "silence_duration_ms": 800,
  "idle_timeout_ms": 6000}}}}}
```

- `turn_detection: null` disables VAD — you then commit manually with
  `input_audio_buffer.commit`.
- `idle_timeout_ms` (server_vad only): if no speech arrives for that long after
  a response, the server emits `input_audio_buffer.timeout_triggered` and a
  prompting response (the turn is recorded as `[silence]`).
- `type: "semantic_vad"` is accepted; its `eagerness` (`low` / `medium` /
  `high` / `auto`) maps to a longer or shorter silence window.
- Invalid values are rejected with an `error` event
  (`invalid_value` + a `param` path like
  `session.audio.input.turn_detection.threshold`).

!!! note "Manual commits need ≥ 100 ms of audio"
    Like the real API, a client `input_audio_buffer.commit` on an empty or
    sub-100 ms buffer returns an `error` event with code
    `input_audio_buffer_commit_empty`. VAD-initiated commits are exempt.

## Tool calls over the WebSocket

Give a scenario `tool_calls` and the realtime surface streams them as
`function_call` output items — the same loop your production voice agent runs:

```yaml
- name: weather
  match: { content_contains: "weather" }
  response:
    content: "Let me check that for you."
    tool_calls:
      - name: get_weather
        arguments: { location: "NYC" }
```

The response emits `response.function_call_arguments.delta` chunks, then
`response.function_call_arguments.done` and the completed item (with `call_id`,
`name`, `arguments`). Your client answers with a `function_call_output` item
and asks for the follow-up:

```json
{"type": "conversation.item.create", "item": {"type": "function_call_output",
 "call_id": "<call_id from the done event>", "output": "{\"temperature\": 72}"}}
{"type": "response.create"}
```

The mock tracks the tool round-trip: after a `function_call_output`, an
identical re-issue of the same call is suppressed so standard SDK tool loops
**converge** instead of spinning — while a deliberately different follow-up
call (multi-step chains) still goes out. A scenario's `raw_arguments` works
here too, streaming malformed argument JSON to test your parser.

## Ephemeral keys (`client_secrets`)

Browser clients shouldn't hold your real API key, so the Realtime API mints
short-lived ephemeral keys. The mock implements both generations:

```bash
# GA endpoint — returns {"value": "ek_...", "expires_at": ..., "session": {...}}
curl -X POST http://localhost:8080/v1/realtime/client_secrets \
  -H "Content-Type: application/json" \
  -d '{"session": {"type": "realtime", "audio": {"output": {"voice": "marin"}}}}'

# Legacy beta endpoint — returns the session object with a nested client_secret
curl -X POST http://localhost:8080/v1/realtime/sessions \
  -H "Content-Type: application/json" -d '{"model": "gpt-realtime"}'
```

GA keys default to a **600 s** expiry (`expires_after.seconds`, clamped to
10–7200); legacy `/sessions` keys expire after 60 s, as in the real API. The
session payload you mint with is **remembered**: connecting with that `ek_` key
seeds the session configuration before `session.created`, so a browser client
needs no `session.update`.

Use the key either way a browser can:

- `Authorization: Bearer ek_...` header, or
- the browser WebSocket subprotocol:
  `Sec-WebSocket-Protocol: realtime, openai-insecure-api-key.ek_...`

## Transcription sessions

Connect with `?intent=transcription` (or mint a
`session: {type: "transcription"}` client secret) for an input-only
transcription session. Committed audio produces the
`conversation.item.input_audio_transcription.delta` / `.completed` ladder with
`usage` — `gpt-4o-transcribe*` models stream word deltas and token usage,
`whisper-1` emits a single delta with duration usage. `response.create` on a
transcription session is rejected, as on the real API.

## Strict mode

By default the mock is lenient about `session.update` field spellings, so both
GA and older beta clients work. Set:

```bash
MOCKAGENTS_REALTIME_STRICT=1 mockagents start --agents-dir examples
```

and any `session.update` field outside the GA schema (e.g. beta-era top-level
`voice`, `modalities`, `turn_detection`) is rejected with an `error` event —
`invalid_request_error`, code `unknown_parameter`, and `param` naming the
offending path. Use it to catch clients still sending beta shapes before a
production migration.

## Quota, logging, and tenancy

Realtime interactions are logged (protocol `openai-realtime`, one row per
response, tool-call counts and scenario names but **no conversation content**)
and count against [per-tenant quotas](management-api.md) in multi-tenant mode.
Because an established WebSocket can't return an HTTP 429, an over-quota
generation surfaces as a `response.done` with `status: "failed"`.

## Troubleshooting

- **"Conversation already has an active response in progress"** — you sent
  `response.create` while a default-conversation response was streaming. Wait
  for `response.done`, or cancel first with `response.cancel`.
- **My audio never triggers VAD** — the energy detector needs real signal:
  silence or near-silence stays below the threshold. Lower
  `turn_detection.threshold`, or verify you're sending base64 PCM16 @ 24 kHz.
- **Voice turns hit the wrong scenario** — remember audio transcribes to
  `[audio input]`; content matching only works for text items.
- **The SDK loop re-runs the same tool forever** — make sure you echo the
  `call_id` in your `function_call_output`; the convergence guard keys off the
  answered call.
- **`error` after `session.update`** — check `error.param` for the exact field
  path; under `MOCKAGENTS_REALTIME_STRICT=1` also check for beta-era field
  spellings.
