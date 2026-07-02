// Round-6 fidelity regression tests (2026-07-02 eval): idle-timer teardown,
// emission-time previous_item_id, delete tombstones, cancel semantics, and
// the new response.create input / transcription-session surfaces.
package realtime

import (
	"context"
	"testing"
	"time"
)

// R6-1 (S1): disabling turn_detection must disarm the idle timeout — a Tick
// firing afterwards nil-dereferenced the VAD state and killed the session.
func TestIdleTimerDisarmedWhenVADDisabled(t *testing.T) {
	ctx := context.Background()
	s := NewSession("r61", "", fakeGen("ok"))
	fc := newFakeClock()
	s.SetClock(fc.now)
	enableVAD(t, s, `{"audio":{"input":{"turn_detection":{"type":"server_vad","idle_timeout_ms":5000}}}}`)

	endVADTurn(t, s) // burst turn completes → idle deadline armed
	if _, ok := s.NextDeadline(); !ok {
		t.Fatal("setup: idle deadline should be armed")
	}

	enableVAD(t, s, `{"audio":{"input":{"turn_detection":null}}}`)
	if dl, ok := s.NextDeadline(); ok {
		t.Fatalf("idle deadline %v survived disabling VAD", dl)
	}
	// Even if a stale timer fires, Tick must not panic and must stay silent.
	if evs := s.Tick(ctx, fc.advance(10*time.Second)); len(evs) != 0 {
		t.Errorf("Tick after VAD disable emitted %v, want nothing", typesOf(evs))
	}
}
