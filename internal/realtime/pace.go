// Deadline-driven pacing, cancellation, and idle timeout (Phase 2 of
// docs/design/realtime-server-vad.md).
//
// The Session stays single-goroutine and deterministic: it never spawns a
// goroutine or arms a timer itself. Instead it exposes NextDeadline() — the
// earliest pending deadline — and Tick(ctx, now) — fire everything due. The
// transport (adapter/realtime.go) selects between the client's next frame and
// a timer armed from NextDeadline; tests drive Tick with a fake clock.
//
// Pacing is what creates the interruption window: a paced response's ladder is
// emitted one event per interval instead of as one atomic burst, so a VAD
// speech start (interrupt_response) or a client response.cancel can land
// mid-response and cancel it — response.done then reports status "cancelled"
// with status_details.reason "turn_detected" / "client_cancelled". Burst mode
// remains the default: pacing applies only when the transport enabled it AND
// the client enabled turn detection (voice sessions are where barge-in lives).
package realtime

import (
	"context"
	"strings"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
)

// idleTimeoutPlaceholder is the user turn synthesized when server VAD's
// idle_timeout_ms fires: the model "prompts the user to continue", so scenario
// authors can match on this marker for "are you still there?" flows.
const idleTimeoutPlaceholder = "[silence]"

// inflightResponse is a paced response mid-emission: the remaining ladder
// events, the next emission deadline, and what's needed to finish or cancel it.
type inflightResponse struct {
	respID    string
	rc        *responseCtx
	queue     []Event
	nextAt    time.Time
	doneItems []any  // items whose output_item.done already emitted (a cancelled response's output)
	onDone    func() // deferred side effects (history append) — only if the response completes
	usage     any    // the final response.done's usage block (billing survives cancellation)
	// Emission tracking for cancellation close-out: the .done events of a
	// cancelled item must carry exactly the concatenation of the deltas the
	// client received (never the full never-streamed payload), a content part
	// that never opened must not be fabricated at close-out, and usage must
	// bill only the words that actually streamed.
	emitted    map[string]string // item id → concatenated delta text (transcript/text/arguments)
	partOpened map[string]bool   // item id → its content_part.added emitted
	transcript string            // all emitted message-transcript deltas (usage on cancel)
	// Emission-time chain values: queued conversation.item.added events carry
	// the BUILD-time previous_item_id, but the conversation can change while
	// the ladder is queued (user items committed/created mid-pace, the tail
	// deleted). The value is rewritten to the actual tail when the .added
	// emits and remembered here so the paired .done agrees.
	chainPrev map[string]any // item id → previous_item_id assigned at .added emission
	// Ids deleted AFTER their announcement: the delete already removed them
	// from the conversation, so the queued conversation.item.done must not
	// re-index them (retrievable-but-not-in-chain resurrection).
	deleted map[string]bool
}

// SetClock injects the time source (tests use a fake); nil/unset means
// time.Now. Only the paced/idle paths ever consult it, so purely-burst
// sessions remain wall-clock-free.
func (s *Session) SetClock(now func() time.Time) { s.now = now }

// SetPacing enables paced response emission with the given inter-event
// interval. The transport sets this; zero (the default) keeps every response an
// atomic burst. Pacing is a Phase-2 approximation with a constant interval —
// wiring the agent's StreamingConfig TTFT/ITL physics (streaming/pacing.go)
// through the Generator is a noted follow-on.
func (s *Session) SetPacing(interval time.Duration) { s.paceInterval = interval }

func (s *Session) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// NextDeadline returns the earliest pending deadline (paced-response emission
// or idle timeout), if any. The transport arms its timer from this.
func (s *Session) NextDeadline() (time.Time, bool) {
	if s.inflight != nil {
		return s.inflight.nextAt, true
	}
	if !s.idleAt.IsZero() {
		return s.idleAt, true
	}
	return time.Time{}, false
}

// Tick fires everything due at now: queued paced-response events (completing
// the response when the queue drains) and then, with nothing in flight, the
// idle timeout. Like Handle, every returned event is stamped.
func (s *Session) Tick(ctx context.Context, now time.Time) []Event {
	// Timer-initiated flows (paced emission, idle timeout, the queued
	// auto-response) have no causing client event: an error emitted from here
	// must carry event_id null, not the id of whatever unrelated client event
	// happened to be handled last.
	s.lastClientEventID = ""
	var out []Event
	for s.inflight != nil && !s.inflight.nextAt.After(now) {
		inf := s.inflight
		ev := inf.queue[0]
		inf.queue = inf.queue[1:]
		emit := true
		// Emission-time conversation joins: the item becomes retrievable and the
		// chain tail only when its announcement reaches the client (see
		// createResponse — build no longer mutates session state).
		switch ev["type"] {
		case "conversation.item.added":
			s.rewriteChainPrev(inf, ev)
			s.joinEmittedItem(ev)
		case "conversation.item.done":
			if item, ok := ev["item"].(map[string]any); ok {
				id, _ := item["id"].(string)
				if prev, assigned := inf.chainPrev[id]; assigned {
					ev["previous_item_id"] = prev
				}
				if inf.deleted[id] {
					// The client was told this item is deleted — its
					// conversation mirror does not close out (the
					// response-scoped output_item.done still emits).
					emit = false
				} else {
					s.rememberItem(item)
				}
			}
		case "response.output_audio.delta":
			s.respondedWithAudio = true // assistant audio reached the client — voice locked
		case "response.content_part.added":
			if id, _ := ev["item_id"].(string); id != "" {
				inf.partOpened[id] = true
			}
		case "response.output_text.delta", "response.output_audio_transcript.delta":
			if id, _ := ev["item_id"].(string); id != "" {
				delta, _ := ev["delta"].(string)
				inf.emitted[id] += delta
				inf.transcript += delta
			}
		case "response.function_call_arguments.delta":
			if id, _ := ev["item_id"].(string); id != "" {
				delta, _ := ev["delta"].(string)
				inf.emitted[id] += delta
			}
		case "response.output_item.done":
			inf.doneItems = append(inf.doneItems, ev["item"])
		}
		if emit {
			out = append(out, ev)
		}
		if len(inf.queue) == 0 {
			if inf.onDone != nil {
				inf.onDone()
			}
			s.inflight = nil
			// A turn that committed while this response was in flight queued its
			// auto-response (vadEndOfTurn); run it now that the slot is free —
			// otherwise the guard would have silently eaten the user's reply.
			if s.pendingResponse {
				s.pendingResponse = false
				out = append(out, s.createResponse(ctx, &ClientEvent{})...)
			} else {
				s.armIdleTimer()
			}
			break
		}
		inf.nextAt = inf.nextAt.Add(s.paceInterval)
	}
	if s.inflight == nil && !s.idleAt.IsZero() && !s.idleAt.After(now) {
		s.idleAt = time.Time{}
		out = append(out, s.idleTimeout(ctx)...)
	}
	return s.stamp(out)
}

// beginPacedResponse queues a fully-built response ladder for incremental
// emission and returns the part a real server sends immediately
// (response.created + rate_limits.updated). onDone runs only if the response
// completes — a cancelled response must not leave its transcript in history.
func (s *Session) beginPacedResponse(respID string, rc *responseCtx, ladder []Event, onDone func()) []Event {
	immediate, queue := ladder[:2], ladder[2:]
	inf := &inflightResponse{
		respID: respID, rc: rc, queue: queue,
		nextAt:  s.clock().Add(s.paceInterval),
		onDone:  onDone,
		emitted: map[string]string{}, partOpened: map[string]bool{},
		chainPrev: map[string]any{}, deleted: map[string]bool{},
	}
	// Keep the final usage block at hand: a cancelled response still bills the
	// generated tokens, so cancelInflight reports it on its response.done.
	if done := ladder[len(ladder)-1]; done["type"] == "response.done" {
		if resp, ok := done["response"].(map[string]any); ok {
			inf.usage = resp["usage"]
		}
	}
	s.inflight = inf
	return immediate
}

// cancelInflight aborts the paced response. The real API first FINALIZES the
// announced-but-unfinished item — its *.done close-out events fire and the item
// stays in the conversation with status "incomplete" (that is what a client's
// conversation.item.truncate then targets) — and only then emits response.done
// (status "cancelled", reason "turn_detected" for VAD barge-in or
// "client_cancelled" for response.cancel). The close-out honors the
// delta-concatenation invariant: every .done payload (and the stored item, and
// usage) carries exactly the deltas the client received — never the full
// never-streamed content — and a content part that never opened is not
// fabricated. Remaining deltas and never-started items are dropped.
func (s *Session) cancelInflight(reason string) []Event {
	inf := s.inflight
	s.inflight = nil
	// A barge-in supersedes the queued auto-response — the new turn's own end
	// will answer. A CLIENT cancel targets only the in-flight response: the
	// pending auto-response belongs to a different, already-committed user
	// turn and survives (the response.cancel handler runs it right after the
	// close-out). The idle timeout belongs to the cancelled flow either way.
	if reason == "turn_detected" {
		s.pendingResponse = false
	}
	s.idleAt = time.Time{}

	var out []Event
drain:
	for _, ev := range inf.queue {
		t, _ := ev["type"].(string)
		itemID, _ := ev["item_id"].(string)
		switch {
		case strings.HasSuffix(t, ".delta"):
			continue // the cut: content past the cancel point is never emitted
		case t == "response.output_item.added" || t == "response.done":
			break drain // a never-started item, or the end of the ladder
		case t == "conversation.item.added":
			// The announced item's mirror pair still opens during close-out; the
			// join side effects apply exactly as they would in Tick.
			s.rewriteChainPrev(inf, ev)
			s.joinEmittedItem(ev)
			out = append(out, ev)
		case t == "conversation.item.done":
			if item, ok := ev["item"].(map[string]any); ok {
				id, _ := item["id"].(string)
				if prev, assigned := inf.chainPrev[id]; assigned {
					ev["previous_item_id"] = prev
				}
				if inf.deleted[id] {
					continue // no conversation mirror close-out for a deleted item
				}
				s.rememberItem(item) // the final (now incomplete) item — retrieve agrees
			}
			out = append(out, ev)
		case t == "response.content_part.added":
			continue // a part the client never saw open is not fabricated now
		case t == "response.output_text.done":
			if !inf.partOpened[itemID] {
				continue
			}
			ev["text"] = inf.emitted[itemID]
			out = append(out, ev)
		case t == "response.output_audio_transcript.done":
			if !inf.partOpened[itemID] {
				continue
			}
			ev["transcript"] = inf.emitted[itemID]
			out = append(out, ev)
		case t == "response.output_audio.done":
			if !inf.partOpened[itemID] {
				continue
			}
			out = append(out, ev)
		case t == "response.content_part.done":
			if !inf.partOpened[itemID] {
				continue
			}
			rewriteContent(ev["part"], inf.emitted[itemID])
			out = append(out, ev)
		case t == "response.function_call_arguments.done":
			// Same rule as unopened content parts: a stream that never emitted
			// a delta is not fabricated a close-out.
			if _, started := inf.emitted[itemID]; !started {
				continue
			}
			ev["arguments"] = inf.emitted[itemID]
			out = append(out, ev)
		case t == "response.output_item.done":
			if item, ok := ev["item"].(map[string]any); ok {
				id, _ := item["id"].(string)
				item["status"] = "incomplete" // same map the mirror .done carries
				switch {
				case item["type"] == "function_call":
					item["arguments"] = inf.emitted[id]
				case !inf.partOpened[id]:
					item["content"] = []any{} // no part ever opened — nothing streamed
				default:
					if content, ok := item["content"].([]any); ok {
						for _, c := range content {
							rewriteContent(c, inf.emitted[id])
						}
					}
				}
				inf.doneItems = append(inf.doneItems, item)
			}
			out = append(out, ev)
		default:
			out = append(out, ev)
		}
	}

	// A message item that fully streamed BEFORE the cancel is a completed
	// conversation item the client heard in full — its transcript joins the
	// engine history even though the response's onDone never runs (only the
	// still-streaming tail is lost to the cancel).
	for _, it := range inf.doneItems {
		item, ok := it.(map[string]any)
		if !ok || item["type"] != "message" || item["status"] != "completed" {
			continue
		}
		if txt := storedItemText(item); txt != "" {
			s.history = append(s.history, engine.RequestMessage{Role: "assistant", Content: txt})
		}
	}

	items := inf.doneItems
	if items == nil {
		items = []any{}
	}
	cancelled := s.responseObject(inf.respID, "cancelled", items, inf.rc)
	cancelled["status_details"] = map[string]any{"type": "cancelled", "reason": reason}
	// Usage bills only the transcript that actually streamed — a head-cancel
	// produced nothing and reports zero output tokens.
	cancelled["usage"] = inf.usage
	if u, ok := inf.usage.(map[string]any); ok {
		if in, ok := u["input_tokens"].(int); ok {
			cancelled["usage"] = usageBlock(in, wordCount(inf.transcript))
		}
	}
	return append(out, Event{"type": "response.done", "response": cancelled})
}

// truncateInflightItem applies a conversation.item.truncate to the item the
// inflight response is still streaming. The mock has no audio timeline, so
// the cut point is what has already been emitted: remaining deltas for the
// item are dropped and the queued close-outs (and usage, and the history
// append) are rewritten to the emitted prefix — the acked truncation must be
// real in every observable surface, not a no-op.
func (s *Session) truncateInflightItem(itemID string) {
	inf := s.inflight
	if inf == nil {
		return
	}
	prefix := inf.emitted[itemID]

	queue := inf.queue[:0:0]
	for _, ev := range inf.queue {
		t, _ := ev["type"].(string)
		belongs, _ := ev["item_id"].(string)
		if item, ok := ev["item"].(map[string]any); ok && belongs == "" {
			belongs, _ = item["id"].(string)
		}
		if belongs != itemID {
			queue = append(queue, ev)
			continue
		}
		switch {
		case strings.HasSuffix(t, ".delta"):
			continue // the cut: nothing past the truncation point is emitted
		case t == "response.output_text.done":
			ev["text"] = prefix
		case t == "response.output_audio_transcript.done":
			ev["transcript"] = prefix
		case t == "response.content_part.done":
			rewriteContent(ev["part"], prefix)
		case t == "response.output_item.done", t == "conversation.item.done":
			// The final item map is shared by both events — one rewrite covers
			// retrieve, the mirror, and the response.done output listing.
			if item, ok := ev["item"].(map[string]any); ok {
				if content, ok2 := item["content"].([]any); ok2 {
					for _, c := range content {
						rewriteContent(c, prefix)
					}
				}
			}
		}
		queue = append(queue, ev)
	}
	inf.queue = queue

	// Usage bills the truncated transcript, and completion appends it to the
	// engine history in place of the full one ("no text in the context the
	// model hasn't heard").
	for _, ev := range inf.queue {
		if ev["type"] != "response.done" {
			continue
		}
		if resp, ok := ev["response"].(map[string]any); ok {
			if u, ok2 := resp["usage"].(map[string]any); ok2 {
				if in, ok3 := u["input_tokens"].(int); ok3 {
					resp["usage"] = usageBlock(in, wordCount(prefix))
				}
			}
		}
	}
	if !inf.rc.outOfBand {
		inf.onDone = func() {
			if prefix != "" {
				s.history = append(s.history, engine.RequestMessage{Role: "assistant", Content: prefix})
			}
		}
	}
}

// rewriteChainPrev replaces a queued conversation.item.added event's
// build-time previous_item_id with the conversation tail AT EMISSION, and
// records the value so the item's paired conversation.item.done matches. The
// tail can have moved since build: a user turn committed mid-pace, a client
// item inserted, or the previous tail deleted — the baked value would fork
// the chain or dangle at an id the server itself rejects.
func (s *Session) rewriteChainPrev(inf *inflightResponse, ev Event) {
	item, ok := ev["item"].(map[string]any)
	if !ok {
		return
	}
	prev := s.previousItemID()
	ev["previous_item_id"] = prev
	if id, _ := item["id"].(string); id != "" {
		inf.chainPrev[id] = prev
	}
}

// rewriteContent replaces a content part's text/transcript payload with the
// emitted prefix (whichever field the part carries), for cancellation
// close-out.
func rewriteContent(part any, prefix string) {
	p, ok := part.(map[string]any)
	if !ok {
		return
	}
	if _, has := p["text"]; has {
		p["text"] = prefix
	}
	if _, has := p["transcript"]; has {
		p["transcript"] = prefix
	}
}

// armIdleTimer schedules the server-VAD idle timeout after a response
// completes. Simplification vs GA (documented in the design doc): the deadline
// is response.done + idle_timeout_ms — the mock's synthesized audio has no real
// playback duration to add.
func (s *Session) armIdleTimer() {
	// "Idle timeout is currently only supported for server_vad mode" (GA);
	// the SemanticVad config has no such field. A transcription session never
	// prompts the user — it never responds at all.
	if s.vad == nil || s.vad.cfg.IdleTimeoutMs <= 0 || s.vad.cfg.Type != "server_vad" || s.isTranscription() {
		return
	}
	// Once per stretch of inactivity (Phase 3): the timeout does NOT re-arm
	// after its own triggered response, or a silent connection would self-prompt
	// forever. Any user activity clears idleFired and re-allows it.
	if s.idleFired {
		return
	}
	s.idleAt = s.clock().Add(time.Duration(s.vad.cfg.IdleTimeoutMs) * time.Millisecond)
}

// idleTimeout fires the server-VAD idle flow: input_audio_buffer.timeout_triggered
// (an empty segment: audio_start_ms == audio_end_ms), the commit ladder for a
// user item with a null transcript, and a model response prompting the user to
// continue (history gains the idleTimeoutPlaceholder turn so scenarios can
// match it).
func (s *Session) idleTimeout(ctx context.Context) []Event {
	// Belt-and-braces: refreshVAD disarms the idle deadline whenever the idle
	// timeout stops being configured, but a stale deadline must never panic
	// the read loop or fire under a config that doesn't ask for it.
	if s.vad == nil || s.vad.cfg.Type != "server_vad" || s.vad.cfg.IdleTimeoutMs <= 0 {
		return nil
	}
	s.idleFired = true
	v := s.vad
	prevItem := s.previousItemID()
	itemID := s.nextID("item")
	s.joinTail(itemID)
	at := int(v.totalMs)

	out := []Event{
		{"type": "input_audio_buffer.timeout_triggered",
			"audio_start_ms": at, "audio_end_ms": at, "item_id": itemID},
		{"type": "input_audio_buffer.committed", "previous_item_id": prevItem, "item_id": itemID},
	}
	out = append(out, conversationItemEvents(prevItem, s.rememberItem(map[string]any{
		"id": itemID, "object": "realtime.item", "type": "message", "status": "completed",
		"role": "user", "content": []any{map[string]any{"type": "input_audio", "transcript": nil}},
	}))...)
	s.history = append(s.history, engine.RequestMessage{Role: "user", Content: idleTimeoutPlaceholder})
	return append(out, s.createResponse(ctx, &ClientEvent{})...)
}
