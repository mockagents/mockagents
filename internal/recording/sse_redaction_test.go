package recording

import (
	"strings"
	"testing"
)

func TestRedactorSSEEveryNetworkSplit(t *testing.T) {
	const secret = "ghp_1234567890abcdefghijklmnopqrstuvwxyz"
	frame := "data: {\"token\":\"" + secret + "\"}\n\n"
	r, _ := NewRedactor(nil)
	for split := 1; split < len(frame); split++ {
		it := &Interaction{Streaming: true, StreamEvents: []StreamEvent{
			{DelayMs: 1, Data: frame[:split]}, {DelayMs: 2, Data: frame[split:]},
		}}
		if err := r.Apply(it); err != nil {
			t.Fatalf("split %d: %v", split, err)
		}
		if len(it.StreamEvents) != 1 {
			t.Fatalf("split %d: got %d frames", split, len(it.StreamEvents))
		}
		if strings.Contains(it.StreamEvents[0].Data, secret) {
			t.Fatalf("split %d leaked secret", split)
		}
		if it.StreamEvents[0].DelayMs != 2 {
			t.Fatalf("split %d lost completion delay", split)
		}
	}
}

func TestRedactorSSERejectsIncompleteAndOversizeFrames(t *testing.T) {
	r, _ := NewRedactor(nil)
	incomplete := &Interaction{Streaming: true, StreamEvents: []StreamEvent{{Data: "data: {\"token\":\"sk-secret\"}"}}}
	if err := r.Apply(incomplete); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("incomplete error = %v", err)
	}
	oversize := &Interaction{Streaming: true, StreamEvents: []StreamEvent{{Data: strings.Repeat("x", maxRecordedSSEFrameBytes+1)}}}
	if err := r.Apply(oversize); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversize error = %v", err)
	}
}
