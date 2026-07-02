# Design: Server VAD / turn detection for the mock Realtime API (F5)

**Status:** Phase 1 implemented (`internal/realtime/vad.go`) · Phases 2–3 proposed
**Origin:** round-3 fidelity eval, finding F5 (S1, architectural)
**Scope:** `internal/realtime` + `internal/adapter/realtime.go`

## 1. Problem

GA Realtime sessions default to server-side voice activity detection
(`turn_detection: {type: "server_vad", create_response: true}`). A stock voice
client therefore:

1. streams `input_audio_buffer.append` continuously (mic audio, **including
   silence frames**) — it never sends `input_audio_buffer.commit` or
   `response.create`;
2. waits for the **server** to emit `input_audio_buffer.speech_started` /
   `speech_stopped`, auto-commit the turn, and auto-run a response.

The mock accepts a `turn_detection` config in `session.update` and echoes it
back in the session object **as if active**, but never emits a VAD event, never
auto-commits, and never auto-responds. Result: a default-config GA voice client
(openai-agents realtime, the reference console, LiveKit/Pipecat pipelines)
connects, streams audio, and **hangs forever in "listening"**. Echo-without-
action is worse than rejection — the client has no signal anything is wrong.

## 2. Key insight: most of server VAD does not need an async server

The backlog assumed F5 "needs the async write model". That is only one-third
true. Real clients stream **continuous** audio — silence arrives as low-energy
PCM frames, not as an absence of frames. So the two core VAD transitions are
decidable **synchronously, inside `Handle("input_audio_buffer.append")`**:

- **speech start** — the first append whose decoded energy crosses the
  threshold;
- **speech stop** — appends whose accumulated low-energy audio spans
  `silence_duration_ms`.

PCM16 mono @ 24 kHz is 48 bytes/ms and mean-absolute-sample energy is a real
(if crude) VAD — the same family of detector `server_vad` itself describes
(`threshold`, "louder audio to activate"). It is deterministic, pure Go
(no cgo), and computable per append. **Phase 1 therefore ships the canonical
voice loop with zero changes to the synchronous `Session.Handle` model.**

Wall-clock timers are genuinely required only for:

- `idle_timeout_ms` → `input_audio_buffer.timeout_triggered` (fires when **no**
  frames arrive);
- interrupting an **in-flight** response (barge-in) — which first requires
  responses to be *paced* over the WS instead of emitted as one atomic burst.

Those are Phase 2.

## 3. Phase 1 — synchronous energy VAD (effort: S–M, ~1 day)

**Behavior** (only when the client has *enabled* turn detection via
`session.update`; see §6 for the default-on question):

```
client                                mock
──────────────────────────────────── ─────────────────────────────────────────
input_audio_buffer.append (speech) → input_audio_buffer.speech_started
                                       {audio_start_ms, item_id}   ← pre-minted
input_audio_buffer.append (speech) → (accumulate)
input_audio_buffer.append (silence)→ (accumulate silence ms)
input_audio_buffer.append (silence)→ [silence ≥ silence_duration_ms]
                                     input_audio_buffer.speech_stopped
                                       {audio_end_ms, item_id}
                                     input_audio_buffer.committed
                                     conversation.item.added / .done
                                     [create_response ≠ false]
                                     response.created … response.done   (existing ladder)
```

**Mechanics**

- `Session` gains a `vad` sub-state: decoded-ms counter (`bytes/48`), speech
  active flag, pre-minted next `item_id` (the GA `speech_started` event carries
  the id of the item that *will* be created), accumulated silence ms.
- Energy detector: base64-decode the append payload, mean(|int16|)/32768 vs
  `threshold` (default 0.5). Frames that fail to decode as PCM16 count as
  speech (a mock shouldn't punish synthetic test audio).
- `audio_start_ms` = total buffered ms at speech onset − `prefix_padding_ms`
  (default 300), clamped ≥ 0. `audio_end_ms` = ms at the stop decision.
- Turn end runs the **existing** commit + item + response ladders (reuse; the
  auto-path must stay byte-identical to the manual path).
- `semantic_vad`: same detector; `eagerness` maps to the silence window
  (high→2000 ms, auto/medium→4000 ms, low→8000 ms — the documented max
  timeouts). No semantic modeling in a mock.
- Manual `input_audio_buffer.commit`/`response.create` while VAD is active:
  **follow the real API** (verify exact behavior during implementation; believed
  to be an `invalid_request_error`). At minimum do not double-commit.
- `interrupt_response`, `idle_timeout_ms`: accepted, inert, documented (Phase 2).

**Files:** `internal/realtime/session.go` (+`vad.go` if >150 LOC),
`session_test.go` (synthetic PCM helpers: zeros = silence, square wave =
speech), `internal/adapter/realtime_test.go` (one end-to-end WS VAD turn),
CHANGELOG, `site/docs` Realtime page.

**Acceptance:** a client that only appends speech-then-silence receives the full
`speech_started → … → response.done` sequence with correct
`audio_start_ms`/`audio_end_ms`/`item_id` chaining; a session without
turn_detection behaves exactly as today (all existing tests untouched).

## 4. Phase 2 — deadline-driven timers + interruptible responses (effort: M–L, 2–4 days)

Two capabilities, one mechanism:

**Mechanism — deadlines, not goroutines.** Keep `Session` single-goroutine and
deterministic. Add:

```go
func (s *Session) NextDeadline() (time.Time, bool)   // earliest pending timer
func (s *Session) Tick(now time.Time) []Event        // fire due timers
```

The adapter read-loop becomes a `select` between `c.Read` and a
`time.Timer` armed from `NextDeadline()`. Tests drive `Tick` with a fake clock —
no sleeps, no races, `Session` itself never spawns a goroutine.

**Capability A — `idle_timeout_ms`:** arm a deadline after `response.done`
(spec: response.done time + audio playback duration — the mock knows the
synthesized audio's nominal duration); on fire emit
`input_audio_buffer.timeout_triggered {audio_start_ms, audio_end_ms, item_id}`,
commit the (empty) segment, and auto-run a response.

**Capability B — paced responses + barge-in:** emit the response ladder
incrementally against deadlines (reuse the TTFT/ITL physics of
`internal/streaming/pacing.go` — same fault model as the SSE adapters, fixing
today's "entire ladder in one burst" deviation as a side effect). With an
in-flight response:

- VAD speech start (or client `response.cancel`) → stop emission, emit
  `response.done` with `status:"cancelled"`,
  `status_details.reason:"turn_detected"` (or `"client_cancelled"`);
- honor `interrupt_response:false` by letting the response finish;
- `response_cancel_not_active` then only fires when truly nothing is in flight.

Phase 2 unlocks, for free: real mid-stream chaos on the Realtime surface
(truncation/malformed faults currently SSE-only) and meaningful
`conversation.item.truncate` timing.

## 5. Phase 3 — polish (effort: S)

- `input_audio_buffer.timeout_triggered` edge semantics; VAD + transcription
  interplay (`speech_stopped` before transcription events, matching GA order).
- Config validation (reject `threshold` ∉ [0,1], negative durations) with GA
  error codes.

## 6. Compatibility decision (needs a call)

GA **defaults VAD on** for new sessions; the mock currently reports
`turn_detection: null`. Flipping the default would make every existing
manual-commit mock user's flow error (real API rejects manual commit under
VAD). Recommendation:

- **Phase 1:** VAD only when the client explicitly configures it. Default stays
  `null` — an honest, documented deviation (the session object already tells
  clients VAD is off, which is self-consistent).
- **Later:** opt-in GA-default parity via env (`MOCKAGENTS_REALTIME_GA_DEFAULTS=1`)
  once VAD has soaked.

## 7. Risks / open questions

| # | Risk / question | Mitigation |
|---|---|---|
| 1 | Real API's exact error for manual commit under VAD (code/message) is unverified | Verify against a live endpoint or SDK conformance during Phase 1; worst case use `invalid_request_error` + descriptive code |
| 2 | Energy VAD misclassifies compressed/no-PCM test audio | Non-PCM-decodable frames count as speech; document that silence must be near-zero PCM16 |
| 3 | Pacing over WS changes event timing existing tests may assume | Pacing only when `StreamingConfig`/VAD requires it; burst mode remains the default |
| 4 | `speech_started.item_id` pre-minting must match the eventually created item | Single mint point in the VAD state, consumed by the commit path (test-pinned) |
| 5 | Semantic VAD is necessarily fake | Documented: eagerness→window mapping only |

## 8. Out of scope

WebRTC/SIP transports (`output_audio_buffer.*`), transcription-only sessions
(`type:"transcription"`), MCP-in-Realtime, real STT/TTS.
