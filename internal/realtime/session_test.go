package realtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/types"
)

func fakeGen(content string) Generator {
	return func(_ context.Context, _ /*model*/, _ /*sessionID*/ string, _ []engine.RequestMessage) (*engine.Response, error) {
		return &engine.Response{Content: content}, nil
	}
}

func fakeGenTool(content string, calls ...types.ToolCallSpec) Generator {
	return func(_ context.Context, _, _ string, _ []engine.RequestMessage) (*engine.Response, error) {
		return &engine.Response{Content: content, ToolCalls: calls}, nil
	}
}

func firstEvent(evs []Event, typ string) Event {
	for _, e := range evs {
		if e["type"] == typ {
			return e
		}
	}
	return nil
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
		"response.output_audio_transcript.delta", "response.output_audio.delta", "response.output_audio.done",
		"response.output_audio_transcript.done", "response.output_item.done"} {
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
		case "response.output_audio_transcript.delta":
			assembled += e["delta"].(string)
		case "response.output_audio_transcript.done":
			doneTranscript = e["transcript"].(string)
		case "response.output_audio.delta":
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

func TestSession_FunctionCallLadder(t *testing.T) {
	s := NewSession("s", "gpt-realtime",
		fakeGenTool("Let me check the weather.", types.ToolCallSpec{Name: "get_weather", Arguments: map[string]any{"city": "Paris"}}))
	ev := s.Handle(context.Background(), &ClientEvent{Type: "response.create"})
	tps := typesOf(ev)

	for _, want := range []string{
		"response.output_item.added",
		"response.function_call_arguments.delta",
		"response.function_call_arguments.done",
	} {
		if !contains(tps, want) {
			t.Errorf("ladder missing %q; got %v", want, tps)
		}
	}
	// Content was present, so an audio message item precedes the function call.
	if !contains(tps, "response.output_audio.delta") {
		t.Errorf("expected an audio message item alongside the tool call; got %v", tps)
	}

	// The streamed argument deltas reassemble into the .done arguments.
	var assembled, doneArgs string
	for _, e := range ev {
		switch e["type"] {
		case "response.function_call_arguments.delta":
			assembled += e["delta"].(string)
		case "response.function_call_arguments.done":
			doneArgs = e["arguments"].(string)
		}
	}
	if assembled != doneArgs {
		t.Errorf("reassembled args %q != done args %q", assembled, doneArgs)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(doneArgs), &got); err != nil || got["city"] != "Paris" {
		t.Errorf("function call arguments = %q, want {\"city\":\"Paris\"}", doneArgs)
	}

	// response.done lists both items; the function_call carries name + call_id.
	done := firstEvent(ev, "response.done")
	output := done["response"].(map[string]any)["output"].([]any)
	if len(output) != 2 {
		t.Fatalf("response.done output should have message + function_call, got %d", len(output))
	}
	fc := output[1].(map[string]any)
	if fc["type"] != "function_call" || fc["name"] != "get_weather" || fc["call_id"] == "" {
		t.Errorf("function_call item malformed: %v", fc)
	}
}

func TestSession_ToolCallOnlyNoMessage(t *testing.T) {
	s := NewSession("s", "gpt-realtime", fakeGenTool("", types.ToolCallSpec{Name: "lookup"}))
	ev := s.Handle(context.Background(), &ClientEvent{Type: "response.create"})
	tps := typesOf(ev)
	if contains(tps, "response.output_audio.delta") {
		t.Errorf("a content-free tool-call turn should emit no audio message; got %v", tps)
	}
	if !contains(tps, "response.function_call_arguments.done") {
		t.Errorf("expected function call events; got %v", tps)
	}
	done := firstEvent(ev, "response.done")
	output := done["response"].(map[string]any)["output"].([]any)
	if len(output) != 1 || output[0].(map[string]any)["type"] != "function_call" {
		t.Errorf("expected a single function_call item, got %v", output)
	}
}

func TestSession_TextOnlyModality(t *testing.T) {
	s := NewSession("s", "gpt-realtime", fakeGen("Hello in text."))
	s.Handle(context.Background(), &ClientEvent{Type: "session.update", Session: []byte(`{"output_modalities":["text"]}`)})
	ev := s.Handle(context.Background(), &ClientEvent{Type: "response.create"})
	tps := typesOf(ev)

	if !contains(tps, "response.output_text.delta") || !contains(tps, "response.output_text.done") {
		t.Errorf("text-only response must stream output_text events; got %v", tps)
	}
	if contains(tps, "response.output_audio.delta") {
		t.Errorf("text-only response must not stream audio; got %v", tps)
	}
	// The content part is output_text.
	part := firstEvent(ev, "response.content_part.added")["part"].(map[string]any)
	if part["type"] != "output_text" {
		t.Errorf("content part type = %v, want output_text", part["type"])
	}
}

func TestSession_GASessionObject(t *testing.T) {
	s := NewSession("s", "gpt-realtime", fakeGen("ok"))
	sess := s.Greeting()[0]["session"].(map[string]any)
	if sess["type"] != "realtime" {
		t.Errorf("session.type = %v, want realtime", sess["type"])
	}
	if _, ok := sess["output_modalities"]; !ok {
		t.Error("session object missing output_modalities (GA field)")
	}
}

func TestSession_InputAudioTranscription(t *testing.T) {
	// Without transcription configured: committed item has a null transcript and
	// no transcription.completed event.
	s := NewSession("s", "gpt-realtime", fakeGen("ok"))
	s.Handle(context.Background(), &ClientEvent{Type: "input_audio_buffer.append", Audio: "AAAA"})
	ev := s.Handle(context.Background(), &ClientEvent{Type: "input_audio_buffer.commit"})
	if contains(typesOf(ev), "conversation.item.input_audio_transcription.completed") {
		t.Error("transcription event emitted without input_audio_transcription configured")
	}

	// With transcription enabled: the event fires and carries the transcript.
	s2 := NewSession("s2", "gpt-realtime", fakeGen("ok"))
	s2.Handle(context.Background(), &ClientEvent{Type: "session.update", Session: []byte(`{"input_audio_transcription":{"model":"whisper-1"}}`)})
	s2.Handle(context.Background(), &ClientEvent{Type: "input_audio_buffer.append", Audio: "AAAA"})
	ev2 := s2.Handle(context.Background(), &ClientEvent{Type: "input_audio_buffer.commit"})
	tc := firstEvent(ev2, "conversation.item.input_audio_transcription.completed")
	if tc == nil {
		t.Fatalf("expected transcription.completed event; got %v", typesOf(ev2))
	}
	if tc["transcript"] == "" || tc["transcript"] == nil {
		t.Error("transcription.completed must carry a transcript")
	}
}

func TestSession_EventsHaveEventID(t *testing.T) {
	s := NewSession("s", "gpt-realtime", fakeGenTool("Checking.", types.ToolCallSpec{Name: "t", Arguments: map[string]any{"a": 1}}))
	// Greeting + a full response ladder (message + function call) must all carry
	// a non-empty event_id — a required field on every Realtime server event.
	var all []Event
	all = append(all, s.Greeting()...)
	all = append(all, s.Handle(context.Background(), &ClientEvent{Type: "response.create"})...)
	if len(all) == 0 {
		t.Fatal("no events produced")
	}
	seen := map[string]bool{}
	for _, e := range all {
		id, _ := e["event_id"].(string)
		if id == "" {
			t.Fatalf("event %v missing event_id", e["type"])
		}
		if seen[id] {
			t.Errorf("duplicate event_id %q", id)
		}
		seen[id] = true
	}
}
