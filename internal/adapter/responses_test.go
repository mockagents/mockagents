package adapter

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// doResponses fires a raw-JSON body at the Responses handler. The body is raw
// JSON (not a marshaled struct) because `input` is polymorphic — a bare string
// in some tests, a typed item array in others.
func doResponses(t *testing.T, h *ResponsesHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.HandleResponses(rec, req)
	return rec
}

// responsesAgent reuses the shared OpenAI test agent plus extra scenarios that
// exercise the refusal and truncation (finish_reason=length) output paths.
func responsesAgent() *types.AgentDefinition {
	a := testOpenAIAgent()
	a.Spec.Behavior.Scenarios = append(a.Spec.Behavior.Scenarios,
		types.Scenario{
			Name:     "refuse",
			Match:    &types.MatchRule{ContentContains: "bomb"},
			Response: types.ScenarioResponse{Refusal: "I can't help with that."},
		},
		types.Scenario{
			Name:  "truncated",
			Match: &types.MatchRule{ContentContains: "longstory"},
			Response: types.ScenarioResponse{
				Content:      "This was cut off",
				FinishReason: "length",
			},
		},
	)
	return a
}

func TestResponses_BasicText(t *testing.T) {
	h := NewResponsesHandler(testEngine(responsesAgent()), nil)
	rec := doResponses(t, h, `{"model":"gpt-4o","input":"hello"}`)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.Equal(t, "response", resp["object"])
	assert.Equal(t, "completed", resp["status"])
	assert.Equal(t, "gpt-4o", resp["model"])
	assert.Contains(t, resp["id"], "resp_")
	assert.Nil(t, resp["error"])

	output := resp["output"].([]any)
	require.Len(t, output, 1)
	item := output[0].(map[string]any)
	assert.Equal(t, "message", item["type"])
	assert.Equal(t, "assistant", item["role"])
	assert.Contains(t, item["id"], "msg_")

	content := item["content"].([]any)
	require.Len(t, content, 1)
	part := content[0].(map[string]any)
	assert.Equal(t, "output_text", part["type"])
	assert.Equal(t, "Hi there!", part["text"])

	usage := resp["usage"].(map[string]any)
	assert.Greater(t, usage["input_tokens"].(float64), 0.0)
	assert.Greater(t, usage["output_tokens"].(float64), 0.0)
	assert.Equal(t,
		usage["input_tokens"].(float64)+usage["output_tokens"].(float64),
		usage["total_tokens"].(float64),
	)
}

func TestResponses_ArrayInputWithInstructions(t *testing.T) {
	h := NewResponsesHandler(testEngine(responsesAgent()), nil)
	body := `{"model":"gpt-4o","instructions":"be terse",
		"input":[{"role":"user","content":[{"type":"input_text","text":"hello"}]}]}`
	rec := doResponses(t, h, body)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	// instructions echoed back verbatim.
	assert.Equal(t, "be terse", resp["instructions"])
	item := resp["output"].([]any)[0].(map[string]any)
	part := item["content"].([]any)[0].(map[string]any)
	assert.Equal(t, "Hi there!", part["text"])
}

func TestResponses_ToolCall(t *testing.T) {
	h := NewResponsesHandler(testEngine(responsesAgent()), nil)
	rec := doResponses(t, h, `{"model":"gpt-4o","input":"check weather"}`)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	// Find the function_call output item.
	var fc map[string]any
	for _, o := range resp["output"].([]any) {
		if m := o.(map[string]any); m["type"] == "function_call" {
			fc = m
		}
	}
	require.NotNil(t, fc, "expected a function_call output item")
	assert.Equal(t, "get_weather", fc["name"])
	assert.Equal(t, "completed", fc["status"])
	assert.Contains(t, fc["call_id"], "call_")
	assert.Contains(t, fc["arguments"], "NYC")
}

func TestResponses_PreviousResponseIDContinuity(t *testing.T) {
	h := NewResponsesHandler(testEngine(responsesAgent()), nil)

	// Turn 1.
	rec1 := doResponses(t, h, `{"model":"gpt-4o","input":"hello"}`)
	require.Equal(t, http.StatusOK, rec1.Code)
	var resp1 map[string]any
	require.NoError(t, json.Unmarshal(rec1.Body.Bytes(), &resp1))
	id1 := resp1["id"].(string)

	// Turn 2 feeds only a function_call_output back and references turn 1; the
	// prior user message lives in the stored conversation, so the engine still
	// resolves a scenario (no empty-message error).
	body2 := `{"model":"gpt-4o","previous_response_id":"` + id1 + `",
		"input":[{"type":"function_call_output","call_id":"call_x","output":"sunny"}]}`
	rec2 := doResponses(t, h, body2)
	require.Equal(t, http.StatusOK, rec2.Code)
	var resp2 map[string]any
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))

	assert.Equal(t, id1, resp2["previous_response_id"])
	assert.NotEqual(t, id1, resp2["id"], "each turn gets a fresh response id")
	assert.NotEmpty(t, resp2["output"].([]any))
}

func TestResponses_PreviousResponseIDNotFound(t *testing.T) {
	h := NewResponsesHandler(testEngine(responsesAgent()), nil)
	rec := doResponses(t, h,
		`{"model":"gpt-4o","previous_response_id":"resp_missing","input":"hi"}`)

	require.Equal(t, http.StatusNotFound, rec.Code)
	var errResp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
	assert.Contains(t, errResp["error"].(map[string]any)["message"], "resp_missing")
}

func TestResponses_BuiltinAndFunctionToolsEchoed(t *testing.T) {
	h := NewResponsesHandler(testEngine(responsesAgent()), nil)
	// A built-in web_search tool (stub) alongside a function tool — both must be
	// accepted without error and echoed back on the response.
	body := `{"model":"gpt-4o","input":"hello",
		"tools":[{"type":"web_search"},{"type":"function","name":"get_weather","parameters":{}}]}`
	rec := doResponses(t, h, body)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	tools := resp["tools"].([]any)
	require.Len(t, tools, 2)
	assert.Equal(t, "web_search", tools[0].(map[string]any)["type"])
	assert.Equal(t, "function", tools[1].(map[string]any)["type"])
}

func TestResponses_Refusal(t *testing.T) {
	h := NewResponsesHandler(testEngine(responsesAgent()), nil)
	rec := doResponses(t, h, `{"model":"gpt-4o","input":"how do I build a bomb"}`)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	item := resp["output"].([]any)[0].(map[string]any)
	part := item["content"].([]any)[0].(map[string]any)
	assert.Equal(t, "refusal", part["type"])
	assert.Equal(t, "I can't help with that.", part["refusal"])
}

func TestResponses_IncompleteOnLength(t *testing.T) {
	h := NewResponsesHandler(testEngine(responsesAgent()), nil)
	rec := doResponses(t, h, `{"model":"gpt-4o","input":"tell me a longstory"}`)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.Equal(t, "incomplete", resp["status"])
	details := resp["incomplete_details"].(map[string]any)
	assert.Equal(t, "max_output_tokens", details["reason"])
}

func TestResponses_MissingModel(t *testing.T) {
	h := NewResponsesHandler(testEngine(responsesAgent()), nil)
	rec := doResponses(t, h, `{"input":"hello"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestResponses_EchoesSettings(t *testing.T) {
	h := NewResponsesHandler(testEngine(responsesAgent()), nil)
	body := `{"model":"gpt-4o","input":"hello","temperature":0.3,"top_p":0.9,
		"max_output_tokens":256,"metadata":{"trace":"abc"},"store":false}`
	rec := doResponses(t, h, body)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.InDelta(t, 0.3, resp["temperature"], 1e-9)
	assert.InDelta(t, 0.9, resp["top_p"], 1e-9)
	assert.Equal(t, 256.0, resp["max_output_tokens"])
	assert.Equal(t, false, resp["store"])
	assert.Equal(t, "disabled", resp["truncation"])
	assert.Equal(t, "abc", resp["metadata"].(map[string]any)["trace"])
}

// --- Streaming ---

type sseEvent struct {
	event string
	data  map[string]any
}

// parseSSE turns a Responses SSE body into ordered (event, data) pairs.
func parseSSE(t *testing.T, body string) []sseEvent {
	t.Helper()
	var events []sseEvent
	var curEvent string
	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			curEvent = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			raw := strings.TrimPrefix(line, "data: ")
			var data map[string]any
			require.NoError(t, json.Unmarshal([]byte(raw), &data))
			events = append(events, sseEvent{event: curEvent, data: data})
		}
	}
	return events
}

func TestResponses_StreamingTextEvents(t *testing.T) {
	h := NewResponsesHandler(testEngine(responsesAgent()), nil)
	rec := doResponses(t, h, `{"model":"gpt-4o","input":"hello","stream":true}`)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/event-stream; charset=utf-8", rec.Header().Get("Content-Type"))

	events := parseSSE(t, rec.Body.String())
	require.NotEmpty(t, events)

	// The named-event ladder must appear in order.
	want := []string{
		"response.created",
		"response.in_progress",
		"response.output_item.added",
		"response.content_part.added",
		"response.output_text.delta",
		"response.output_text.done",
		"response.content_part.done",
		"response.output_item.done",
		"response.completed",
	}
	got := make([]string, 0, len(events))
	for _, e := range events {
		got = append(got, e.event)
	}
	for _, w := range want {
		assert.Contains(t, got, w, "missing event %s", w)
	}
	// First and last frames are created / completed.
	assert.Equal(t, "response.created", events[0].event)
	assert.Equal(t, "response.completed", events[len(events)-1].event)

	// sequence_number is monotonic from 0.
	for i, e := range events {
		assert.Equal(t, float64(i), e.data["sequence_number"], "seq at index %d", i)
	}

	// Reassembling the text deltas reproduces the full content.
	var sb strings.Builder
	for _, e := range events {
		if e.event == "response.output_text.delta" {
			sb.WriteString(e.data["delta"].(string))
		}
	}
	assert.Equal(t, "Hi there!", sb.String())

	// The terminal response carries the completed status + full output.
	final := events[len(events)-1].data["response"].(map[string]any)
	assert.Equal(t, "completed", final["status"])
	assert.NotEmpty(t, final["output"].([]any))
}

func TestResponses_StreamingToolCallEvents(t *testing.T) {
	h := NewResponsesHandler(testEngine(responsesAgent()), nil)
	rec := doResponses(t, h, `{"model":"gpt-4o","input":"check weather","stream":true}`)

	require.Equal(t, http.StatusOK, rec.Code)
	events := parseSSE(t, rec.Body.String())

	var sawAdded, sawArgsDone bool
	var args strings.Builder
	for _, e := range events {
		switch e.event {
		case "response.output_item.added":
			if item, ok := e.data["item"].(map[string]any); ok && item["type"] == "function_call" {
				sawAdded = true
				assert.Equal(t, "get_weather", item["name"])
			}
		case "response.function_call_arguments.delta":
			args.WriteString(e.data["delta"].(string))
		case "response.function_call_arguments.done":
			sawArgsDone = true
			assert.Contains(t, e.data["arguments"], "NYC")
		}
	}
	assert.True(t, sawAdded, "expected a function_call output_item.added")
	assert.True(t, sawArgsDone, "expected function_call_arguments.done")
	assert.Contains(t, args.String(), "NYC")
}

func TestResponses_StoreEvictionBounded(t *testing.T) {
	// The store is a bounded FIFO: after pushing past the cap, the oldest id
	// is evicted (returns not-found) while the newest is retained.
	s := newResponseStore()
	first := "resp_first"
	s.put("tenant-a", first, storedResponse{msgs: []engine.RequestMessage{{Role: "user", Content: "x"}}, turns: 1})
	for i := 0; i < maxStoredResponses; i++ {
		s.put("tenant-b", "resp_fill_"+strings.Repeat("a", i%5)+"_"+itoa(i), storedResponse{})
	}
	_, ok := s.get("tenant-a", first)
	assert.False(t, ok, "oldest entry should have been evicted")
}

func TestResponses_TenantRetention(t *testing.T) {
	for _, owner := range []string{"tenant-a", ""} {
		for _, setting := range []string{"", `,"store":true`, `,"store":false`} {
			for _, stream := range []bool{false, true} {
				t.Run(owner+setting+"/stream="+fmt.Sprint(stream), func(t *testing.T) {
					h := NewResponsesHandler(testEngine(responsesAgent()), nil)
					call := func(tenant, body string) *httptest.ResponseRecorder {
						r := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
						r = r.WithContext(engine.WithTenantID(r.Context(), tenant))
						w := httptest.NewRecorder()
						h.HandleResponses(w, r)
						return w
					}
					first := call(owner, `{"model":"gpt-4o","input":"hello","stream":`+fmt.Sprint(stream)+setting+`}`)
					require.Equal(t, 200, first.Code, first.Body.String())
					body := first.Body.Bytes()
					if stream {
						for _, line := range strings.Split(first.Body.String(), "\n") {
							if strings.HasPrefix(line, "data: ") && strings.Contains(line, `"type":"response.completed"`) {
								var event struct {
									Response json.RawMessage `json:"response"`
								}
								require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event))
								body = event.Response
							}
						}
					}
					var response ResponsesResponse
					require.NoError(t, json.Unmarshal(body, &response))
					require.NotEmpty(t, response.ID)
					for _, caller := range []string{"tenant-a", "tenant-b", ""} {
						want := 404
						if caller == owner && setting != `,"store":false` {
							want = 200
						}
						got := call(caller, `{"model":"gpt-4o","input":"next","previous_response_id":"`+response.ID+`"}`)
						assert.Equal(t, want, got.Code, "caller=%q: %s", caller, got.Body.String())
					}
				})
			}
		}
	}
}

func TestResponseStore_ConcurrentTenantKeys(t *testing.T) {
	s := newResponseStore()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tenant := fmt.Sprint(i)
			for j := 0; j < 100; j++ {
				id := fmt.Sprint(j)
				s.put(tenant, id, storedResponse{msgs: []engine.RequestMessage{{Content: tenant}}})
				if got, ok := s.get(tenant, id); ok && got.msgs[0].Content != tenant {
					t.Errorf("wrong tenant history")
				}
			}
		}(i)
	}
	wg.Wait()
	assert.Len(t, s.m, maxStoredResponses)
	assert.Len(t, s.order, maxStoredResponses)
}

// itoa avoids importing strconv just for the eviction test.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// turnAgent answers by turn number, so a chain that does not advance turns is
// visible in the content (review E-01).
func turnAgent() *types.AgentDefinition {
	a := testOpenAIAgent()
	one, two, three := 1, 2, 3
	a.Spec.Behavior.Scenarios = []types.Scenario{
		{Name: "t1", Match: &types.MatchRule{TurnNumber: &one}, Response: types.ScenarioResponse{Content: "FIRST"}},
		{Name: "t2", Match: &types.MatchRule{TurnNumber: &two}, Response: types.ScenarioResponse{Content: "SECOND"}},
		{Name: "t3", Match: &types.MatchRule{TurnNumber: &three}, Response: types.ScenarioResponse{Content: "THIRD"}},
	}
	return a
}

func responsesText(t *testing.T, rec *httptest.ResponseRecorder) (id, text string) {
	t.Helper()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out struct {
		ID     string `json:"id"`
		Output []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.NotEmpty(t, out.Output)
	require.NotEmpty(t, out.Output[0].Content)
	return out.ID, out.Output[0].Content[0].Text
}

// A previous_response_id chain advances turn_number on every hop, and no
// engine session is stored for header-less calls (review E-01).
func TestResponses_ChainAdvancesTurnsWithoutStoringSessions(t *testing.T) {
	eng := testEngine(turnAgent())
	h := NewResponsesHandler(eng, nil)

	id, text := responsesText(t, doResponses(t, h, `{"model":"gpt-4o","input":"one"}`))
	assert.Equal(t, "FIRST", text)
	id, text = responsesText(t, doResponses(t, h, `{"model":"gpt-4o","input":"two","previous_response_id":"`+id+`"}`))
	assert.Equal(t, "SECOND", text)
	_, text = responsesText(t, doResponses(t, h, `{"model":"gpt-4o","input":"three","previous_response_id":"`+id+`"}`))
	assert.Equal(t, "THIRD", text)

	for i := 0; i < 20; i++ {
		doResponses(t, h, `{"model":"gpt-4o","input":"one-shot"}`)
	}
	assert.Zero(t, eng.States.Count(), "header-less Responses calls must not pin engine sessions")
}

// The same holds for a conversation referenced across turns.
func TestResponses_ConversationAdvancesTurns(t *testing.T) {
	conv := newConversationStore()
	eng := testEngine(turnAgent())
	h := NewResponsesHandler(eng, conv)
	ch := NewConversationsHandler(conv)

	req := httptest.NewRequest(http.MethodPost, "/v1/conversations", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	ch.HandleCreate(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	for _, want := range []string{"FIRST", "SECOND", "THIRD"} {
		_, text := responsesText(t, doResponses(t, h, `{"model":"gpt-4o","input":"next","conversation":"`+created.ID+`"}`))
		assert.Equal(t, want, text)
	}
	assert.Zero(t, eng.States.Count())
}

// An unmodelled input item (a replayed reasoning item) is skipped rather than
// becoming an empty user turn that shadows the real one (review E-10).
func TestResponses_UnknownInputItemsAreSkipped(t *testing.T) {
	h := NewResponsesHandler(testEngine(responsesAgent()), nil)
	rec := doResponses(t, h, `{"model":"gpt-4o","input":[
		{"role":"user","content":"hello"},
		{"type":"reasoning","id":"rs_1","summary":[]},
		{"type":"item_reference","id":"msg_1"}]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// /v1/models lists each model once, skips agents without a model, and keeps
// a stable created timestamp (review E-20).
func TestModels_DeduplicatedAndStable(t *testing.T) {
	a, b, c := testOpenAIAgent(), testOpenAIAgent(), testOpenAIAgent()
	a.Metadata.Name, b.Metadata.Name, c.Metadata.Name = "a", "b", "c"
	a.Spec.Model, b.Spec.Model, c.Spec.Model = "gpt-4o", "gpt-4o", ""
	h := &OpenAIHandler{Engine: testEngine(a, b, c)}
	list := func() []map[string]any {
		rec := httptest.NewRecorder()
		h.HandleModels(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
		var out struct {
			Data []map[string]any `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
		return out.Data
	}
	first := list()
	require.Len(t, first, 1)
	assert.Equal(t, "gpt-4o", first[0]["id"])
	assert.Equal(t, first[0]["created"], list()[0]["created"])
}

// The warn-mode strict header is bounded (review E-22).
func TestStrictViolationHeader_IsBounded(t *testing.T) {
	rec := httptest.NewRecorder()
	warnings := make([]string, 0, 2000)
	for i := 0; i < 2000; i++ {
		warnings = append(warnings, "tool result references unknown id call_"+itoa(i))
	}
	setStrictViolationHeader(rec, &engine.Response{StrictWarnings: warnings})
	v := rec.Header().Get(HeaderStrictViolation)
	assert.LessOrEqual(t, len(v), maxStrictViolationHeader+64)
	assert.Contains(t, v, "truncated")
}

// Every conversation route answers an oversized body with 413 (review E-21).
func TestConversations_OversizedBodyIs413(t *testing.T) {
	store := newConversationStore()
	ch := NewConversationsHandler(store)
	rec := httptest.NewRecorder()
	ch.HandleCreate(rec, httptest.NewRequest(http.MethodPost, "/v1/conversations", strings.NewReader(`{}`)))
	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	big := `{"metadata":{"k":"` + strings.Repeat("x", maxDecodeBodyBytes+1) + `"}}`
	for name, call := range map[string]http.HandlerFunc{
		"update":       ch.HandleUpdate,
		"create-items": ch.HandleCreateItems,
	} {
		req := httptest.NewRequest(http.MethodPost, "/v1/conversations/"+created.ID, strings.NewReader(big))
		req.SetPathValue("id", created.ID)
		rec := httptest.NewRecorder()
		call(rec, req)
		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, name)
	}
}

// instructions apply per request: a chained call replays the earlier turns
// but not the earlier instructions, and the new ones take effect (E-11).
func TestResponses_InstructionsAreNotCarriedOver(t *testing.T) {
	h := NewResponsesHandler(testEngine(responsesAgent()), nil)
	id, _ := responsesText(t, doResponses(t, h, `{"model":"gpt-4o","input":"hello","instructions":"OLD RULES"}`))
	id2, _ := responsesText(t, doResponses(t, h, `{"model":"gpt-4o","input":"hello again","instructions":"NEW RULES","previous_response_id":"`+id+`"}`))

	stored, ok := h.store.get("", id2)
	require.True(t, ok)
	var systems []string
	for _, m := range stored.msgs {
		if m.Role == "system" {
			systems = append(systems, m.Content)
		}
	}
	assert.Equal(t, []string{"NEW RULES"}, systems)
	assert.Equal(t, 2, stored.turns)
}
