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

// F10: a client-supplied item.id is honored (pre-generated ids let clients
// address their items later); duplicates are rejected with param item.id.
func TestSession_ClientSuppliedItemID(t *testing.T) {
	ctx := context.Background()
	s := NewSession("sci", "", fakeGen("ok"))
	create := &ClientEvent{Type: "conversation.item.create",
		Item: []byte(`{"id":"cli_item_1","type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}`)}

	added := firstEvent(s.Handle(ctx, create), "conversation.item.added")
	if added["item"].(map[string]any)["id"] != "cli_item_1" {
		t.Fatalf("ack item id = %v, want the client-supplied id", added["item"])
	}
	// Addressable by the client's own id.
	got := s.Handle(ctx, &ClientEvent{Type: "conversation.item.retrieve", ItemID: "cli_item_1"})
	if got[0]["type"] != "conversation.item.retrieved" {
		t.Errorf("retrieve by client id = %v", typesOf(got))
	}
	// The chain runs through it.
	next := firstEvent(s.Handle(ctx, &ClientEvent{Type: "conversation.item.create",
		Item: []byte(`{"type":"message","role":"user","content":[{"type":"input_text","text":"again"}]}`)}), "conversation.item.added")
	if next["previous_item_id"] != "cli_item_1" {
		t.Errorf("next item previous_item_id = %v, want cli_item_1", next["previous_item_id"])
	}
	// Duplicate id → invalid_value naming item.id.
	dup := s.Handle(ctx, create)
	if dup[0]["type"] != "error" || dup[0]["error"].(map[string]any)["param"] != "item.id" {
		t.Errorf("duplicate id = %v, want invalid_value on item.id", dup[0])
	}
}

// F10: previous_item_id on conversation.item.create places the item — "root"
// inserts first, a known id inserts after it (the chain tail moves only for
// appends), an unknown id errors.
func TestSession_PreviousItemIDInsert(t *testing.T) {
	ctx := context.Background()
	s := NewSession("spi", "", fakeGen("ok"))
	mk := func(id string) *ClientEvent {
		return &ClientEvent{Type: "conversation.item.create",
			Item: []byte(`{"id":"` + id + `","type":"message","role":"user","content":[{"type":"input_text","text":"x"}]}`)}
	}
	s.Handle(ctx, mk("item_a"))
	s.Handle(ctx, mk("item_b")) // tail = item_b

	// Insert after item_a: ack points at item_a, tail stays item_b.
	ins := mk("item_c")
	ins.PreviousItemID = "item_a"
	added := firstEvent(s.Handle(ctx, ins), "conversation.item.added")
	if added["previous_item_id"] != "item_a" {
		t.Errorf("insert ack previous_item_id = %v, want item_a", added["previous_item_id"])
	}
	next := firstEvent(s.Handle(ctx, mk("item_d")), "conversation.item.added")
	if next["previous_item_id"] != "item_b" {
		t.Errorf("append after insert chains off %v, want the unchanged tail item_b", next["previous_item_id"])
	}

	// "root" inserts at the beginning: ack prev is null, tail unchanged.
	root := mk("item_e")
	root.PreviousItemID = "root"
	added = firstEvent(s.Handle(ctx, root), "conversation.item.added")
	if added["previous_item_id"] != nil {
		t.Errorf("root insert previous_item_id = %v, want null", added["previous_item_id"])
	}

	// Unknown previous_item_id errors.
	bad := mk("item_f")
	bad.PreviousItemID = "item_nope"
	evs := s.Handle(ctx, bad)
	e := evs[0]["error"].(map[string]any)
	if evs[0]["type"] != "error" || e["code"] != "item_not_found" || e["param"] != "previous_item_id" {
		t.Errorf("unknown previous_item_id = %v, want item_not_found on previous_item_id", evs[0])
	}
}

// F15/F16: the response envelope carries the GA fields a strict reader expects
// (usage null on created, audio.output, max_output_tokens), and response.done's
// usage has the full input_token_details breakdown.
func TestResponseEnvelope_GAFields(t *testing.T) {
	s := NewSession("sga", "gpt-realtime", fakeGen("hello"))
	evs := s.Handle(context.Background(), &ClientEvent{Type: "response.create"})

	created := firstEvent(evs, "response.created")["response"].(map[string]any)
	if u, ok := created["usage"]; !ok || u != nil {
		t.Errorf("created usage = %v (present=%v), want present and null", u, ok)
	}
	audioOut := created["audio"].(map[string]any)["output"].(map[string]any)
	if audioOut["voice"] != "alloy" {
		t.Errorf("envelope audio.output.voice = %v, want alloy", audioOut["voice"])
	}
	if _, ok := audioOut["format"]; !ok {
		t.Error("envelope audio.output missing format")
	}
	if raw, _ := json.Marshal(created["max_output_tokens"]); string(raw) != `"inf"` {
		t.Errorf("envelope max_output_tokens = %s, want \"inf\"", raw)
	}

	usage := firstEvent(evs, "response.done")["response"].(map[string]any)["usage"].(map[string]any)
	details := usage["input_token_details"].(map[string]any)
	if _, ok := details["image_tokens"]; !ok {
		t.Error("input_token_details missing image_tokens")
	}
	cached, ok := details["cached_tokens_details"].(map[string]any)
	if !ok {
		t.Fatal("input_token_details missing cached_tokens_details")
	}
	for _, k := range []string{"text_tokens", "audio_tokens", "image_tokens"} {
		if _, ok := cached[k]; !ok {
			t.Errorf("cached_tokens_details missing %q", k)
		}
	}
}

// F17: session.update round-trips the remaining GA fields with correct
// defaults when unset.
func TestSessionUpdate_RoundTripsGAFields(t *testing.T) {
	s := NewSession("srt", "", fakeGen("ok"))

	// Defaults first.
	sess := s.Greeting()[0]["session"].(map[string]any)
	if raw, _ := json.Marshal(sess["truncation"]); string(raw) != `"auto"` {
		t.Errorf("default truncation = %s, want \"auto\"", raw)
	}
	if raw, _ := json.Marshal(sess["parallel_tool_calls"]); string(raw) != "true" {
		t.Errorf("default parallel_tool_calls = %s, want true", raw)
	}
	for _, k := range []string{"tracing", "prompt", "include"} {
		if raw, _ := json.Marshal(sess[k]); string(raw) != "null" {
			t.Errorf("default %s = %s, want null", k, raw)
		}
	}
	if raw, _ := json.Marshal(sess["audio"].(map[string]any)["input"].(map[string]any)["noise_reduction"]); string(raw) != "null" {
		t.Errorf("default noise_reduction = %s, want null", raw)
	}

	// Now set them and read them back off session.updated.
	evs := s.Handle(context.Background(), &ClientEvent{Type: "session.update", Session: []byte(`{
		"tracing":"auto","truncation":"disabled","prompt":{"id":"pmpt_1"},
		"include":["item.input_audio_transcription.logprobs"],"parallel_tool_calls":false,
		"audio":{"input":{"noise_reduction":{"type":"near_field"}}}}`)})
	sess = evs[0]["session"].(map[string]any)
	checks := map[string]string{
		"tracing": `"auto"`, "truncation": `"disabled"`, "prompt": `{"id":"pmpt_1"}`,
		"include": `["item.input_audio_transcription.logprobs"]`, "parallel_tool_calls": "false",
	}
	for k, want := range checks {
		if raw, _ := json.Marshal(sess[k]); string(raw) != want {
			t.Errorf("%s = %s, want %s", k, raw, want)
		}
	}
	nr, _ := json.Marshal(sess["audio"].(map[string]any)["input"].(map[string]any)["noise_reduction"])
	if string(nr) != `{"type":"near_field"}` {
		t.Errorf("noise_reduction = %s", nr)
	}
}

func TestSession_UnknownEvent(t *testing.T) {
	s := NewSession("s4", "", fakeGen("ok"))
	evs := s.Handle(context.Background(), &ClientEvent{Type: "totally.bogus"})
	if len(evs) != 1 || evs[0]["type"] != "error" {
		t.Fatalf("unknown event should yield one error, got %v", typesOf(evs))
	}
}

// response.create inline overrides: per-response output_modalities switch the
// ladder mode, instructions override the session system prompt, and metadata is
// echoed on the response envelope.
func TestSession_ResponseCreateOverrides(t *testing.T) {
	ctx := context.Background()
	var gotHistory []engine.RequestMessage
	gen := func(_ context.Context, _, _ string, history []engine.RequestMessage) (*engine.Response, error) {
		gotHistory = history
		return &engine.Response{Content: "Brief."}, nil
	}
	s := NewSession("so", "gpt-realtime", gen)

	evs := s.Handle(ctx, &ClientEvent{Type: "response.create",
		Response: []byte(`{"output_modalities":["text"],"instructions":"be brief","metadata":{"topic":"weather"}}`)})
	tps := typesOf(evs)
	if !contains(tps, "response.output_text.delta") || contains(tps, "response.output_audio.delta") {
		t.Errorf("per-response text modality not honored; got %v", tps)
	}
	if len(gotHistory) == 0 || gotHistory[0].Role != "system" || gotHistory[0].Content != "be brief" {
		t.Errorf("per-response instructions not prepended: %+v", gotHistory)
	}
	resp := firstEvent(evs, "response.done")["response"].(map[string]any)
	md, _ := json.Marshal(resp["metadata"])
	if string(md) != `{"topic":"weather"}` {
		t.Errorf("metadata = %s, want the echoed object", md)
	}
	if mods := resp["output_modalities"].([]string); len(mods) != 1 || mods[0] != "text" {
		t.Errorf("response output_modalities = %v, want [text]", resp["output_modalities"])
	}
}

// conversation:"none" = out-of-band: conversation_id is null, no conversation-
// item mirror is emitted, and the response leaves no trace in the conversation
// (no history for later turns, no previous_item_id chain update).
func TestSession_OutOfBandResponse(t *testing.T) {
	ctx := context.Background()
	var gotHistory []engine.RequestMessage
	gen := func(_ context.Context, _, _ string, history []engine.RequestMessage) (*engine.Response, error) {
		gotHistory = history
		return &engine.Response{Content: "side classification"}, nil
	}
	s := NewSession("sb", "gpt-realtime", gen)

	itemCreate := &ClientEvent{Type: "conversation.item.create",
		Item: []byte(`{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}`)}
	created := firstEvent(s.Handle(ctx, itemCreate), "conversation.item.added")
	userID := created["item"].(map[string]any)["id"].(string)

	evs := s.Handle(ctx, &ClientEvent{Type: "response.create", Response: []byte(`{"conversation":"none"}`)})
	tps := typesOf(evs)
	if contains(tps, "conversation.item.added") || contains(tps, "conversation.item.done") {
		t.Errorf("out-of-band response must not emit conversation-item events; got %v", tps)
	}
	resp := firstEvent(evs, "response.done")["response"].(map[string]any)
	if resp["conversation_id"] != nil {
		t.Errorf("out-of-band conversation_id = %v, want null", resp["conversation_id"])
	}

	// The next user item still chains off the last CONVERSATION item (the user
	// message), not the out-of-band response item.
	second := firstEvent(s.Handle(ctx, itemCreate), "conversation.item.added")
	if second["previous_item_id"] != userID {
		t.Errorf("post-out-of-band previous_item_id = %v, want %q", second["previous_item_id"], userID)
	}

	// And the out-of-band transcript is not context for later turns.
	s.Handle(ctx, &ClientEvent{Type: "response.create"})
	for _, m := range gotHistory {
		if m.Content == "side classification" {
			t.Errorf("out-of-band response leaked into history: %+v", gotHistory)
		}
	}
}

// Per-response audio.output.voice and an integer max_output_tokens cap are
// honored: the envelope echoes the overrides, the transcript is trimmed, and
// the response + item end status "incomplete" / reason max_output_tokens.
func TestResponseCreate_VoiceAndMaxTokenOverrides(t *testing.T) {
	s := NewSession("smo", "gpt-realtime", fakeGen("one two three four five six"))
	evs := s.Handle(context.Background(), &ClientEvent{Type: "response.create",
		Response: []byte(`{"audio":{"output":{"voice":"cedar"}},"max_output_tokens":3}`)})

	created := firstEvent(evs, "response.created")["response"].(map[string]any)
	if v := created["audio"].(map[string]any)["output"].(map[string]any)["voice"]; v != "cedar" {
		t.Errorf("envelope voice = %v, want the per-response override cedar", v)
	}
	if raw, _ := json.Marshal(created["max_output_tokens"]); string(raw) != "3" {
		t.Errorf("envelope max_output_tokens = %s, want 3", raw)
	}

	done := firstEvent(evs, "response.done")["response"].(map[string]any)
	if done["status"] != "incomplete" {
		t.Fatalf("capped response status = %v, want incomplete", done["status"])
	}
	sd := done["status_details"].(map[string]any)
	if sd["type"] != "incomplete" || sd["reason"] != "max_output_tokens" {
		t.Errorf("status_details = %v, want incomplete/max_output_tokens", sd)
	}
	item := done["output"].([]any)[0].(map[string]any)
	if item["status"] != "incomplete" {
		t.Errorf("capped item status = %v, want incomplete", item["status"])
	}
	transcript := item["content"].([]any)[0].(map[string]any)["transcript"].(string)
	if transcript != "one two three" {
		t.Errorf("trimmed transcript = %q, want the first 3 words", transcript)
	}
	usage := done["usage"].(map[string]any)
	if usage["output_tokens"] != 3 {
		t.Errorf("output_tokens = %v, want the capped 3", usage["output_tokens"])
	}
	// The trimmed content is what entered history.
	found := false
	for _, m := range s.history {
		if m.Role == "assistant" && m.Content == "one two three" {
			found = true
		}
	}
	if !found {
		t.Errorf("history did not get the trimmed transcript: %+v", s.history)
	}
}

// conversation.item.retrieve / delete address stored items; unknown ids get an
// item-specific error, not unknown_event.
func TestSession_ItemRetrieveDelete(t *testing.T) {
	ctx := context.Background()
	s := NewSession("sr", "", fakeGen("ok"))
	created := firstEvent(s.Handle(ctx, &ClientEvent{
		Type: "conversation.item.create",
		Item: []byte(`{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}`),
	}), "conversation.item.added")
	id := created["item"].(map[string]any)["id"].(string)

	got := s.Handle(ctx, &ClientEvent{Type: "conversation.item.retrieve", ItemID: id})
	if got[0]["type"] != "conversation.item.retrieved" {
		t.Fatalf("retrieve events = %v", typesOf(got))
	}
	if got[0]["item"].(map[string]any)["id"] != id {
		t.Errorf("retrieved wrong item: %v", got[0]["item"])
	}

	del := s.Handle(ctx, &ClientEvent{Type: "conversation.item.delete", ItemID: id})
	if del[0]["type"] != "conversation.item.deleted" || del[0]["item_id"] != id {
		t.Errorf("delete events = %v", del)
	}

	// Retrieval after deletion (and any unknown id) errors with item_not_found.
	for _, typ := range []string{"conversation.item.retrieve", "conversation.item.delete"} {
		evs := s.Handle(ctx, &ClientEvent{Type: typ, ItemID: id})
		if evs[0]["type"] != "error" || evs[0]["error"].(map[string]any)["code"] != "item_not_found" {
			t.Errorf("%s on deleted item = %v, want item_not_found error", typ, evs[0])
		}
	}
}

// conversation.item.truncate (the barge-in primitive) acks with the echoed
// cut point and drops the truncated transcript from the stored item.
func TestSession_ItemTruncate(t *testing.T) {
	ctx := context.Background()
	s := NewSession("st", "", fakeGen("A long spoken answer."))
	s.Handle(ctx, &ClientEvent{
		Type: "conversation.item.create",
		Item: []byte(`{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}`),
	})
	ladder := s.Handle(ctx, &ClientEvent{Type: "response.create"})
	msgItem := firstEvent(ladder, "response.output_item.done")["item"].(map[string]any)
	id := msgItem["id"].(string)

	evs := s.Handle(ctx, &ClientEvent{Type: "conversation.item.truncate", ItemID: id, ContentIndex: 0, AudioEndMs: 1500})
	if evs[0]["type"] != "conversation.item.truncated" {
		t.Fatalf("truncate events = %v", typesOf(evs))
	}
	if evs[0]["item_id"] != id || evs[0]["audio_end_ms"] != 1500 || evs[0]["content_index"] != 0 {
		t.Errorf("truncated ack = %v", evs[0])
	}

	// The stored item's audio transcript is dropped.
	got := firstEvent(s.Handle(ctx, &ClientEvent{Type: "conversation.item.retrieve", ItemID: id}), "conversation.item.retrieved")
	part := got["item"].(map[string]any)["content"].([]any)[0].(map[string]any)
	if part["transcript"] != "" {
		t.Errorf("post-truncate transcript = %q, want empty", part["transcript"])
	}

	// Unknown item id errors.
	bad := s.Handle(ctx, &ClientEvent{Type: "conversation.item.truncate", ItemID: "item_nope"})
	if bad[0]["type"] != "error" || bad[0]["error"].(map[string]any)["code"] != "item_not_found" {
		t.Errorf("truncate unknown item = %v, want item_not_found error", bad[0])
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
