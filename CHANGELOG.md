# Changelog

All notable changes to MockAgents are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/).

MockAgents has not cut a tagged release yet; the version headings below mark the
internal **v0.1 → v0.2 → v0.3** development milestones. All three are on `main`.

---

## [Unreleased]

### Changed
- **`mockagents mcp` (HTTP) now binds `127.0.0.1` by default** — per the MCP
  spec's guidance for local servers (the primary DNS-rebinding defence). Pass
  the new `--bind 0.0.0.0` flag to expose it beyond the host, e.g. inside a
  container whose port is mapped out. The `initialize` handshake now also
  **echoes a supported requested `protocolVersion`** instead of always
  answering the newest revision, so pre-2025-11-25 clients no longer fail the
  handshake; capabilities advertise `completions` and `listChanged`.
- **The test runner is now multi-turn** — a `kind: TestSuite` case replays *every*
  user step as a turn in one session (the engine accumulates conversation history
  and per-session turn count), instead of only sending the last user message. This
  makes the trajectory assertions meaningful across a real conversation:
  `tool_call_sequence` / `tool_call_count` now aggregate the tool calls of every
  turn in order, and `node_sequence` aggregates the pipeline nodes across all turns;
  the outcome assertions (`response_contains`, `response_matches`,
  `scenario_matched`, `refusal`) read the final turn. A `node_id`-scoped assertion
  is evaluated against that node in the final turn. Single-step cases are
  unaffected (one turn ⇒ aggregate equals final), so existing suites behave
  identically.

### Added
- **Strict tools mode (`strict_tools`)** — an opt-in "fail like production"
  knob that makes the mock enforce what the real provider APIs enforce and the
  mock was otherwise lenient about. Configure it per agent under
  `spec.behavior.strict_tools` (`level: off | warn | strict`, plus optional
  `ids` / `tool_choice` / `schemas` booleans) or fleet-wide with the
  `MOCKAGENTS_STRICT_TOOLS` environment variable (per-agent YAML overrides the
  env default). Three dimensions: **round-trip tool ids** — an orphan or
  mismatched `tool_call_id` / `tool_use_id` / `call_id` (or a Gemini
  `functionResponse` that matches no prior call), and an unanswered tool call,
  now return each provider's real `400` with its documented message; **tool_choice
  forcing** — `required` / named-function / Anthropic `any`/`tool` / Gemini
  `ANY` now actually force a tool call (synthesized when the scenario emits
  none), reporting `finish_reason: "stop"` on the OpenAI family, and an unknown
  named tool is a `400`; `parallel_tool_calls: false` / `disable_parallel_tool_use`
  caps the response to one call; **strict schemas** — a `strict: true` function
  tool whose parameter schema violates the structured-outputs subset (missing
  `additionalProperties: false`, `required` not covering every key) is rejected
  at request time, exactly like the real API. `off` is the default (existing
  suites are unaffected); `warn` runs every check but only logs and sets an
  `X-Mockagents-Strict-Violation` response header (assert on its absence to
  migrate a suite incrementally); `strict` fails with the provider 400. See the
  [Strict Tools guide](https://mockagents.github.io/mockagents/guides/strict-tools/).
- **MCP `tools/call` argument validation** — a mock `kind: MCPServer` now
  validates call arguments against each tool's `inputSchema` by default (the MCP
  spec requires servers to validate inputs). A wrong-typed or missing-required
  argument returns an `isError: true` execution result with actionable,
  path-qualified feedback — never a `-32602` protocol error — matching the
  2025-11-25 revision and official SDK servers; extra arguments on a permissive
  schema stay accepted. Opt out per server with `spec.strictArgs: false`.
- **MCP bidirectional / admin surface is now served** — the documented
  server→client endpoints `GET /mcp/events`, `POST /mcp/response`,
  `POST /mcp/sample`, and `POST /mcp/roots` (sampling/roots over the Streamable
  HTTP transport) are now mounted by `mockagents mcp`; they previously existed
  only in-library and every request 404'd against a runnable server.
- **Realtime server VAD Phase 3: validation, transcription ladder, idle-loop
  guard** — an invalid `turn_detection` config in `session.update` (unknown
  `type`, `threshold` outside [0,1], negative durations, bad `eagerness`) now
  **rejects the whole update** with a GA-shaped error — `code: "invalid_value"`
  and `param` naming the exact offending field path — instead of being silently
  stored or ignored. With input transcription enabled, a commit now streams the
  full GA transcription ladder: `conversation.item.input_audio_transcription.delta`
  chunks followed by `.completed`, which now carries the **required `usage`**
  field (the `duration` variant, computed deterministically from the decoded
  audio length). The server-VAD idle timeout fires **once per stretch of user
  inactivity** — it does not re-arm after its own triggered response, so a
  silent connection cannot self-prompt forever; fresh user activity re-allows
  it.
- **Realtime server VAD Phase 2: paced responses, barge-in, idle timeout** —
  the `Session` now exposes a deadline API (`NextDeadline()` / `Tick(ctx, now)`;
  it still never spawns a goroutine or arms a timer itself) and the WebSocket
  transport selects between the client's next frame and the session's next
  deadline. On VAD-enabled sessions responses are **paced**: `response.created`
  + `rate_limits.updated` are sent immediately and the rest of the ladder
  streams one event per interval — creating the interruption window that makes
  barge-in real. A VAD speech start now **cancels an in-flight response**
  (`response.done` with `status:"cancelled"`,
  `status_details.reason:"turn_detected"`; the cancelled transcript never enters
  the conversation history) unless `interrupt_response:false`;
  `response.cancel` cancels likewise (`reason:"client_cancelled"`) and only
  errors with `response_cancel_not_active` when nothing is in flight; a second
  `response.create` during one is rejected with
  `conversation_already_has_active_response`. **`idle_timeout_ms`** is honored:
  after a response completes the deadline fires
  `input_audio_buffer.timeout_triggered` (an empty segment), commits a
  null-transcript user item, and auto-runs a follow-up response — the history
  gains a `[silence]` user turn so scenarios can match "are you still there?"
  flows. Documented simplifications: pacing uses a constant inter-event
  interval (StreamingConfig TTFT/ITL physics are a follow-on) and the idle
  deadline is `response.done` + `idle_timeout_ms` (no playback-duration
  offset). Burst emission remains the default for non-VAD sessions.
- **Realtime server VAD / turn detection (Phase 1)** — when a client enables
  `turn_detection` via `session.update`, the appended audio now drives a real
  (deterministic, pure-Go) energy detector over the PCM16 payload: speech onset
  emits `input_audio_buffer.speech_started` (with `audio_start_ms` net of
  `prefix_padding_ms`, pre-announcing the future item id), and
  `silence_duration_ms` of accumulated low-energy audio ends the turn —
  `input_audio_buffer.speech_stopped`, the same auto-commit ladder a manual
  commit produces (carrying the pre-announced item id), and, unless
  `create_response:false`, the auto-response. `threshold` is honored (mean
  absolute PCM16 amplitude, 0..1); `semantic_vad` maps `eagerness` to the
  silence window via the documented max timeouts (high 2 s, auto/medium 4 s,
  low 8 s); non-PCM payloads count as speech so synthetic test audio always
  "speaks". A stock GA voice client that only streams audio now gets the full
  server-driven turn loop instead of hanging. Deviations (documented in
  `docs/design/realtime-server-vad.md`): the session default remains
  `turn_detection: null` (GA defaults VAD on — flipping would break
  manual-commit flows), and `interrupt_response` / `idle_timeout_ms` are
  accepted but inert until the Phase 2 deadline-driven model.
- **A2A streaming + richer messages** (NF-04 depth) — the mock A2A server now
  serves **`message/stream`** over Server-Sent Events (it previously returned
  `-32601` and the Agent Card force-disabled streaming): a request streams a
  `Task` → `status-update` (working) → `artifact-update` → final `status-update`
  (`final: true`) sequence, each frame a JSON-RPC result, and the card now
  advertises `capabilities.streaming`. `message/send` can return a **bare
  `Message`** instead of a `Task` (the A2A result is `Task|Message`) when a
  response sets `as_message: true`. Messages and artifacts now model all three
  A2A **Part** kinds — `text`, `file` (bytes/uri), and `data` (structured JSON):
  incoming file/data parts round-trip into task history, and a response's
  `data:` field emits a `data` Part alongside the text. `examples/a2a-server.yaml`
  shows the data part.
- **Realtime mid-session function calls + text mode** (NF-01 depth) — a Realtime
  `response.create` now emits a `function_call` output item per tool call the
  scenario produced, with the streamed `response.function_call_arguments.delta` /
  `.done` events a voice agent's tool loop consumes (call_id + name + assembled
  JSON arguments). A response carries both an assistant message **and** its
  function calls; a content-free tool-call turn skips the message item. The
  session also honors **`output_modalities`** — `["text"]` streams a
  `response.output_text.delta`/`.done` ladder with an `output_text` content part
  instead of audio — and the session object is the **GA shape**: `type:
  "realtime"`, top-level `output_modalities` / `instructions` / `tools` /
  `tool_choice` / `max_output_tokens` (default `"inf"`), an `expires_at`, and a
  nested `audio` object whose `input`/`output` carry the format, transcription,
  turn_detection, **voice** (at `audio.output.voice`, not top level) and speed.
  `session.update` is accepted in either the GA-nested or the beta-flat form, so
  a client's settings round-trip to their GA locations. Committed
  input audio emits `conversation.item.input_audio_transcription.completed` when
  the client configured `input_audio_transcription` (otherwise the item transcript
  is null — a mock has no STT). Barge-in (server-VAD `speech_started`/`stopped`,
  mid-stream `response.cancel`) remains a follow-on: it needs an async streaming
  model that the current synchronous event-in/events-out session does not have.
- **Three cheap test-runner assertions** for `mockagents test` — `no_tool_call`
  (the response made no tool calls — the common "agent shouldn't have used a tool
  here" check), `refusal` (the response is an assistant refusal, optionally
  containing a `value` substring), and `response_matches` (the response content
  matches a `value` **regular expression**, where `response_contains` is
  substring-only). Pure runner additions.
- **`tool_call_args` assertion** — asserts a named tool was called with arguments
  matching every given entry, where `tool_call`'s subset match cannot: keys may be
  **dotted paths** into nested argument objects (`filters.class`) and values
  compare **type-tolerantly** (a YAML `2` matches the JSON `2.0` arguments take on
  the wire). Passes when some call to `tool` matches all entries. Dependency-free
  (no JSONPath).
- **`tool_error` / `handles_tool_error` assertions** — test agent error-handling
  against the mock's existing tool-error injection (`tools[].responses[].error`
  and `error_rate`). `tool_error` asserts a simulated tool result was an error
  (optional `tool` name; optional `value` matching the error code or message);
  `handles_tool_error` asserts the agent **recovered** — an error occurred earlier
  in the (multi-turn) trajectory yet the final turn is a clean answer (non-empty,
  not a refusal, not itself ending in an error), with an optional `value` the
  recovery text must contain. The runner now surfaces each turn's simulated
  `ToolResults` (aggregate + final-turn) so these read real injected errors.

### Fixed
- **The tool-call loop now converges on every protocol** — the canonical agent
  loop (ask → model returns a tool call → client sends the tool result → model
  answers) previously never converged against the mock: scenario matching keys
  on the last user message, so a request whose tail was a tool result re-matched
  the original question and re-issued the *same* tool call forever. This
  affected OpenAI Chat Completions (and Azure), the Responses API, Gemini, and
  Anthropic. The engine now detects a tool-result tail and consumes an identical
  re-issue so the loop terminates naturally (`finish_reason` / `stop_reason`
  falls out per provider), while genuinely different or multi-step calls still
  go out. `tool_choice: "none"` is now honored on every surface (scenario tool
  calls are suppressed), and a batch of wire-shape fixes make no-argument calls
  render `{}` (Anthropic `tool_use.input` is a required key; OpenAI/Responses
  emit `"{}"` not `"null"`), Anthropic tool-call ids carry the `toolu_` prefix,
  `stream_options.include_usage` emits the final usage chunk, Gemini attaches
  `finishReason` to the last content-bearing chunk, and the OpenAI error
  envelope renders `param: null`. Verified against the live `openai`,
  `anthropic`, and `google-genai` SDKs.
- **MCP Streamable HTTP spec conformance** — a batch of fixes to the mock MCP
  server: spec-accurate JSON-RPC error codes (`-32602` for an unknown tool name
  or bad arguments, `-32002` for an unknown resource URI, `-32600` for a batch
  body, `-32602` for an unknown pagination cursor or a missing required prompt
  argument); id-less requests are treated as notifications and never answered;
  error responses whose id can't be determined render `"id": null` instead of
  omitting the member; a session allows at most one concurrent server→client
  `GET` stream (a second is `409 Conflict`); an over-sized stdio frame yields a
  parse error and the loop keeps serving instead of the process exiting; and
  schema-less tools / nameless resources always carry the required `inputSchema`
  / `name` members so a strict SDK client can parse the whole list.
- **Realtime fidelity, live-SDK verified** — a sweep of the Realtime WebSocket
  surface validated against `openai-python` over a real socket (zero schema
  validation errors across ~250 events): server VAD now defaults **on** per the
  GA API; VAD auto-commit no longer trips the 100 ms minimum-commit floor; the
  Realtime tool loop converges (an identical `function_call` is not re-issued
  after its `function_call_output`); per-session **quota enforcement and
  interaction logging** now apply to Realtime connections (through adapter
  hooks, since a long-lived socket bypasses the HTTP middleware chain);
  ephemeral `client_secrets` keys seed the session config; transcription-only
  sessions are gated correctly; and several GA error/event shapes were
  corrected. A strict-mode env knob (`MOCKAGENTS_REALTIME_STRICT`) rejects
  unknown `session.update` fields with a GA-shaped `unknown_parameter` error.
- **Realtime round-4 fidelity batch** (4th adversarial eval, verified against
  the GA SDK types) — **VAD:** the GA `threshold` scale (0..1 activation) was
  compared directly against mean-abs PCM16 amplitude, so **real microphone
  audio never triggered speech detection** (typical speech averages ~0.08 of
  full scale vs the 0.5 default); a new amplitude mapping makes GA-typical
  values work on realistic levels. `speech_stopped.audio_end_ms` now includes
  the silence window per GA; `idle_timeout_ms` arms only under `server_vad`.
  **Out-of-band concurrency:** GA allows `conversation:"none"` responses to run
  in parallel with the default-conversation response — the mock rejected all
  concurrency, breaking the guardrail/side-response pattern. OOB responses are
  now burst-emitted, never occupy the in-flight slot, and can no longer be
  cancelled by VAD barge-in (GA scopes `interrupt_response` to
  default-conversation responses). `response.cancel` honors `response_id`.
  **Cancellation:** an interrupted response now closes out its in-progress item
  (the `*.done` events fire, the item is re-stamped `incomplete`, listed in the
  cancelled `output`, and retrievable as incomplete) instead of dropping the
  ladder; cancelled and failed `response.done` carry `usage`. **Responses:**
  per-response `audio.output.{voice,format}` and `max_output_tokens` overrides
  are honored and echoed; an integer cap is enforced (trimmed transcript,
  status `incomplete`, `status_details.reason:"max_output_tokens"`).
  **Items:** `conversation.item.create` acks echo the client's content **as
  sent** (input_audio/input_image/multi-part/assistant `output_*` parts survive
  instead of being rebuilt as a single `input_text`); truncation is restricted
  to assistant message items with audio content. **Session/endpoints:**
  `session.created` reports the server-default instructions when the client set
  none; `conversation.created` is emitted after `session.created`; the
  `client_secrets` response session embeds the required `client_secret`
  {value, expires_at} and the `audio.input` block, echoes requested
  `output_modalities`/`tools`/`tool_choice`/`turn_detection`, and defaults to
  the GA-documented **600 s** expiry; legacy `POST /v1/realtime/sessions`
  returns the pre-GA shape (the session object itself) instead of the GA
  envelope.
- **Realtime polish batch (round-3 eval F10/F15–F18)** — (F10) a client-supplied
  `item.id` on `conversation.item.create` is honored (pre-generated ids can now
  be truncated/deleted/retrieved; duplicates rejected with `param: "item.id"`),
  and the event's `previous_item_id` places the item: `"root"` inserts first, a
  known id inserts after it (the `previous_item_id` chain tail only moves for
  appends), an unknown id errors `item_not_found` — engine history stays
  append-order (documented mock simplification). (F15) the `response` envelope
  now carries `usage` (null until `response.done`), `audio.output`
  (voice + format), and `max_output_tokens`. (F16) `usage.input_token_details`
  gains `image_tokens` and `cached_tokens_details` (text/audio/image). (F17)
  `session.update` now stores and echoes `tracing`, `truncation` (default
  `"auto"`), `prompt`, `include`, `parallel_tool_calls` (default `true`), and
  `audio.input.noise_reduction`, and accepts the beta-flat aliases
  `turn_detection` (validated like the GA nested form) and
  `input_audio_format`/`output_audio_format` (`"pcm16"`/`"g711_ulaw"`/
  `"g711_alaw"` translated to GA format objects). (F18)
  `POST /v1/realtime/client_secrets` reads the GA request shape —
  `session.audio.output.voice`, `session.instructions`, and
  `expires_after.seconds` (clamped 10–7200 s, default 60) — and returns a fuller
  GA session stub.
- **Realtime `response.create` inline overrides** (round-3 eval) — the inline
  `response` payload was silently ignored. The mock now honors per-response
  `instructions` (override the session system prompt for one turn),
  `output_modalities` (switch one response between the audio and text ladders;
  beta `modalities` alias accepted), `metadata` (echoed verbatim on the
  `response.created`/`response.done` envelope — the field clients route
  side-responses by), and **`conversation: "none"`** (out-of-band responses: the
  response joins no conversation — `conversation_id` is `null`, no
  `conversation.item.*` mirror is emitted, the transcript does not become
  context for later turns, and the `previous_item_id` chain is untouched).
  Per-response `tools` and custom `input` context remain unsupported (the
  engine's tools come from the agent definition). The response envelope now also
  always carries `metadata` (null unless set).
- **Realtime conversation-item operations** (round-3 eval) —
  `conversation.item.truncate` (the barge-in primitive every interruption-capable
  WS client sends), `conversation.item.retrieve`, and `conversation.item.delete`
  previously returned an `unknown_event` error. The session now indexes every
  completed conversation item: truncate acks `conversation.item.truncated`
  (echoing `item_id`/`content_index`/`audio_end_ms`) and drops the truncated
  audio transcript from the stored item; retrieve returns
  `conversation.item.retrieved` with the item; delete acks
  `conversation.item.deleted`. Unknown ids get an `item_not_found` error rather
  than `unknown_event`. (Mock simplification: deletion does not rewrite the
  engine history already accumulated.)
- **Realtime error fidelity** (round-3 eval) — three gaps in how failures reach
  a GA client: (1) a response that cannot be generated now closes the ladder —
  `response.done` is **always** emitted (status `failed` +
  `status_details.error`, preceded by a `server_error` event carrying the
  detail) instead of a bare error that left clients awaiting `response.done`
  hanging; (2) `response.cancel` now returns the cancel-specific
  `response_cancel_not_active` error (which SDKs recognize and suppress) instead
  of `unknown_event`; (3) error objects now carry the full GA shape — `param`
  (null) and `error.event_id` echoing the id of the client event that caused the
  error, the correlation handle SDKs match errors to requests with.
- **Realtime tool loop: `function_call_output` items now work** (round-3 eval) —
  a client answering a `function_call` with `conversation.item.create`
  `{type:"function_call_output", call_id, output}` previously had the item
  mangled into an empty user *message* (its ack carried the wrong item shape)
  and the output never reached the engine, so follow-up scenario matching could
  not see tool results. The item is now parsed by type (mirroring the Responses
  adapter's mapping): the output joins history as a `tool` turn and the ack
  echoes the real `function_call_output` item (`call_id` + `output`). Echoed
  `function_call` items (context replay) are likewise acked with their real
  shape instead of a message rewrite.
- **Realtime GA wire-shape corrections** (round-3 eval, verified against the GA
  SDK types): the `response.content_part.added`/`.done` events' `part.type` now
  uses the GA short names `"text"`/`"audio"` (item *content* correctly keeps
  `output_text`/`output_audio` — the GA API is asymmetric here); the default
  `output_modalities` is now `["audio"]` (GA responses are only ever `["audio"]`
  or `["text"]`, never both); and the spurious `content_index` field added to
  `response.function_call_arguments.delta`/`.done` by the round-2 sweep is
  removed (a `function_call` item has no content parts; the GA event types omit
  it).
- **Realtime GA `conversation.item.added` / `conversation.item.done`** — the mock
  announced a new conversation item with the beta `conversation.item.created`
  event; the GA dialect replaced it with an `added` → `done` pair. Both are now
  emitted (each carrying `previous_item_id` and the completed item) after a
  client `conversation.item.create` and on `input_audio_buffer.commit`. Response
  output items are mirrored too: each `response.output_item.added`/`.done` in the
  response ladder is now followed by the matching `conversation.item.added`
  (status `in_progress`) / `conversation.item.done` (finalized) — so a client
  tracking the conversation from conversation-item events sees model turns as
  well as user turns.
- **Realtime `previous_item_id`** — the `input_audio_buffer.committed` and
  conversation-item server events now carry `previous_item_id` (the id of the
  prior conversation item, or `null` for the first item), a field GA SDKs use to
  order the conversation. The id chains across item creates, committed audio
  buffers, and response output items, so a turn following a model response
  correctly points back at that response's item rather than the last user item.
- **A2A stream no longer silently drops a frame** — on a (defensive) JSON-marshal
  failure mid-`message/stream`, `serveStream` now aborts the SSE stream instead of
  `continue`-ing, which could emit a stream that looks complete but is missing a
  frame (including the terminal `final: true` status-update).
- **Fidelity polish (round-2 eval, minor gaps)** — Realtime: `response.done`
  usage adds the GA per-modality
  `input_token_details`/`output_token_details`; and the `response` envelope
  (created + done) now carries `output_modalities`, `conversation_id`, and
  `status_details`. Runner: numeric tolerance in `tool_call_args` now covers every
  int/uint/float kind plus `json.Number` (not just `int`/`float64`), and the
  validator now **rejects `node_id`** on the whole-run aggregate assertions
  (`node_sequence`, `tool_error`, `handles_tool_error`, `latency_ms_lt`) instead
  of silently ignoring it. Docs note that `handles_tool_error` is a multi-turn
  check and which assertions accept `node_id`. A2A: corrected an overstated
  "`preferredTransport` is required" comment (it is optional in the v0.3 schema).
- **Fidelity polish (round-2 eval)** — three spec-conformance gaps in the new
  surfaces. (1) Every Realtime **server event now carries a unique `event_id`**
  (a required field on all Realtime events; previously omitted, which trips
  strict SDK models) — stamped at one choke point so every emit path is covered,
  including the adapter's JSON-parse error. (2) The A2A **Agent Card defaults the
  required `version` and `description`** when a document omits them, and `skills`
  now always renders as a JSON array (never null/omitted) — so a minimal card is
  still spec-valid. (3) The TestSuite **schema and validator agree on `max_ms`**
  (schema now `minimum: 1`, matching the validator's positive-value rule).
- **TestSuite validation rejected the NF-03 trajectory assertions** — the
  validator and the JSON schema only knew the four original assertion types, so
  `mockagents validate` (and the GUI/schema check) flagged a suite using
  `tool_call_count`, `tool_call_sequence`, or `node_sequence` — the very
  assertions NF-03 shipped — as `unknown assertion type`. The validator and
  `schema/mockagents-v1-testsuite.json` now recognize all assertion types
  (including the new behavioral ones) and validate their type-specific fields
  (`count` ≥ 0, non-empty `sequence`, compilable `response_matches` regex).
- **Realtime API now speaks the GA wire dialect** — the mock advertises the GA
  model `gpt-realtime` but was emitting the deprecated **beta** response events
  (`response.audio.delta` / `response.audio_transcript.delta` / `…done`) and the
  beta `type:"audio"` content part. OpenAI removed the beta interface in May 2026,
  so a current SDK received **no audio or transcript**. The response ladder now
  uses the GA names — `response.output_audio.delta`,
  `response.output_audio_transcript.delta`, their `.done` counterparts, and the
  `output_audio` content-part type — and emits the standard `rate_limits.updated`
  event at the start of each response.
- **Conversations API fidelity** — three bugs that broke the OpenAI SDK's stateful
  thread flow. (1) The Responses object now echoes `conversation: {"id": …}` so an
  SDK can read `response.conversation.id` to continue a thread (it was omitted
  entirely). (2) `GET /v1/conversations/{id}/items` now honors `limit`,
  `order` (defaulting to `desc`, newest-first, like the real API), and the `after`
  cursor, and reports an accurate `has_more` (it previously returned every item in
  insertion order with `has_more` hardcoded `false`). (3) A turn with `store:false`
  still appends its input + output to the referenced conversation — conversation
  items have their own lifecycle and are not gated by the Responses `store` flag
  (the next turn was silently losing history).
- **A2A Agent Card now satisfies the v0.3 spec** — the served card was missing the
  **required** `preferredTransport` field (spec-strict clients reject a card
  without it); it is now filled with `"JSONRPC"` at serve time. Each skill's `tags`
  is also a required field that must be a JSON array — a skill that left it unset
  rendered `null`, so the server now normalizes it to `[]`. The declared
  definition is never mutated by this normalization.

### Documentation
- **Architecture documentation rebuilt** — `ARCHITECTURE.md` (also published on
  the docs site) is a full rewrite with seven verified Mermaid diagrams: system
  context, a component map with the package import-direction rules, and sequence
  diagrams for the Chat Completions request flow, the Realtime session
  (server VAD, barge-in), and the MCP Streamable HTTP transport, plus a
  cross-cutting chaos / strict-tools view and a data / deployment view.
- **New user guides** for the Realtime API, MCP servers, A2A agents, strict
  tools, and chaos / fault injection, plus TypeScript and Go SDK reference
  pages joining the Python one; the existing pages were swept for stale
  commands, flags, and YAML fields against the current code.

## [0.4.0] - 2026-06-17

### Added
- **Mock A2A (Agent2Agent) servers** (NF-04) — a new `kind: A2AServer` document
  and a `mockagents a2a` command that serve Google's agent-to-agent protocol
  (now Linux-Foundation-governed), mirroring how `kind: MCPServer` mocks MCP. The
  server publishes the declared **Agent Card** at `/.well-known/agent-card.json`
  (URL/protocolVersion/modes filled in at serve time) and answers the JSON-RPC
  surface at `POST /`: `message/send` runs the document's canned, match-based
  responses and returns a `Task` (status + artifact + history), `tasks/get`
  retrieves it, and `tasks/cancel` cancels a non-terminal task (terminal tasks
  return the A2A `TaskNotCancelable` error). Loader + validator + single-file
  `validate` dispatch were extended for the new kind; an `examples/a2a-server.yaml`
  shows a weather agent. Streaming (`message/stream`), push notifications, and
  signed cards are documented follow-ons.
- **OpenAI Realtime API over WebSocket** (NF-01) — the first WebSocket transport
  in the mock, for testing **voice agents** deterministically and offline.
  `GET /v1/realtime` upgrades to a WebSocket speaking the Realtime event protocol,
  and `POST /v1/realtime/client_secrets` (and the legacy `/v1/realtime/sessions`)
  mints an ephemeral session token. On connect the server emits `session.created`;
  it handles `session.update`, `conversation.item.create` (text input),
  `input_audio_buffer.append`/`commit`/`clear`, and `response.create`. A
  `response.create` runs the agent's scenario engine on the accumulated
  conversation and streams the full response event ladder (`response.created` →
  `output_item.added` → `content_part.added` → `audio_transcript.delta` +
  `audio.delta` … → `response.done`). Audio is **synthesized deterministically**
  (a mock has no TTS — stable base64 PCM16 derived from the transcript), and the
  transcript is whatever the matched scenario produces, so a voice-agent test is
  fast, free, and reproducible. Built on `github.com/coder/websocket` (pure-Go,
  zero transitive deps). Mid-session tool calls, barge-in/interruption, and
  WebSocket fault injection are documented follow-ons.
- **Agent-trajectory test assertions** (NF-03) — the `mockagents test` runner
  gains three assertions that target the most common 2026 agent bugs (wrong tool,
  wrong count, wrong order):
  - `tool_call_count` — the exact number of tool calls in the response
    (`count:`; a pointer so `count: 0` asserts "no tool calls");
  - `tool_call_sequence` — the ordered list of tool-call names (`sequence:`);
  - `node_sequence` — the ordered pipeline node ids that ran, for `target:
    pipeline:` suites (`sequence:`) — a multi-agent planning/trajectory check.
  (The existing `tool_call` assertion already does subset/partial argument
  matching.) Pure additions to the runner + TestSuite schema; existing suites
  are unaffected.
- **OpenAI Conversations API** (NF-02) — the stateful companion to the Responses
  API and the replacement for Assistants Threads (the Assistants API sunsets
  2026-08-26). `POST/GET/POST(update)/DELETE /v1/conversations` plus the
  `/v1/conversations/{id}/items` list/create/get/delete sub-resource, backed by a
  per-tenant bounded-FIFO store. The Responses API gains a `conversation` param (a
  string id or `{"id": …}`): the conversation's stored Items are replayed as prior
  turns, and — when `store` is not false — each turn's input and output are
  appended back to it, so a multi-turn loop carries state by passing a conversation
  id instead of chaining `previous_response_id` (the two are mutually exclusive).
  The item→message mapping is shared with the inline Responses input parser, so
  replaying a conversation and sending the same items inline produce identical
  history.
- **Manage MockAgents agents over MCP** (MCP-03) — `mockagents mcp --manage` now
  exposes the agent write API as built-in MCP tools, so an MCP client (e.g. an AI
  coding agent) can manage your mock fixtures over the Model Context Protocol:
  - **`list_agents`** — name, model, protocol, and scenario count of every served
    agent;
  - **`get_agent`** — an agent's canonical YAML by name;
  - **`validate_agent`** — validate a definition (YAML or JSON) without persisting
    it, returning the same report the CLI/editor use;
  - **`create_agent`** — create a new agent (conflict-checked); it serves
    immediately and persists to `--agents-dir`;
  - **`put_agent`** — create-or-replace;
  - **`delete_agent`** — stop serving and remove the persisted file.
  The tools reuse the write API's validation, canonicalization, path-safety, and
  persist-in-place semantics. `--manage` works even with no `kind: MCPServer`
  document (it serves a synthetic `mockagents-admin` server), and composes with a
  declarative server's own tools when one is selected. Under the hood the MCP
  server gained a generic `RegisterTool(spec, handler)` hook so a tool's
  `tools/call` can be backed by Go code (a domain failure returns an `isError`
  result; an unexpected fault maps to a JSON-RPC error) instead of only the
  declarative canned responses. MCP-side authentication/tenancy (managed agents
  are owned by the single-tenant namespace today) is a documented follow-on.
- **Anthropic Message Batches API** (A-08) — the asynchronous, inline sibling of
  `/v1/messages`, completing the Batch surface alongside the OpenAI Files+Batch
  API. `POST /v1/messages/batches` takes its requests inline
  (`{"requests":[{"custom_id","params"}, …]}` — no Files prerequisite), processes
  the whole batch eagerly and deterministically by replaying each request's
  `params` through the live `/v1/messages` handler (so a batched request is
  byte-for-byte the same as the synchronous one), and exposes `GET`/list/cancel/
  delete plus `GET /v1/messages/batches/{id}/results` (JSONL). `processing_status`
  (`in_progress` → `ended`, or `canceling` → `ended` after a cancel) is derived
  from elapsed time vs. an optional `X-Mockagents-Batch-Delay-Ms` so a poll loop
  observes the lifecycle without any background goroutine; `request_counts`
  tallies succeeded/errored (and canceled on cancel). The whole batch is validated
  up front (non-empty, ≤100k requests, unique non-empty `custom_id`s, params
  present), and the store is per-tenant bounded-FIFO. Streaming is forced off on
  batched requests so it can't corrupt the JSONL framing.
- **MCP conformance badge + CI** (M-02) — a new `mcp-conformance` workflow runs
  the official [`@modelcontextprotocol/conformance`](https://www.npmjs.com/package/@modelcontextprotocol/conformance)
  server suite against the mock's Streamable-HTTP `/mcp` endpoint on every change
  to the MCP code or fixture. It serves `conformance/server/` and gates the run
  with `conformance/expected-failures.yml`, so a new regression — or a baselined
  scenario that starts passing (stale entry) — fails CI. All static-content
  scenarios pass; the baseline holds only the flows a static declarative mock
  can't model (server-initiated sampling / elicitation / progress / log
  notifications mid-call, and stateful URI templates). A `conformance-validated`
  badge links the workflow. To reach that bar the MCP server gained:
  - **embedded-resource content blocks** — a `type: resource` tool/prompt block
    now serializes to the spec's `{type:"resource", resource:{uri,mimeType,text,blob}}`
    EmbeddedResource shape (previously the fields were emitted flat on the block);
  - **`audio` content blocks** — `{type:"audio", data, mimeType}` is now a valid
    content type alongside `text`/`image`/`resource`;
  - **`resources/subscribe` + `resources/unsubscribe`** — the server tracks the
    subscribed URI set and returns `{}`, and advertises `resources.subscribe` in
    `initialize`;
  - **prompt argument interpolation in resource URIs** — `{{arg}}` placeholders
    are now expanded in a content block's `uri` (not only in `text`), so a prompt
    can embed a resource whose URI is supplied as an argument.
- **`setup-mockagents` GitHub Action — source builds + self-test** (E-03) —
  the `deploy/actions/setup-mockagents` and `deploy/actions/mockagents-test`
  composite actions gain a `source-path` input that builds the CLI from a local
  checkout (`go build ./cmd/mockagents`) instead of `go install …@latest`. This
  lets a workflow test its own working tree (`source-path: ${{ github.workspace }}`)
  and unblocks CI before a release tag exists. A new `actions-selftest.yml`
  workflow exercises both actions end-to-end against the repo's `examples/`
  (start → exported `OPENAI_BASE_URL` round-trips a chat completion; validate →
  `kind:TestSuite` → JUnit report), so a regression in the action wiring fails
  CI instead of every downstream consumer. Added a `README.md` for
  `setup-mockagents` and hardened both install steps to pass inputs via `env:`
  rather than inline `${{ }}` interpolation. (Marketplace publishing remains a
  release-time step.)
- **`@mockagents/vitest` test-runner helper** (E-02) — a new package
  (`sdk/vitest`) that auto-spawns the MockAgents server once per test file and
  redirects the OpenAI / Anthropic / Gemini SDK base-URL (and dummy key)
  environment variables at it, mirroring the Python `pytest` plugin's
  zero-change ergonomics:
  - `setupMockAgents(options)` registers a `beforeAll` that starts the server on
    an auto-selected free port and patches the provider env, and an `afterAll`
    that restores the env and stops the server. The returned handle exposes
    `.url` / `.server` / `.client`.
  - A `mockagentsFixture(handle)` helper layers idiomatic Vitest fixtures
    (`mockagents`, `mockagentsClient`) on top for fixture-injection style.
  - A `@mockagents/vitest/jest` subpath provides the same `setupMockAgents`
    wired to Jest's global hooks (importing it pulls in no Vitest dependency).
  - Options extend `MockAgentServerOptions` with `patchEnv` (default `true`),
    extra `env` (merged after, and overriding, the provider patch; restored
    afterwards), and `startTimeoutMs`. Reuses the SDK's `MockAgentServer` /
    `MockAgentClient` rather than reimplementing process spawn/health logic.
    (npm publish under the `@mockagents` scope is a follow-on release step.)
- **Streamable HTTP MCP transport** (M-01) — `mockagents mcp --transport http`
  now serves the current MCP Streamable HTTP revision on a single `/mcp`
  endpoint instead of the legacy POST-only JSON handler:
  - **POST** — send one JSON-RPC message. A request is answered either with a
    single `application/json` body or, when the client sends
    `Accept: …text/event-stream`, as a short SSE stream carrying the response as
    a `message` event before closing. A notification or response (no `id`) is
    acknowledged with `202 Accepted` and no body.
  - **GET** — opens a resumable server→client SSE stream. Every event carries a
    monotonic `id:`; a reconnecting client replays only the events it missed by
    sending `Last-Event-ID`.
  - **DELETE** — terminates the session.
  - **Sessions** — the server mints an `Mcp-Session-Id` on `initialize` and
    returns it on that response; later requests must echo it (an absent header
    is `400`, an unknown/expired id is `404` so the client reinitializes).
  - **Hardening** — `Origin` is validated to defend against DNS rebinding
    (disallowed origin → `403`; loopback always allowed), the
    `MCP-Protocol-Version` header is validated on post-init requests
    (unsupported → `400`), POST bodies are size-capped, the session table and
    per-session event log are bounded, and concurrent GET streams per session
    are capped (excess → `429`).
  - The default advertised protocol version is bumped to **`2025-11-25`**, with
    `2025-06-18` / `2025-03-26` / `2024-11-05` still accepted in the
    `MCP-Protocol-Version` header. A new `/mcp/notify` admin endpoint pushes a
    server notification onto every live session's GET stream. The legacy
    POST-only JSON transport remains available at **`/mcp/rpc`**. (Server-
    initiated `sampling`/`roots` over the streamable stream and JSON-RPC
    batching are documented follow-ons.)
- **OpenAI Files + Batch API** (A-08) — the asynchronous, file-driven sibling of
  the per-request endpoints, so a client can run the full
  upload → create → poll → download flow against the mock:
  - **Files API** — `POST /v1/files` (multipart upload with a `purpose`),
    `GET /v1/files` (with the `purpose` filter), `GET /v1/files/{id}`,
    `GET /v1/files/{id}/content` (raw bytes), and `DELETE /v1/files/{id}`. An
    in-memory, per-tenant store (bounded FIFO) backs both the uploaded request
    JSONL and the batch-generated output/error files.
  - **Batch API** — `POST /v1/batches`, `GET /v1/batches`, `GET /v1/batches/{id}`,
    and `POST /v1/batches/{id}/cancel`. The input file is processed **eagerly and
    deterministically** at create time: each JSONL line
    (`{custom_id, method, url, body}`) is replayed through the **live** endpoint
    handler it names, so a batched request is byte-for-byte the same as the
    synchronous one. Supported endpoints: `/v1/chat/completions`,
    `/v1/embeddings`, `/v1/responses`.
  - Dispatched results are written to an `output_file` (one JSONL line per
    request, with the sub-response's `status_code` and `body`); lines that can't
    be dispatched at all (malformed JSON, missing/duplicate `custom_id`, an
    endpoint that doesn't match the batch) go to an `error_file`, and the
    `request_counts` (`total`/`completed`/`failed`) tally both.
  - **Simulated lifecycle** — status is derived from elapsed time on every read
    (no background goroutine): batches complete immediately by default, or stay
    `in_progress` until an optional `X-Mockagents-Batch-Delay-Ms` header elapses,
    so a poll loop can observe the non-terminal state. `cancel` works while a
    batch is in flight. Streaming is forced off on batched requests (the real
    Batch API rejects it; an SSE body would also break the JSONL framing).
- **Per-framework "Testing with MockAgents" guide** (DOC-01) — a new
  [Testing with Agent Frameworks](site/docs/guides/framework-testing.md) guide
  with copy-pasteable, demo-grounded recipes for the agent frameworks that have
  no official mock story: OpenAI Agents SDK (Responses + Chat Completions),
  Anthropic Claude Agent SDK (CLI subprocess + MCP-namespaced tools), Google ADK
  (native Gemini + LiteLLM bridge), CrewAI (`crewai_mock_llm` adapter), and
  LangChain/LangGraph (`chat_openai`/`chat_anthropic`/`patched_env` adapters).
  Covers the per-framework redirect mechanism, the `/v1`-suffix-vs-root gotcha,
  loop-termination via `X-Session-Id` (or content markers), and how to assert
  (TestSuite, the `mockagents` pytest fixture, the interaction log). Linked from
  the README docs index and the mkdocs nav.
- **Connection-layer fault injection** (FB-03 slice 5, completing the FB-03
  failure-injection catalog) — a new `chaos.connection` block faults the request
  at the TRANSPORT layer, before any HTTP response is written, by hijacking the
  TCP connection:
  - `mode: reset` (alias `peer-reset`) — sends a TCP RST (client sees "connection
    reset by peer").
  - `mode: empty` — closes with no bytes (client sees an empty reply / EOF).
  - `mode: random` (aliases `random-then-close`, `garbage`) — writes non-HTTP
    garbage bytes then closes (client sees a malformed response).

  Triggered by `rate` (probability) or `fail_first` (the first N requests, then
  recover) with the same semantics as `chaos.errors`; on HTTP/2 (no hijack) the
  server falls back to a 502. Adds the `connection-reset` preset and
  `examples/connection-fault-agent.yaml`. This is the transport-level complement
  to the existing HTTP-status faults (`chaos.errors`) and mid-stream faults
  (`streaming.truncate_after_chunks` / `malformed`). (Also fixes the
  InteractionCapture `captureWriter` to implement `Unwrap` so
  `http.ResponseController` can reach the connection through the full middleware
  chain.)
- **Cassette importers** (R-05, completing record/replay v2) — convert existing
  recordings into a MockAgents cassette that `mockagents replay` serves:
  - `mockagents import vcr <cassette.yaml>` — import a vcrpy (Python) YAML
    cassette. Handles vcrpy's body shapes (scalar string, `{string: ...}`,
    `{base64_string: ...}` including gzip'd, capped at 32 MiB against
    decompression bombs) **and parsed-JSON request bodies** (vcrpy's JSON
    serializer renders the body as a YAML mapping — re-encoded to JSON so it
    imports and hash-matches). By default only POSTs to known LLM paths are kept
    (`--all` imports everything); credential-bearing headers (Authorization,
    Cookie, `x-api-key`, `x-goog-api-key`, bearer/token/secret/auth headers) are
    dropped; non-importable interactions are skipped with a reason rather than
    failing the whole file.
  - `mockagents import openai-stored-completions <export.jsonl>` — import an
    OpenAI stored-completions JSONL export. Accepts an envelope
    (`{"request":..,"response":..}`) or a flat stored completion (reconstructs
    the request from `model` + `messages`→`/v1/chat/completions` or
    `input`→`/v1/responses`, plus sampling params). Unrecognized lines are
    skipped with a reason.
  - `Cassette.AppendAll` writes an imported batch to disk in a single pass.

  Note: secrets embedded in request/response **bodies** are not redacted on
  import — review before committing, or re-record through
  `mockagents record --redact`.
- **Replay record modes** (R-01) — `mockagents replay --record-mode=<mode>
  --upstream <url>` turns the replay server into a record/replay hybrid by
  wiring the upstream into the existing `Replay.Fallback` seam:
  - `none` (default) — replay only; a miss returns the 404 diagnostics
    (byte-for-byte unchanged from before).
  - `new_episodes` — replay recorded interactions; on a miss, forward to
    `--upstream`, serve the client, and record the new interaction so it replays
    next time.
  - `once` — like `new_episodes` when the cassette holds nothing yet (records),
    like `none` when it is already populated (replay only). Resolved against the
    recorded count, so a leftover empty cassette still records.
  - `all` — never replay; forward + record every request (faithful re-record /
    passthrough, errors included).

  Record-on-miss reuses the record command's `--api-key` / `--redact` /
  `--redact-pattern` wiring and never caches a transient failure as canonical: a
  4xx/5xx upstream response (and a 200 SSE stream that breaks mid-flight) is
  served to the client but not written to the cassette. With `--match-ignore`
  active, the match index now extends incrementally as the cassette grows so
  newly-recorded interactions become matchable without a restart.
  (`internal/recording/mode.go`, `Proxy.SkipRecordOnError`; CLI flags on
  `replay`.) Known follow-ons: the cassette is rewritten in full on every record
  (fine for `none`/short sessions, O(n²) for a long-lived `all` session) and
  `all` does not de-duplicate repeated requests.
- **Configurable replay matchers + miss diagnostics** (R-02) — two independent
  improvements to `mockagents replay`:
  - **`--match-ignore <field>`** (repeatable) makes matching ignore the named
    top-level request-body fields, so a request that differs from the recorded
    interaction only in `temperature`, `seed`, `stream`, `metadata` (or any field
    you name) still hits the cassette. Ignoring is **replay-time only** — the
    cassette on disk and each interaction's stored hash are unchanged. Exact-hash
    matching stays the default; the flag derives a separate "match key" (ignored
    fields stripped, then hashed) via a lazily-built secondary index, and
    sequenced playback (R-04) is preserved. (`internal/recording/matcher.go`;
    `Replay.Matcher`.)
  - **Structured miss diagnostics** — a 404 replay miss now returns a JSON body
    (`Content-Type: application/json`) with the request hash and a `nearest`
    block: the closest recorded interaction **on the same method+path**, scored by
    top-level field overlap, plus a field-level `diff` listing `changed` /
    `missing_in_request` / `extra_in_request` entries (grouped, alphabetical,
    bounded to 25 with values truncated to 200 bytes). A drifted prompt now names
    the field that changed instead of returning an opaque hash. The diff's notion
    of equality matches the matcher (float64 numbers, so `1` and `1.0` are equal),
    and the `Fallback` path is unchanged — diagnostics only fire when no
    `Fallback` is set. (`internal/recording/diagnostics.go`.)
- **Cassette redaction** (R-03) — `mockagents record` gains `--redact` and
  `--redact-pattern <regexp>` (repeatable; implies `--redact`) so secrets are
  masked **before** the interaction is written to the cassette, making recorded
  traffic safer to commit. Default masking covers common provider formats
  (`sk-*`, `key-*`, `Bearer` tokens, AWS `AKIA…`, GitHub `ghp_/github_pat_…`,
  Slack `xox[baprs]-…`, Google/Gemini `AIza…`, and JWTs); `--redact-pattern`
  adds caller-supplied regexps. Redaction is **structure-preserving** — it walks
  the JSON and rewrites string *values* only, so a pattern can never break the
  cassette's framing, rename a key, or corrupt an SSE frame, and large integers
  (token ids, timestamps) survive the round-trip exactly. The request **hash is
  computed from the original body before redaction**, so replay still matches an
  un-redacted request; the live response forwarded to the client is never
  touched. Coverage is best-effort — review a cassette before committing.
  (`internal/recording/redact.go`; `storage.SanitizeBody` now masks every
  occurrence and is idempotent.)
- **Sequenced cassette playback** (R-04) — when a cassette holds multiple
  interactions recorded for the same request hash, replay now serves them **in
  order** (the Nth identical request gets the Nth recorded response), repeating
  the last response once the sequence is exhausted. This makes a multi-turn
  agentic loop — which sends the same request shape each turn — replay the
  correct per-turn response. Single-interaction cassettes are unchanged; the
  on-disk format is unchanged. (`Cassette.LookupSequence` + a per-hash cursor on
  the `Replay` handler.)
- **Vision input parsing** (A-05) — the OpenAI Chat Completions and Anthropic
  Messages adapters now recognize image content parts (OpenAI `image_url`,
  including `data:` URLs; Anthropic `{type:image, source: base64|url}`). The
  image count is carried **out-of-band**, so an image-only turn is no longer
  rejected as an empty message and the flattened user text stays pure (regex
  matching, templates, and token counts are unaffected). A new `has_image`
  scenario match rule fires on image presence, and the request's image count is
  returned in the `X-Mockagents-Image-Count` response header for assertions.
  Example: `examples/vision-agent.yaml`. (Responses-API `input_image` and Gemini
  `inline_data` are noted follow-ons.)
- **Anthropic depth** (A-04) — three Messages-API additions for offline testing
  of cost-cache and thinking-trace handling: **`POST /v1/messages/count_tokens`**
  (returns `{"input_tokens":N}`, engine-free); **prompt-caching usage fields**
  `cache_creation_input_tokens` / `cache_read_input_tokens`, driven by
  `cache_control` markers — a first request bills creation and an identical
  repeat bills read (the fields are omitted when no marker is present, matching
  the SDK's Optional shape); and **extended-thinking blocks** — when thinking is
  enabled (the `thinking` param or an `anthropic-beta: …thinking…` header) the
  response leads with a deterministic `{"type":"thinking",…}` block and the
  thinking tokens count toward output. Non-streaming; the streaming variants are
  a noted follow-on.
- **Azure OpenAI URL routing** (A-06) — an `AzureOpenAI()` SDK client now runs
  unchanged against the mock. Adds the classic deployment surface
  (`POST /openai/deployments/{deployment}/chat/completions` and `/embeddings`,
  where the `{deployment}` path segment becomes the model when the body omits
  it) and the new unified surface (`/openai/v1/chat/completions`,
  `/openai/v1/embeddings`), both delegating to the existing OpenAI handlers. The
  `api-version` query parameter is accepted and ignored. Azure paths are wired
  into the billable/loggable classifier (logged + quota-counted like `/v1/*`),
  exempted in the auth-skip allowlist like the OpenAI routes they delegate to,
  and the tenancy layer now also reads the Azure `api-key` header — so an
  `AzureOpenAI()` client works in both single- and multi-tenant mode.
- **OpenAI Moderations API** (A-07) — a new `POST /v1/moderations` surface
  (omni-moderation shape) for testing guardrail pipelines offline. Returns
  `flagged` + the full set of 13 category booleans, `category_scores`, and
  `category_applied_input_types`, **deterministically**: a keyword→category map
  flags known-harmful phrases (with word-boundary matching, so "skill" doesn't
  trip "kill") at a high score while benign text stays low, over an FNV-seeded
  per-category baseline so scores are realistic and stable across runs. Accepts
  `input` as a string, string array, or content parts; one result per input.
  Zero-config (no agent definition); engine-free like `/v1/embeddings`. Free on
  the real API, so it is deliberately excluded from quota/cost accounting.
- **OpenAI structured outputs / `response_format`** (A-03) — Chat Completions
  now honors `response_format`. With `{type:"json_schema", json_schema:{schema,
  strict}}` the mock returns assistant `content` that is a JSON string
  **conforming to the supplied schema** (so an SDK `.parse()` — Pydantic / Zod —
  round-trips), synthesized deterministically from the schema when the scenario
  doesn't already supply valid JSON. Handles nested objects, arrays, `$ref`/
  `$defs` (incl. recursive), `anyOf`/`oneOf`/`allOf`, `enum`/`const`, nullable
  type arrays, and string `format`s, with depth + array + total-node budgets so
  a hostile/recursive schema can't blow up. `{type:"json_object"}` guarantees a
  JSON object. A planted refusal surfaces as `message.refusal` +
  `finish_reason:"content_filter"`. Example: `examples/structured-output-agent.yaml`.
- **OpenAI Embeddings API** (A-02) — a new `POST /v1/embeddings` surface
  returning **deterministic, L2-normalized vectors** seeded from a hash of
  (input, model, dimensions), so the same request always yields identical
  vectors offline. Matches the real wire shape (`object:"list"`,
  `data[].object:"embedding"`, input-only `usage`), supports `input` as a
  string / string array / token-id array(s), configurable `dimensions`
  (reduce-only cap to the model's native width), and `encoding_format`
  `float` (default) or `base64`. Zero-config — any embedding model name works
  without an agent definition. Also adds the three `text-embedding-*` models to
  the cost table and wires `/v1/embeddings` (and, fixing an A-01 gap,
  `/v1/responses`) into the billable-path classifier so both are logged and
  quota-counted.
- **OpenAI Responses API** (A-01) — a new `POST /v1/responses` surface, the
  default transport of the OpenAI Agents SDK. Supports the polymorphic `input`
  (bare string or typed item array incl. `function_call_output`), `instructions`,
  typed output items (`message` with `output_text`/`refusal` parts and
  `function_call` items), Responses-shaped `usage`, and request-setting echo
  (tools, tool_choice, text, reasoning, temperature, …). **Streaming** emits the
  full named-event ladder (`response.created` → `in_progress` →
  `output_item.added` → `content_part.added` → `output_text.delta`/`.done` →
  `content_part.done` → `output_item.done` → `completed`, plus
  `function_call_arguments.delta`/`.done`) with monotonic `sequence_number`.
  **Stateful** `previous_response_id` replays prior turns from a bounded
  in-memory store, so Agents-SDK tool loops work. Built-in tools
  (`web_search`/`file_search`/…) are accepted as stubs. Chaos errors render in
  the OpenAI error envelope; bodies are size-capped like the other adapters.
- **Scenario-pack templates** (FB-01) — `mockagents init --template <name>` /
  `--list-templates` scaffold a runnable project (agent + a matching TestSuite +
  README) from five curated, embedded packs: `basic`, `customer-support`, `rag`,
  `coding-agent`, `planner`. A docs gallery catalogs every example pack.
- **Hallucination fixtures** (FB-02) — a `hallucination` block on a scenario
  response (typed fault + ground truth) advertised via the
  `X-Mockagents-Hallucination` header, for testing a client's grounding guardrails.
- **Runtime agent write API** (FB-04) — `POST /api/v1/agents` (create),
  `PUT /api/v1/agents/{name}` (replace), `DELETE /api/v1/agents/{name}`: create,
  edit, and remove agents at runtime with no restart (YAML or JSON, validated,
  editor-gated, audited as `agent.created`/`agent.updated`/`agent.deleted`). Plus
  `mockagents add`/`rm` CLI and **Save/Delete in the GUI console** (FB-06) —
  completing the YAML + CLI + API + GUI quadfecta.
- **Failure / error catalog** (FB-03) — `chaos.errors.fail_first` (fail the first
  N requests then recover, for retry/backoff testing); **provider-accurate**
  injected error bodies + `Retry-After` for OpenAI/Anthropic/Gemini; named chaos
  **presets** (`server-down`, `rate-limited`, `access-denied`, `unauthorized`,
  `flaky`, `slow`); and **semantic** response faults (`finish_reason` override,
  `refusal`, malformed tool-call `raw_arguments`) — honored on streaming too.
- **Load-test target** (FB-05) — distribution-based stream timing
  (`ttft_p50_ms`/`ttft_p95_ms`/`itl_p50_ms`/`itl_p95_ms`, lognormal-sampled per
  request) plus k6 + Locust recipes and a "load-test your LLM app for free" guide.
- **GUI console redesign** — the Next.js web console was restyled end-to-end to
  the "MockAgents Console" design system: a `--sr-*` design-token foundation
  with a light/dark theme toggle (SSR-safe, no flash), a new grouped sidebar
  shell with breadcrumbs, and every surface (agent catalog, agent detail, logs,
  costs, audit, pipelines, editor, tenants/keys, account) rebuilt to the design.
  Icons render as JSX (no `dangerouslySetInnerHTML`). (§2.55)
- **Homelab deployment scripts** — a `homelabsetup/` suite that provisions a K3s
  cluster (`bootstrap-homelab.sh`: K3s + MetalLB + an in-cluster registry +
  containerd mirror) and deploys MockAgents via the bundled Helm chart
  (`deploy-homelab.sh`: build/push an immutable `build-<ts>` image, render
  `examples/` into a ConfigMap, `helm upgrade --install` with a Traefik ingress
  on `mockagents.local`). Includes `fresh-deploy`, `stop`/`restart` (pause/resume
  via replica annotations), and `cleanup` lifecycle scripts plus a
  `DEPLOY_MOCKAGENTS.md` guide. Supports `--multi-tenant` (captures the bootstrap
  admin key) and `--persist` (PVC-backed SQLite log).

### Changed
- Documentation refresh: `CHANGELOG.md` rebuilt to cover v0.1–v0.3,
  `docs/architecture-diagrams.md` and `docs/sequence-diagrams.md` updated from
  the CLI-only-MVP baseline to the current control-plane architecture, and
  `README.md` RBAC table corrected for the `platform` role.

### Security
- **Bounded request-body decoding** — the OpenAI/Anthropic/Gemini adapter routes
  now cap each decoded request body at 10 MiB (`http.MaxBytesReader`) instead of
  draining it into an unbounded pooled-buffer allocation. An oversized body is
  rejected with `413 Request Entity Too Large` in the provider's own error
  envelope. Closes an unbounded-allocation DoS on every adapter route.

### Fixed
- **License detection** — `LICENSE` now carries the full verbatim Apache-2.0
  text (the previous truncated header with an embedded copyright line made
  GitHub report `NOASSERTION`); the project copyright notice moved to a new
  `NOTICE` file.

---

## [v0.3.0] — Control plane, MCP duplex, SDK parity

### Added
- **Multi-tenant control plane GUI** — cookie-based admin auth (`/login`),
  tenant CRUD, and per-tenant API-key management (mint, role change, rotate)
  in the web console. (§2.32)
- **MCP v0.3 bidirectional transport** — server-initiated `sampling/createMessage`
  and `roots/list` flow over an SSE duplex channel: clients subscribe to
  `GET /mcp/events` and POST replies to `POST /mcp/response`. In-process
  primitives `Server.SendRequest` / `Sample` / `ListRoots`; `POST /mcp/sample`
  and `/mcp/roots` admin triggers for black-box tests. (§2.33)
- **Real-time log feed over SSE** — `GET /api/v1/logs/stream` plus a same-origin
  GUI proxy; new interaction rows appear sub-second after the SQLite write.
  (§2.34)
- **Schema-aware config editor** — `POST /api/v1/config/validate` and a GUI
  `/editor` running the *same* validator as `mockagents validate` (no
  client-side schema duplication). (§2.35)
- **Pipeline DAG viewer + management API** — `GET /api/v1/pipelines[/{name}]`
  and a read-only SVG DAG view in the console. (§2.36)
- **API-key rotation** — `POST /api/v1/keys/{id}/rotate` regenerates a secret in
  place (stable id/name/role/tenant), flushes the auth cache, and emits an
  `api_key.rotated` audit event with old + new prefixes. (§2.37)
- **Self-service key rotation + burn** — `POST /api/v1/keys/me/rotate` lets any
  authenticated operator rotate their own key; `POST /api/v1/keys/me/burn`
  rotates without returning the new plaintext (emergency response to a
  compromised browser session). GUI `/account` surface. (§2.47, §2.50)
- **Bulk + selective tenant-key rotation** — `POST /api/v1/tenants/{id}/keys/rotate`
  rotates every key in a tenant transactionally; `?except=self` spares the
  caller's own key so an admin can't lock themselves out. (§2.49, §2.51)
- **MCP bidirectional helpers in all three SDKs** — `McpClient` / `McpEvent` /
  `McpEventStream` with identical surfaces in Python, TypeScript, and Go.
  (§2.39, §2.40, §2.41)
- **Go SDK streaming + in-process engine** — `ChatStream` / `MessageStream` /
  `IterStream` and `NewInProcessClient`, which runs an engine + `httptest.Server`
  inline so Go tests skip the subprocess. (§2.31)
- **Multi-kind validation** — `Pipeline`, `TestSuite`, and `MCPServer` documents
  validate under `mockagents validate`, including pipeline graph checks (cycle +
  unreachable-node detection) and a second cross-document pass that resolves
  every agent/target/node reference across a directory. (§2.38, §2.42, §2.43,
  §2.45, §2.46)
- **Aggregate SSE stream metrics** — admin-gated `GET /api/v1/logs/stream/metrics`
  snapshot of every subscriber's drop count and buffer utilization; the GUI
  surfaces backpressure as a sticky badge. (§2.44, §2.48)

### Changed
- **`platform` super-admin role** — RBAC is now ordered
  `viewer < editor < admin < platform`. Managing the tenant *collection*
  (`GET/POST /api/v1/tenants`, `DELETE /tenants/{id}`) requires `platform`,
  which is minted only by the CLI bootstrap; the management API refuses to
  assign it, so a per-tenant admin cannot self-escalate. (§2.53)
- **Localhost bind by default** — `mockagents start` binds `127.0.0.1`;
  container/remote deployments opt in with `--host 0.0.0.0` /
  `MOCKAGENTS_HOST`. (§2.52)
- **Tenant scope derives from the API-key principal**, not the spoofable
  `X-Mockagents-Tenant` header; `/v1/models` and logs/costs/streams are
  tenant-scoped. (§2.52)

### Security
- **Two-package multi-pass security review hardening** — fixed a cross-tenant
  API-key IDOR (a tenant admin could rotate/delete/promote another tenant's
  key), unified every management route behind a single role-floor table +
  `mountManaged` chokepoint that panics on an un-floored route, repaired
  silently-unmounted live-feed routes + SSE flush, made auth fail closed, and
  added body-size caps, uniform error envelopes, and YAML-alias-bomb bounds.
  Each fix is neuter-verified. (§2.53)
- **GUI security hardening** — `Secure`/`SameSite=Strict` HttpOnly cookie,
  one-time key plaintext via a server-side flash store (never URLs), same-origin
  guards on proxy routes, and a CSP + security-header set. (`docs/SECURITY-REVIEW-GUI.md`)
- **Privacy & retention controls** — `MOCKAGENTS_LOG_BODIES`
  (`full`|`sanitized`|`none`) gates response-body capture and
  `MOCKAGENTS_LOG_MAX_ROWS` bounds the interaction table.

### Performance
- **Hot-path optimizations** — O(1) tenant→model index replacing a per-request
  O(n) scan, skipping the no-op tracing wrapper, coarsened auth `last_used`
  writes, a pooled response encoder, memoized match lowering, and single-copy
  body capture. Each is benchmark-guarded and neuter-verified;
  `docs/PERFORMANCE.md` is the handoff doc, `docs/benchmarks/latest.{json,md}`
  the checked-in baseline. (§2.52, §2.54)
- **`govulncheck` remediation** — `toolchain go1.26.4` + `golang.org/x/net`
  upgrade clear all reachable stdlib/net vulnerabilities.

---

## [v0.2.0] — Performance, streaming parity, observability surfaces

### Added
- **TypeScript and Python SDK streaming parity** — `chatStream`/`messageStream`/
  `iterStream` (TS) and `message_stream`/`iter_stream` (Py) plus `StreamChunk`.
- **GUI v0.2 surfaces** — cost dashboard, audit log viewer, per-row log detail,
  and the first live feed.
- **MCP v0.2** — `completion/complete`, `logging/setLevel`, and a server-emitted
  notification queue with an admin notify endpoint.
- **Tenant-scoped agent isolation** — `metadata.tenant_id`, engine tenant
  context (`engine.WithTenantID` / `TenantIDFromContext`), registry `*ForTenant`
  visibility methods, and the opt-in `X-Mockagents-Tenant` header.
- **Cost estimation** — per-model price table (`internal/pricing`), `cost_usd`
  log annotation, and `GET /api/v1/costs`; `MOCKAGENTS_PRICING` loads overrides.
- **Audit logging** — append-only SQLite log of control-plane mutations with
  `GET /api/v1/audit`, including `auth.denied` and role-change events.
- **Streaming cassette capture** — record/replay now tees and replays SSE
  streams.
- **Helm chart v0.2** — opt-in HPA, PodDisruptionBudget, NetworkPolicy, and
  ServiceMonitor.

### Performance
- **v0.2 perf workstream** — pooled JSON decode buffers (-39 % B/op vs
  `json.NewDecoder`), a bounded async log-worker pool (replacing unbounded
  goroutine fan-out), a TTL auth cache that skips bcrypt on repeat resolutions
  (~36 ms cold → sub-µs hot), SQLite multi-conn pool (`MaxOpenConns=8` +
  `synchronous=NORMAL`), session history pre-sizing, tracer NoOp bypass, and
  pooled template/response buffers. Hot path moved -10 % to -24 %.

---

## [v0.1.0] — Foundation MVP

### Added
- **Agent definitions** — declarative `mockagents/v1` YAML with JSON-schema
  validation (`schema/mockagents-v1-agent.json`).
- **Mock engine** — scenario matching (`content_contains`, `content_regex`,
  `turn_number`, `default`), 15+ template functions (`{{ uuid }}`,
  `{{ random_int }}`, `{{ fake_name }}`, …), and conversation state management.
- **Tool-call simulation** — match-based tool responses, error injection,
  parameter validation, and parallel processing.
- **Protocol adapters** — wire-compatible OpenAI Chat Completions
  (`/v1/chat/completions`) and Anthropic Messages (`/v1/messages`), each with
  non-streaming and SSE-streaming modes.
- **HTTP server** — `net/http` multi-agent routing, middleware (auth, logging,
  CORS), graceful shutdown, fsnotify hot reload (`--watch`), and a management
  API.
- **Multi-agent pipelines** (`kind: Pipeline`) — sequential, parallel, and graph
  topologies with substring-matched conditional edges.
- **TestSuite runner** (`kind: TestSuite`) — declarative cases with
  `tool_call` / `response_contains` / `scenario_matched` / `latency_ms_lt`
  assertions; `mockagents test` emits text/JSON/JUnit.
- **Chaos engineering** — per-agent `latency`, `errors`, and `rate_limit`
  injection, evaluated before tool resolution.
- **Record & playback** — proxy a real upstream once (`mockagents record`),
  replay offline forever (`mockagents replay`); cassettes are JSON-lines.
- **Mock MCP server** (`kind: MCPServer`) — JSON-RPC 2.0 over HTTP + stdio
  (`mockagents mcp`).
- **Contract testing** — `mockagents contract extract | diff` classifies changes
  as breaking / additive / info for CI.
- **OpenTelemetry tracing** — opt-in OTLP/HTTP exporter, no-op (zero overhead)
  by default.
- **SDKs** — Python (`MockAgentServer`, `MockAgentClient`, `expect()` assertions,
  LangChain/LangGraph/CrewAI adapters), TypeScript, and Go.
- **Multi-tenant auth + RBAC** (opt-in `MOCKAGENTS_MULTI_TENANT=1`) — bcrypt API
  keys, `viewer`/`editor`/`admin` roles, and a bootstrap admin key.
- **Web console v0.1** (Next.js 15) — agent catalog and interaction-log views.
- **Interaction logging** — pure-Go SQLite (`modernc.org/sqlite`, no cgo) with a
  query API and `mockagents logs`.
- **Packaging** — single static binary, multi-stage Docker image,
  docker-compose, Helm chart v0.1, and GitHub Actions / GitLab CI templates.

### Protocol support
- OpenAI Chat Completions API (non-streaming + SSE streaming)
- Anthropic Messages API (non-streaming + SSE streaming)
- Model Context Protocol (JSON-RPC 2.0, HTTP + stdio)

### CLI commands
- `init`, `start`, `validate`, `logs`, `test`, `record`, `replay`, `mcp`,
  `contract`
