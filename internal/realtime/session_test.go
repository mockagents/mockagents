package realtime

import (
	"context"
	"encoding/json"
	"errors"
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

	// A beta-style top-level voice is accepted and echoed at the GA location
	// (audio.output.voice).
	evs := s.Handle(context.Background(), &ClientEvent{Type: "session.update", Session: []byte(`{"voice":"verse","instructions":"be brief"}`)})
	if len(evs) != 1 || evs[0]["type"] != "session.updated" {
		t.Fatalf("update events = %v", typesOf(evs))
	}
	sess := evs[0]["session"].(map[string]any)
	if v := audioOutVoice(sess); v != "verse" {
		t.Errorf("voice not applied at audio.output.voice: %v", v)
	}
	if _, top := sess["voice"]; top {
		t.Error("GA session object must not carry a top-level voice")
	}
}

// audioOutVoice digs out session.audio.output.voice from a session object.
func audioOutVoice(sess map[string]any) any {
	audio, _ := sess["audio"].(map[string]any)
	out, _ := audio["output"].(map[string]any)
	return out["voice"]
}

func TestSession_ItemCreateThenResponseLadder(t *testing.T) {
	s := NewSession("s2", "gpt-4o", fakeGen("Hi there!"))

	evs := s.Handle(context.Background(), &ClientEvent{
		Type: "conversation.item.create",
		Item: []byte(`{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}`),
	})
	// GA announces a new item with the added → done pair (not the beta
	// conversation.item.created).
	if len(evs) != 2 || evs[0]["type"] != "conversation.item.added" || evs[1]["type"] != "conversation.item.done" {
		t.Fatalf("item.create events = %v, want [conversation.item.added conversation.item.done]", typesOf(evs))
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
	// Append then commit produces committed + the item added/done pair.
	s.Handle(context.Background(), &ClientEvent{Type: "input_audio_buffer.append", Audio: "AAAA"})
	evs := s.Handle(context.Background(), &ClientEvent{Type: "input_audio_buffer.commit"})
	for _, want := range []string{"input_audio_buffer.committed", "conversation.item.added", "conversation.item.done"} {
		if !contains(typesOf(evs), want) {
			t.Errorf("commit events missing %q: %v", want, typesOf(evs))
		}
	}
}

func TestSession_PreviousItemIDChains(t *testing.T) {
	ctx := context.Background()
	s := NewSession("sp", "gpt-realtime", fakeGen("Hi!"))

	itemCreate := &ClientEvent{
		Type: "conversation.item.create",
		Item: []byte(`{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}`),
	}

	// First item: previous_item_id is present and null (this is the first item)
	// on both halves of the added/done pair.
	first := s.Handle(ctx, itemCreate)
	added := firstEvent(first, "conversation.item.added")
	prev, ok := added["previous_item_id"]
	if !ok {
		t.Fatal("conversation.item.added missing previous_item_id")
	}
	if prev != nil {
		t.Errorf("first item previous_item_id = %v, want null", prev)
	}
	if done := firstEvent(first, "conversation.item.done"); done["previous_item_id"] != nil {
		t.Errorf("conversation.item.done previous_item_id = %v, want null", done["previous_item_id"])
	}
	firstID := added["item"].(map[string]any)["id"].(string)

	// Second item chains off the first.
	second := firstEvent(s.Handle(ctx, itemCreate), "conversation.item.added")
	if second["previous_item_id"] != firstID {
		t.Errorf("second item previous_item_id = %v, want %q", second["previous_item_id"], firstID)
	}
	secondID := second["item"].(map[string]any)["id"].(string)

	// A response's output item joins the conversation, so the next user turn
	// chains off the response item — not the last user item.
	ladder := s.Handle(ctx, &ClientEvent{Type: "response.create"})
	var respItemID string
	for _, e := range ladder {
		if e["type"] == "response.output_item.done" {
			respItemID = e["item"].(map[string]any)["id"].(string)
		}
	}
	if respItemID == "" || respItemID == secondID {
		t.Fatalf("expected a distinct response output item id, got %q", respItemID)
	}
	third := firstEvent(s.Handle(ctx, itemCreate), "conversation.item.added")
	if third["previous_item_id"] != respItemID {
		t.Errorf("post-response item previous_item_id = %v, want %q", third["previous_item_id"], respItemID)
	}
}

func TestSession_CommitCarriesPreviousItemID(t *testing.T) {
	ctx := context.Background()
	s := NewSession("sc", "", fakeGen("ok"))
	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: "AAAA"})
	evs := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.commit"})

	committed := firstEvent(evs, "input_audio_buffer.committed")
	added := firstEvent(evs, "conversation.item.added")
	// Both events report the same (null, on the first turn) previous_item_id.
	if p, ok := committed["previous_item_id"]; !ok || p != nil {
		t.Errorf("committed previous_item_id = %v (present=%v), want null", committed["previous_item_id"], ok)
	}
	if p, ok := added["previous_item_id"]; !ok || p != nil {
		t.Errorf("added previous_item_id = %v (present=%v), want null", added["previous_item_id"], ok)
	}
	firstID := added["item"].(map[string]any)["id"].(string)

	// A second commit chains off the first committed item.
	s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.append", Audio: "BBBB"})
	evs2 := s.Handle(ctx, &ClientEvent{Type: "input_audio_buffer.commit"})
	if p := firstEvent(evs2, "input_audio_buffer.committed")["previous_item_id"]; p != firstID {
		t.Errorf("second committed previous_item_id = %v, want %q", p, firstID)
	}
	if p := firstEvent(evs2, "conversation.item.added")["previous_item_id"]; p != firstID {
		t.Errorf("second added previous_item_id = %v, want %q", p, firstID)
	}
}

// GA mirrors response output items into the conversation: each output item's
// response.output_item.added/.done is followed by a conversation.item.added/.done
// carrying previous_item_id, so a conversation event log chains correctly.
func TestSession_ResponseLadder_ConversationItemMirror(t *testing.T) {
	ctx := context.Background()
	s := NewSession("sm", "gpt-realtime",
		fakeGenTool("Checking.", types.ToolCallSpec{Name: "get_weather", Arguments: map[string]any{"city": "Paris"}}))

	created := firstEvent(s.Handle(ctx, &ClientEvent{
		Type: "conversation.item.create",
		Item: []byte(`{"type":"message","role":"user","content":[{"type":"input_text","text":"weather?"}]}`),
	}), "conversation.item.added")
	userItemID := created["item"].(map[string]any)["id"].(string)

	ladder := s.Handle(ctx, &ClientEvent{Type: "response.create"})
	tps := typesOf(ladder)

	// Every response.output_item.added/.done is immediately mirrored.
	var added, done []Event
	for i, e := range ladder {
		switch e["type"] {
		case "response.output_item.added":
			if i+1 >= len(tps) || tps[i+1] != "conversation.item.added" {
				t.Errorf("event after output_item.added (index %d) should be conversation.item.added, ladder = %v", i, tps)
			}
		case "response.output_item.done":
			if i+1 >= len(tps) || tps[i+1] != "conversation.item.done" {
				t.Errorf("event after output_item.done (index %d) should be conversation.item.done, ladder = %v", i, tps)
			}
		case "conversation.item.added":
			added = append(added, e)
		case "conversation.item.done":
			done = append(done, e)
		}
	}
	if len(added) != 2 || len(done) != 2 {
		t.Fatalf("mirror counts: added=%d done=%d, want 2/2 (message + function_call)", len(added), len(done))
	}

	// The message item chains off the user item; the function_call chains off the
	// message item; done events repeat the prev id and carry the completed item.
	msgID := added[0]["item"].(map[string]any)["id"].(string)
	if added[0]["previous_item_id"] != userItemID {
		t.Errorf("message mirror previous_item_id = %v, want %q", added[0]["previous_item_id"], userItemID)
	}
	if added[1]["previous_item_id"] != msgID {
		t.Errorf("function_call mirror previous_item_id = %v, want %q", added[1]["previous_item_id"], msgID)
	}
	for i, d := range done {
		if st := d["item"].(map[string]any)["status"]; st != "completed" {
			t.Errorf("done[%d] item status = %v, want completed", i, st)
		}
		if d["previous_item_id"] != added[i]["previous_item_id"] {
			t.Errorf("done[%d] previous_item_id = %v, want %v", i, d["previous_item_id"], added[i]["previous_item_id"])
		}
	}
}

// The tool loop: a client answers a function_call with a function_call_output
// item. The ack must echo the real item shape (not a message), and the output
// must reach engine history as a tool turn so follow-up scenario matching sees it.
func TestSession_FunctionCallOutputItem(t *testing.T) {
	ctx := context.Background()
	var gotHistory []engine.RequestMessage
	gen := func(_ context.Context, _, _ string, history []engine.RequestMessage) (*engine.Response, error) {
		gotHistory = history
		return &engine.Response{Content: "The weather is sunny."}, nil
	}
	s := NewSession("st", "gpt-realtime", gen)

	evs := s.Handle(ctx, &ClientEvent{
		Type: "conversation.item.create",
		Item: []byte(`{"type":"function_call_output","call_id":"call_1","output":"{\"temp\":22}"}`),
	})
	added := firstEvent(evs, "conversation.item.added")
	if added == nil {
		t.Fatalf("no conversation.item.added; events = %v", typesOf(evs))
	}
	item := added["item"].(map[string]any)
	if item["type"] != "function_call_output" || item["call_id"] != "call_1" || item["output"] != `{"temp":22}` {
		t.Errorf("ack item = %v, want a function_call_output echo", item)
	}
	if _, hasRole := item["role"]; hasRole {
		t.Error("function_call_output ack must not carry a message role")
	}

	// The tool result joins engine history as a tool turn (the same mapping the
	// Responses adapters use), visible to the next response.create.
	s.Handle(ctx, &ClientEvent{Type: "response.create"})
	found := false
	for _, m := range gotHistory {
		if m.Role == "tool" && m.Content == `{"temp":22}` {
			found = true
		}
	}
	if !found {
		t.Errorf("tool output not in engine history as a tool turn: %+v", gotHistory)
	}
}

// An echoed prior function_call item (context replay) is acked with the real
// function_call shape, not rewritten into a message.
func TestSession_FunctionCallItemEcho(t *testing.T) {
	s := NewSession("sf", "", fakeGen("ok"))
	evs := s.Handle(context.Background(), &ClientEvent{
		Type: "conversation.item.create",
		Item: []byte(`{"type":"function_call","call_id":"call_9","name":"get_weather","arguments":"{}"}`),
	})
	item := firstEvent(evs, "conversation.item.added")["item"].(map[string]any)
	if item["type"] != "function_call" || item["name"] != "get_weather" || item["call_id"] != "call_9" {
		t.Errorf("function_call ack item = %v", item)
	}
}

func TestSession_UnknownEvent(t *testing.T) {
	s := NewSession("s4", "", fakeGen("ok"))
	evs := s.Handle(context.Background(), &ClientEvent{Type: "totally.bogus"})
	if len(evs) != 1 || evs[0]["type"] != "error" {
		t.Fatalf("unknown event should yield one error, got %v", typesOf(evs))
	}
}

// GA error objects carry five fields; error.event_id echoes the id of the
// client event that caused the error (the SDK correlation handle).
func TestSession_ErrorEchoesClientEventID(t *testing.T) {
	s := NewSession("s", "", fakeGen("ok"))
	evs := s.Handle(context.Background(), &ClientEvent{Type: "totally.bogus", EventID: "evt_42"})
	e := evs[0]["error"].(map[string]any)
	if e["event_id"] != "evt_42" {
		t.Errorf("error.event_id = %v, want evt_42", e["event_id"])
	}
	if p, ok := e["param"]; !ok || p != nil {
		t.Errorf("error.param = %v (present=%v), want present and null", p, ok)
	}
	// Without a client event_id the echo is null, not absent.
	evs = s.Handle(context.Background(), &ClientEvent{Type: "totally.bogus"})
	e = evs[0]["error"].(map[string]any)
	if id, ok := e["event_id"]; !ok || id != nil {
		t.Errorf("error.event_id = %v (present=%v), want present and null", id, ok)
	}
}

// response.cancel with nothing in flight mirrors the real API's cancel-specific
// error code (which SDKs suppress), not unknown_event.
func TestSession_ResponseCancelNotActive(t *testing.T) {
	s := NewSession("s", "", fakeGen("ok"))
	evs := s.Handle(context.Background(), &ClientEvent{Type: "response.cancel", EventID: "evt_9"})
	if len(evs) != 1 || evs[0]["type"] != "error" {
		t.Fatalf("response.cancel events = %v, want one error", typesOf(evs))
	}
	e := evs[0]["error"].(map[string]any)
	if e["code"] != "response_cancel_not_active" {
		t.Errorf("error.code = %v, want response_cancel_not_active", e["code"])
	}
	if e["event_id"] != "evt_9" {
		t.Errorf("error.event_id = %v, want evt_9", e["event_id"])
	}
}

// A response that cannot be generated still closes the ladder: response.done is
// ALWAYS emitted (status "failed" + status_details.error), with a server_error
// event carrying the detail — a client awaiting response.done must not hang.
func TestSession_GenerationFailureLadder(t *testing.T) {
	genErr := func(_ context.Context, _, _ string, _ []engine.RequestMessage) (*engine.Response, error) {
		return nil, errors.New("agent exploded")
	}
	s := NewSession("s", "", genErr)
	evs := s.Handle(context.Background(), &ClientEvent{Type: "response.create"})
	tps := typesOf(evs)
	if tps[0] != "response.created" || tps[len(tps)-1] != "response.done" {
		t.Fatalf("failure ladder = %v, want response.created … response.done", tps)
	}
	done := firstEvent(evs, "response.done")["response"].(map[string]any)
	if done["status"] != "failed" {
		t.Errorf("response.status = %v, want failed", done["status"])
	}
	sd := done["status_details"].(map[string]any)
	if sd["type"] != "failed" || sd["error"].(map[string]any)["type"] != "server_error" {
		t.Errorf("status_details = %v, want type failed + server_error", sd)
	}
	errBody := firstEvent(evs, "error")["error"].(map[string]any)
	if errBody["type"] != "server_error" || errBody["message"] != "agent exploded" {
		t.Errorf("error body = %v, want server_error with the engine message", errBody)
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

// A scenario may plant malformed JSON via raw_arguments (FB-03) to exercise a
// client's tool-arg parser; the Realtime ladder must emit it verbatim rather
// than marshaling the structured Arguments (parity with the OpenAI adapter).
func TestSession_FunctionCallLadder_RawArguments(t *testing.T) {
	const raw = `{"city":` // deliberately invalid JSON
	s := NewSession("s", "gpt-realtime",
		fakeGenTool("", types.ToolCallSpec{
			Name:         "get_weather",
			Arguments:    map[string]any{"city": "Paris"}, // must be ignored when RawArguments is set
			RawArguments: raw,
		}))
	ev := s.Handle(context.Background(), &ClientEvent{Type: "response.create"})

	done := firstEvent(ev, "response.function_call_arguments.done")
	if done == nil || done["arguments"].(string) != raw {
		t.Errorf("arguments.done = %v, want verbatim %q", done["arguments"], raw)
	}
	final := firstEvent(ev, "response.done")
	output := final["response"].(map[string]any)["output"].([]any)
	fc := output[len(output)-1].(map[string]any)
	if fc["arguments"].(string) != raw {
		t.Errorf("function_call item arguments = %q, want verbatim %q", fc["arguments"], raw)
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
	// The content_part events use the SHORT part type ("text"), unlike item
	// content which uses "output_text" — the GA API is asymmetric here.
	part := firstEvent(ev, "response.content_part.added")["part"].(map[string]any)
	if part["type"] != "text" {
		t.Errorf("content part type = %v, want text", part["type"])
	}
	// Item content keeps the long name.
	item := firstEvent(ev, "response.output_item.done")["item"].(map[string]any)
	content := item["content"].([]any)[0].(map[string]any)
	if content["type"] != "output_text" {
		t.Errorf("item content type = %v, want output_text", content["type"])
	}
}

func TestSession_GASessionObject(t *testing.T) {
	s := NewSession("s", "gpt-realtime", fakeGen("ok"))
	sess := s.Greeting()[0]["session"].(map[string]any)
	if sess["type"] != "realtime" {
		t.Errorf("session.type = %v, want realtime", sess["type"])
	}
	for _, k := range []string{"output_modalities", "instructions", "tools", "tool_choice", "max_output_tokens", "audio"} {
		if _, ok := sess[k]; !ok {
			t.Errorf("GA session object missing %q", k)
		}
	}
	// GA default output_modalities is ["audio"] — never ["audio","text"].
	if mods, _ := sess["output_modalities"].([]string); len(mods) != 1 || mods[0] != "audio" {
		t.Errorf("default output_modalities = %v, want [audio]", sess["output_modalities"])
	}
	// The beta top-level voice/modalities must NOT be present.
	if _, ok := sess["voice"]; ok {
		t.Error("GA session object must not carry top-level voice")
	}
	if _, ok := sess["modalities"]; ok {
		t.Error("GA session object must not carry the beta modalities alias")
	}
	// Voice + format live under audio.output; transcription/turn_detection under input.
	audio := sess["audio"].(map[string]any)
	out := audio["output"].(map[string]any)
	if out["voice"] != "alloy" {
		t.Errorf("default voice = %v, want alloy (under audio.output)", out["voice"])
	}
	if _, ok := out["format"]; !ok {
		t.Error("audio.output missing format")
	}
	if _, ok := audio["input"].(map[string]any)["turn_detection"]; !ok {
		t.Error("audio.input missing turn_detection")
	}
}

func TestSession_GANestedRoundTrip(t *testing.T) {
	s := NewSession("s", "gpt-realtime", fakeGen("ok"))
	// A GA client sets voice + speed under audio.output and transcription under
	// audio.input; the server echoes them at the same GA locations.
	s.Handle(context.Background(), &ClientEvent{Type: "session.update", Session: []byte(
		`{"audio":{"output":{"voice":"marin","speed":1.5},"input":{"transcription":{"model":"whisper-1"}}}}`)})
	sess := s.Greeting()[0]["session"].(map[string]any)
	out := sess["audio"].(map[string]any)["output"].(map[string]any)
	if out["voice"] != "marin" {
		t.Errorf("nested voice not round-tripped: %v", out["voice"])
	}
	if out["speed"] != 1.5 {
		t.Errorf("nested speed not round-tripped: %v", out["speed"])
	}
	// GA-nested transcription must also enable the transcription event path.
	if !s.transcriptionEnabled() {
		t.Error("GA audio.input.transcription should enable transcription")
	}
}

func TestSession_ExpiresAt(t *testing.T) {
	s := NewSession("s", "gpt-realtime", fakeGen("ok"))
	if _, ok := s.Greeting()[0]["session"].(map[string]any)["expires_at"]; ok {
		t.Error("expires_at must be omitted when unset (deterministic default)")
	}
	s.SetExpiry(1750000000)
	if got := s.Greeting()[0]["session"].(map[string]any)["expires_at"]; got != int64(1750000000) {
		t.Errorf("expires_at = %v, want 1750000000", got)
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

func TestSession_ResponseEnvelopeAndUsageDetails(t *testing.T) {
	s := NewSession("s", "gpt-realtime",
		fakeGenTool("Checking.", types.ToolCallSpec{Name: "t", Arguments: map[string]any{"a": 1}}))
	ev := s.Handle(context.Background(), &ClientEvent{Type: "response.create"})

	// The response envelope (created + done) carries the GA fields.
	for _, typ := range []string{"response.created", "response.done"} {
		r := firstEvent(ev, typ)["response"].(map[string]any)
		for _, k := range []string{"output_modalities", "conversation_id", "status_details"} {
			if _, ok := r[k]; !ok {
				t.Errorf("%s response missing %q", typ, k)
			}
		}
	}
	// response.done usage carries the GA per-modality token details.
	usage := firstEvent(ev, "response.done")["response"].(map[string]any)["usage"].(map[string]any)
	if _, ok := usage["input_token_details"]; !ok {
		t.Error("usage missing input_token_details")
	}
	if _, ok := usage["output_token_details"]; !ok {
		t.Error("usage missing output_token_details")
	}
	// Function-call argument events must NOT carry content_index — a
	// function_call item has no content parts and the GA event types omit it.
	// (Round-3 eval reversed the round-2 assumption here, verified against the
	// GA SDK ResponseFunctionCallArgumentsDelta/DoneEvent types.)
	for _, typ := range []string{"response.function_call_arguments.delta", "response.function_call_arguments.done"} {
		if _, ok := firstEvent(ev, typ)["content_index"]; ok {
			t.Errorf("%s must not carry content_index", typ)
		}
	}
}
