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

// paced reports whether responses should be emitted incrementally: the
// transport opted in AND the client enabled turn detection.
func (s *Session) paced() bool { return s.paceInterval > 0 && s.vad != nil }

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
	var out []Event
	for s.inflight != nil && !s.inflight.nextAt.After(now) {
		inf := s.inflight
		ev := inf.queue[0]
		inf.queue = inf.queue[1:]
		out = append(out, ev)
		if ev["type"] == "response.output_item.done" {
			inf.doneItems = append(inf.doneItems, ev["item"])
		}
		if len(inf.queue) == 0 {
			if inf.onDone != nil {
				inf.onDone()
			}
			s.inflight = nil
			s.armIdleTimer()
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
	s.inflight = &inflightResponse{
		respID: respID, rc: rc, queue: queue,
		nextAt: s.clock().Add(s.paceInterval),
		onDone: onDone,
	}
	return immediate
}

// cancelInflight aborts the paced response: the rest of its ladder is dropped
// and response.done reports status "cancelled" with the given
// status_details.reason ("turn_detected" for VAD barge-in, "client_cancelled"
// for response.cancel). Output lists only the items that completed.
func (s *Session) cancelInflight(reason string) []Event {
	inf := s.inflight
	s.inflight = nil
	items := inf.doneItems
	if items == nil {
		items = []any{}
	}
	cancelled := s.responseObject(inf.respID, "cancelled", items, inf.rc)
	cancelled["status_details"] = map[string]any{"type": "cancelled", "reason": reason}
	return []Event{{"type": "response.done", "response": cancelled}}
}

// armIdleTimer schedules the server-VAD idle timeout after a response
// completes. Simplification vs GA (documented in the design doc): the deadline
// is response.done + idle_timeout_ms — the mock's synthesized audio has no real
// playback duration to add.
func (s *Session) armIdleTimer() {
	// "Idle timeout is currently only supported for server_vad mode" (GA);
	// the SemanticVad config has no such field.
	if s.vad == nil || s.vad.cfg.IdleTimeoutMs <= 0 || s.vad.cfg.Type != "server_vad" {
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
	s.idleFired = true
	v := s.vad
	prevItem := s.previousItemID()
	itemID := s.nextID("item")
	s.lastItemID = itemID
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
