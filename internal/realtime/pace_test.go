package realtime

import (
	"context"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
)

type fakeClock struct{ t time.Time }

func newFakeClock() *fakeClock                          { return &fakeClock{t: time.Unix(1000, 0)} }
func (f *fakeClock) now() time.Time                     { return f.t }
func (f *fakeClock) advance(d time.Duration) time.Time { f.t = f.t.Add(d); return f.t }

// pacedSession builds a VAD session with paced emission and a fake clock.
func pacedSession(t *testing.T, gen Generator, turnDetection string) (*Session, *fakeClock) {
	t.Helper()
	s := NewSession("sp", "gpt-realtime", gen)
	fc := newFakeClock()
	s.SetClock(fc.now)
	s.SetPacing(10 * time.Millisecond)
	enableVAD(t, s, turnDetection)
	return s, fc
}

// endVADTurn streams speech then enough silence to end the turn, returning the
// turn-end events.
func endVADTurn(t *testing.T, s *Session) []Event {
	t.Helper()
	ctx := context.Background()
	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(200, speechAmp)})
	return s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(600, quietAmp)})
}

// drain advances the fake clock in steps, Ticking until no deadline remains
// (or max steps), collecting everything emitted.
func drain(t *testing.T, s *Session, fc *fakeClock, max int) []Event {
	t.Helper()
	ctx := context.Background()
	var out []Event
	for range max {
		if _, ok := s.NextDeadline(); !ok {
			return out
		}
		out = append(out, s.Tick(ctx, fc.advance(10*time.Millisecond))...)
	}
	return out
}

// A paced response returns only response.created + rate_limits immediately;
// the rest of the ladder drains via Tick, and the transcript joins history
// only when the response completes.
func TestPaced_DrainsViaTick(t *testing.T) {
	s, fc := pacedSession(t, fakeGen("Hi there friend"), serverVAD)

	evs := endVADTurn(t, s)
	tps := typesOf(evs)
	if tps[len(tps)-2] != "response.created" || tps[len(tps)-1] != "rate_limits.updated" {
		t.Fatalf("paced turn-end must stop after created+rate_limits; got %v", tps)
	}
	if contains(tps, "response.done") {
		t.Fatal("paced response.done must not be emitted synchronously")
	}
	if _, ok := s.NextDeadline(); !ok {
		t.Fatal("a paced response must expose a deadline")
	}
	// The transcript is not in history until the response completes.
	for _, m := range s.history {
		if m.Role == "assistant" {
			t.Fatalf("assistant transcript in history before completion: %+v", s.history)
		}
	}

	rest := drain(t, s, fc, 100)
	rtps := typesOf(rest)
	if rtps[len(rtps)-1] != "response.done" {
		t.Fatalf("drained ladder must end with response.done; got %v", rtps)
	}
	for _, want := range []string{"response.output_item.added", "response.output_audio.delta", "response.output_item.done"} {
		if !contains(rtps, want) {
			t.Errorf("drained ladder missing %q; got %v", want, rtps)
		}
	}
	for _, ev := range rest {
		if _, ok := ev["event_id"]; !ok {
			t.Fatalf("Ticked event missing event_id: %v", ev)
		}
	}
	found := false
	for _, m := range s.history {
		if m.Role == "assistant" && m.Content == "Hi there friend" {
			found = true
		}
	}
	if !found {
		t.Errorf("completed response's transcript missing from history: %+v", s.history)
	}
	if _, ok := s.NextDeadline(); ok {
		t.Error("no deadline should remain after completion (no idle timeout configured)")
	}
}

// Barge-in: a VAD speech start mid-response cancels it — speech_started is
// followed by response.done status "cancelled" / reason "turn_detected", and
// the cancelled transcript never reaches history.
func TestPaced_BargeIn(t *testing.T) {
	s, fc := pacedSession(t, fakeGen("A long answer that gets interrupted"), serverVAD)
	endVADTurn(t, s)
	s.Tick(context.Background(), fc.advance(20*time.Millisecond)) // a couple of ladder events out

	evs := s.Handle(context.Background(), &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(100, speechAmp)})
	tps := typesOf(evs)
	if tps[0] != "input_audio_buffer.speech_started" {
		t.Fatalf("barge-in events = %v, want speech_started first", tps)
	}
	done := firstEvent(evs, "response.done")
	if done == nil {
		t.Fatalf("barge-in must cancel the in-flight response; got %v", tps)
	}
	resp := done["response"].(map[string]any)
	if resp["status"] != "cancelled" {
		t.Errorf("cancelled status = %v", resp["status"])
	}
	sd := resp["status_details"].(map[string]any)
	if sd["type"] != "cancelled" || sd["reason"] != "turn_detected" {
		t.Errorf("status_details = %v, want cancelled/turn_detected", sd)
	}
	if _, ok := s.NextDeadline(); ok {
		t.Error("no deadline should survive cancellation")
	}
	for _, m := range s.history {
		if m.Role == "assistant" {
			t.Errorf("cancelled transcript leaked into history: %+v", s.history)
		}
	}
}

// response.cancel cancels an in-flight paced response (client_cancelled); with
// nothing in flight it keeps the cancel-specific error.
func TestPaced_ClientCancel(t *testing.T) {
	s, fc := pacedSession(t, fakeGen("to be cancelled"), serverVAD)
	endVADTurn(t, s)

	evs := s.Handle(context.Background(), &ClientEvent{Type: "response.cancel"})
	done := firstEvent(evs, "response.done")
	if done == nil {
		t.Fatalf("response.cancel mid-flight must yield response.done; got %v", typesOf(evs))
	}
	sd := done["response"].(map[string]any)["status_details"].(map[string]any)
	if sd["reason"] != "client_cancelled" {
		t.Errorf("reason = %v, want client_cancelled", sd["reason"])
	}

	// Nothing in flight anymore → the specific error again.
	evs = s.Handle(context.Background(), &ClientEvent{Type: "response.cancel"})
	if evs[0]["type"] != "error" || evs[0]["error"].(map[string]any)["code"] != "response_cancel_not_active" {
		t.Errorf("post-completion cancel = %v, want response_cancel_not_active", evs[0])
	}
	_ = fc
}

// interrupt_response:false lets the in-flight response survive a barge-in and
// complete; the new turn's auto-response is skipped while one is in flight.
func TestPaced_InterruptResponseFalse(t *testing.T) {
	s, fc := pacedSession(t, fakeGen("uninterruptible"),
		`{"audio":{"input":{"turn_detection":{"type":"server_vad","interrupt_response":false}}}}`)
	endVADTurn(t, s)

	evs := s.Handle(context.Background(), &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(100, speechAmp)})
	if firstEvent(evs, "response.done") != nil {
		t.Fatalf("interrupt_response:false must not cancel; got %v", typesOf(evs))
	}
	// The second turn ends while the first response is still in flight: it
	// commits but does not stack a second response.
	evs = s.Handle(context.Background(), &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(600, quietAmp)})
	tps := typesOf(evs)
	if !contains(tps, "input_audio_buffer.committed") || contains(tps, "response.created") {
		t.Fatalf("second turn during inflight = %v, want commit without a new response", tps)
	}

	rest := drain(t, s, fc, 100)
	rtps := typesOf(rest)
	if rtps[len(rtps)-1] != "response.done" {
		t.Fatalf("the surviving response must complete; got %v", rtps)
	}
	if st := firstEvent(rest, "response.done")["response"].(map[string]any)["status"]; st != "completed" {
		t.Errorf("surviving response status = %v, want completed", st)
	}
}

// idle_timeout_ms: after a completed response, the deadline fires the GA idle
// flow — timeout_triggered (empty segment), a null-transcript user item, and a
// model response prompting the user to continue.
func TestIdleTimeout(t *testing.T) {
	var lastUser string
	gen := func(_ context.Context, _, _ string, history []engine.RequestMessage) (*engine.Response, error) {
		for _, m := range history {
			if m.Role == "user" {
				lastUser = m.Content
			}
		}
		return &engine.Response{Content: "are you still there?"}, nil
	}
	// Burst mode (no pacing): idle timers work independently of pacing.
	s := NewSession("si", "", gen)
	fc := newFakeClock()
	s.SetClock(fc.now)
	enableVAD(t, s, `{"audio":{"input":{"turn_detection":{"type":"server_vad","idle_timeout_ms":5000}}}}`)

	endVADTurn(t, s) // full burst turn, arms the idle deadline at response.done
	deadline, ok := s.NextDeadline()
	if !ok || deadline != fc.now().Add(5*time.Second) {
		t.Fatalf("idle deadline = %v (ok=%v), want now+5s", deadline, ok)
	}

	evs := s.Tick(context.Background(), fc.advance(5*time.Second))
	tps := typesOf(evs)
	trig := firstEvent(evs, "input_audio_buffer.timeout_triggered")
	if trig == nil {
		t.Fatalf("idle Tick events = %v, want timeout_triggered", tps)
	}
	if trig["audio_start_ms"] != trig["audio_end_ms"] {
		t.Errorf("timeout segment must be empty: %v", trig)
	}
	added := firstEvent(evs, "conversation.item.added")
	if added == nil {
		t.Fatal("idle flow must add a user item")
	}
	content := added["item"].(map[string]any)["content"].([]any)[0].(map[string]any)
	if content["transcript"] != nil {
		t.Errorf("idle item transcript = %v, want null", content["transcript"])
	}
	for _, want := range []string{"response.created", "response.done"} {
		if !contains(tps, want) {
			t.Errorf("idle flow missing %q; got %v", want, tps)
		}
	}
	if lastUser != idleTimeoutPlaceholder {
		t.Errorf("engine saw last user turn %q, want %q", lastUser, idleTimeoutPlaceholder)
	}
}

// User activity clears an armed idle timeout.
func TestIdleTimeout_ClearedByActivity(t *testing.T) {
	s := NewSession("si2", "", fakeGen("ok"))
	fc := newFakeClock()
	s.SetClock(fc.now)
	enableVAD(t, s, `{"audio":{"input":{"turn_detection":{"type":"server_vad","idle_timeout_ms":5000}}}}`)

	endVADTurn(t, s)
	if _, ok := s.NextDeadline(); !ok {
		t.Fatal("idle deadline should be armed after the response")
	}
	// The user starts talking again — the timeout must not fire.
	s.Handle(context.Background(), &ClientEvent{Type: "input_audio_buffer.append", Audio: pcmChunk(100, speechAmp)})
	if dl, ok := s.NextDeadline(); ok {
		t.Errorf("idle deadline %v survived user speech", dl)
	}
}
