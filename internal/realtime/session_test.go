package realtime

import (
	"context"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/engine"
)

func fakeGen(content string) Generator {
	return func(_ context.Context, _ /*model*/, _ /*sessionID*/ string, _ []engine.RequestMessage) (*engine.Response, error) {
		return &engine.Response{Content: content}, nil
	}
}

func typesOf(evs []Event) []string {
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i], _ = e["type"].(string)
	}
	return out
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestSession_GreetingAndUpdate(t *testing.T) {
	s := NewSession("s1", "gpt-realtime", fakeGen("ok"))
	g := s.Greeting()
	if len(g) != 1 || g[0]["type"] != "session.created" {
		t.Fatalf("greeting = %v, want session.created", typesOf(g))
	}

	evs := s.Handle(context.Background(), &ClientEvent{Type: "session.update", Session: []byte(`{"voice":"verse","instructions":"be brief"}`)})
	if len(evs) != 1 || evs[0]["type"] != "session.updated" {
		t.Fatalf("update events = %v", typesOf(evs))
	}
	sess := evs[0]["session"].(map[string]any)
	if sess["voice"] != "verse" {
		t.Errorf("voice not applied: %v", sess["voice"])
	}
}

func TestSession_ItemCreateThenResponseLadder(t *testing.T) {
	s := NewSession("s2", "gpt-4o", fakeGen("Hi there!"))

	evs := s.Handle(context.Background(), &ClientEvent{
		Type: "conversation.item.create",
		Item: []byte(`{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}`),
	})
	if len(evs) != 1 || evs[0]["type"] != "conversation.item.created" {
		t.Fatalf("item.create events = %v", typesOf(evs))
	}

	ladder := s.Handle(context.Background(), &ClientEvent{Type: "response.create"})
	tps := typesOf(ladder)
	if tps[0] != "response.created" {
		t.Errorf("ladder must open with response.created, got %v", tps)
	}
	if tps[len(tps)-1] != "response.done" {
		t.Errorf("ladder must end with response.done, got %v", tps)
	}
	for _, want := range []string{"response.output_item.added", "response.content_part.added",
		"response.audio_transcript.delta", "response.audio.delta", "response.audio.done",
		"response.audio_transcript.done", "response.output_item.done"} {
		if !contains(tps, want) {
			t.Errorf("ladder missing %q; got %v", want, tps)
		}
	}

	// The transcript deltas reassemble the engine content; the done event carries
	// the full transcript; the audio deltas are non-empty base64.
	var assembled, doneTranscript string
	sawAudio := false
	for _, e := range ladder {
		switch e["type"] {
		case "response.audio_transcript.delta":
			assembled += e["delta"].(string)
		case "response.audio_transcript.done":
			doneTranscript = e["transcript"].(string)
		case "response.audio.delta":
			if e["delta"].(string) != "" {
				sawAudio = true
			}
		}
	}
	if strings.TrimSpace(assembled) != "Hi there!" {
		t.Errorf("reassembled transcript = %q, want %q", assembled, "Hi there!")
	}
	if doneTranscript != "Hi there!" {
		t.Errorf("done transcript = %q", doneTranscript)
	}
	if !sawAudio {
		t.Error("expected non-empty base64 audio deltas")
	}
}

func TestSession_AudioBufferCommit(t *testing.T) {
	s := NewSession("s3", "", fakeGen("ok"))
	// Commit with nothing buffered is an error.
	if got := s.Handle(context.Background(), &ClientEvent{Type: "input_audio_buffer.commit"}); got[0]["type"] != "error" {
		t.Fatalf("empty commit should error, got %v", typesOf(got))
	}
	// Append then commit produces committed + item.created.
	s.Handle(context.Background(), &ClientEvent{Type: "input_audio_buffer.append", Audio: "AAAA"})
	evs := s.Handle(context.Background(), &ClientEvent{Type: "input_audio_buffer.commit"})
	if !contains(typesOf(evs), "input_audio_buffer.committed") || !contains(typesOf(evs), "conversation.item.created") {
		t.Errorf("commit events = %v", typesOf(evs))
	}
}

func TestSession_UnknownEvent(t *testing.T) {
	s := NewSession("s4", "", fakeGen("ok"))
	evs := s.Handle(context.Background(), &ClientEvent{Type: "totally.bogus"})
	if len(evs) != 1 || evs[0]["type"] != "error" {
		t.Fatalf("unknown event should yield one error, got %v", typesOf(evs))
	}
}

func TestSynthAudioDeterministic(t *testing.T) {
	a, b := synthAudioChunk("hello "), synthAudioChunk("hello ")
	if a != b {
		t.Error("synthAudioChunk must be deterministic")
	}
	if a == "" || a == synthAudioChunk("world") {
		t.Error("synthAudioChunk must be non-empty and input-dependent")
	}
}
