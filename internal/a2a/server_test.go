package a2a

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testDef() *types.A2AServerDefinition {
	return &types.A2AServerDefinition{
		APIVersion: types.AgentAPIVersion, Kind: types.A2AServerKind,
		Metadata: types.Metadata{Name: "weather-a2a"},
		Spec: types.A2AServerSpec{
			Card: types.A2AAgentCard{
				Name: "Weather Agent", Description: "Talks about the weather", Version: "1.0.0",
				Skills: []types.A2ASkill{{ID: "forecast", Name: "Forecast"}},
			},
			Responses: []types.A2AMessageResponse{
				{Match: "weather", Text: "It is sunny."},
				{Match: "slow", Text: "working on it", State: "working"},
				{Default: true, Text: "I can only talk about weather."},
			},
		},
	}
}

// rpcResult is the decoded JSON-RPC envelope for assertions.
type rpcResult struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func call(t *testing.T, s *Server, method string, params any) rpcResult {
	t.Helper()
	p, _ := json.Marshal(params)
	req, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": json.RawMessage(p)})
	out, err := s.HandleBytes(req)
	require.NoError(t, err)
	var r rpcResult
	require.NoError(t, json.Unmarshal(out, &r))
	return r
}

func sendMessage(t *testing.T, s *Server, text string) Task {
	t.Helper()
	r := call(t, s, "message/send", map[string]any{
		"message": map[string]any{"role": "user", "messageId": "m1",
			"parts": []any{map[string]any{"kind": "text", "text": text}}},
	})
	require.Nil(t, r.Error, "message/send error")
	var task Task
	require.NoError(t, json.Unmarshal(r.Result, &task))
	return task
}

func TestCard(t *testing.T) {
	s := NewServer(testDef())
	c := s.Card("http://example.test")
	assert.Equal(t, "Weather Agent", c.Name)
	assert.Equal(t, "http://example.test/", c.URL)
	assert.Equal(t, types.DefaultA2AProtocolVersion, c.ProtocolVersion)
	assert.Equal(t, types.DefaultA2ATransport, c.PreferredTransport, "preferredTransport is required by A2A v0.3")
	assert.True(t, c.Capabilities.Streaming, "streaming is served over SSE and must be advertised")
	assert.NotEmpty(t, c.DefaultInputModes)

	// Every skill's tags must render as an array (never null) — the testDef
	// skill leaves tags unset, so the server must normalize it to [].
	require.Len(t, c.Skills, 1)
	assert.NotNil(t, c.Skills[0].Tags, "skill tags must be non-nil so the card renders a JSON array")

	// The normalization must not mutate the stored definition.
	assert.Nil(t, s.def.Spec.Card.Skills[0].Tags, "Card() must not mutate the stored def's skills")

	// The card serializes with the required fields present.
	raw, err := json.Marshal(c)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"preferredTransport":"JSONRPC"`)
	assert.Contains(t, string(raw), `"tags":[]`)
}

func TestMessageSend_MatchAndDefault(t *testing.T) {
	s := NewServer(testDef())

	task := sendMessage(t, s, "what is the weather today")
	assert.Regexp(t, `^task-`, task.ID)
	assert.Equal(t, "completed", task.Status.State)
	require.Len(t, task.Artifacts, 1)
	require.Len(t, task.Artifacts[0].Parts, 1)
	assert.Equal(t, "It is sunny.", task.Artifacts[0].Parts[0].Text)
	require.Len(t, task.History, 2)
	assert.Equal(t, "user", task.History[0].Role)
	assert.Equal(t, "agent", task.History[1].Role)

	// Falls back to default.
	def := sendMessage(t, s, "tell me a joke")
	assert.Equal(t, "I can only talk about weather.", def.Artifacts[0].Parts[0].Text)
}

func TestTasks_GetCancelLifecycle(t *testing.T) {
	s := NewServer(testDef())

	// A non-terminal ("working") task is cancelable.
	working := sendMessage(t, s, "this is slow")
	require.Equal(t, "working", working.Status.State)

	got := call(t, s, "tasks/get", map[string]any{"id": working.ID})
	require.Nil(t, got.Error)
	var gotTask Task
	require.NoError(t, json.Unmarshal(got.Result, &gotTask))
	assert.Equal(t, working.ID, gotTask.ID)

	cancelled := call(t, s, "tasks/cancel", map[string]any{"id": working.ID})
	require.Nil(t, cancelled.Error)
	var cTask Task
	require.NoError(t, json.Unmarshal(cancelled.Result, &cTask))
	assert.Equal(t, "canceled", cTask.Status.State)

	// A completed task cannot be canceled.
	done := sendMessage(t, s, "weather please")
	require.Equal(t, "completed", done.Status.State)
	notCancelable := call(t, s, "tasks/cancel", map[string]any{"id": done.ID})
	require.NotNil(t, notCancelable.Error)
	assert.Equal(t, errNotCancelable, notCancelable.Error.Code)

	// Unknown task id → TaskNotFound.
	missing := call(t, s, "tasks/get", map[string]any{"id": "task-999"})
	require.NotNil(t, missing.Error)
	assert.Equal(t, errTaskNotFound, missing.Error.Code)
}

func TestUnknownMethod(t *testing.T) {
	s := NewServer(testDef())
	r := call(t, s, "bogus/method", map[string]any{})
	require.NotNil(t, r.Error)
	assert.Equal(t, errMethodNotFound, r.Error.Code)
}

func TestHTTPHandlers(t *testing.T) {
	s := NewServer(testDef())
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/agent-card.json", s.CardHandler())
	mux.HandleFunc("POST /", s.RPCHandler())
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Card discovery, with the URL derived from the request host.
	resp, err := http.Get(srv.URL + "/.well-known/agent-card.json")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var card types.A2AAgentCard
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))
	assert.Equal(t, "Weather Agent", card.Name)
	assert.Equal(t, srv.URL+"/", card.URL)

	// JSON-RPC message/send over HTTP.
	body := `{"jsonrpc":"2.0","id":1,"method":"message/send","params":{"message":{"role":"user","messageId":"m1","parts":[{"kind":"text","text":"weather?"}]}}}`
	rpcResp, err := http.Post(srv.URL+"/", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer rpcResp.Body.Close()
	require.Equal(t, http.StatusOK, rpcResp.StatusCode)
	var env rpcResult
	require.NoError(t, json.NewDecoder(rpcResp.Body).Decode(&env))
	require.Nil(t, env.Error)
	var task Task
	require.NoError(t, json.Unmarshal(env.Result, &task))
	assert.Equal(t, "It is sunny.", task.Artifacts[0].Parts[0].Text)
}

func streamReq(text string) *rpcRequest {
	p, _ := json.Marshal(map[string]any{"message": map[string]any{
		"role": "user", "parts": []any{map[string]any{"kind": "text", "text": text}}}})
	return &rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "message/stream", Params: p}
}

func TestMessageStream_EventSequence(t *testing.T) {
	s := NewServer(testDef())
	events, rerr := s.StreamResults(streamReq("what is the weather"))
	require.Nil(t, rerr)
	require.Len(t, events, 4)

	task, ok := events[0].(Task)
	require.True(t, ok, "first event is the initial Task")
	assert.Equal(t, "working", task.Status.State)

	su0 := events[1].(statusUpdateEvent)
	assert.Equal(t, "status-update", su0.Kind)
	assert.False(t, su0.Final)

	au := events[2].(artifactUpdateEvent)
	assert.Equal(t, "artifact-update", au.Kind)
	assert.True(t, au.LastChunk)
	assert.Equal(t, "It is sunny.", au.Artifact.Parts[0].Text)

	su1 := events[3].(statusUpdateEvent)
	assert.True(t, su1.Final, "last event must be final")
	assert.Equal(t, "completed", su1.Status.State)

	// The terminal task is retrievable via tasks/get.
	got := call(t, s, "tasks/get", map[string]any{"id": task.ID})
	require.Nil(t, got.Error)
}

func TestMessageStream_OverHTTP_SSE(t *testing.T) {
	s := NewServer(testDef())
	mux := http.NewServeMux()
	mux.HandleFunc("POST /", s.RPCHandler())
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := `{"jsonrpc":"2.0","id":1,"method":"message/stream","params":{"message":{"role":"user","parts":[{"kind":"text","text":"weather?"}]}}}`
	resp, err := http.Post(srv.URL+"/", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	rawBytes, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	raw := string(rawBytes)
	// Every event is a `data: {...}\n\n` frame; parse them.
	var frames []map[string]any
	for _, line := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var env map[string]any
		require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &env))
		frames = append(frames, env["result"].(map[string]any))
	}
	require.Len(t, frames, 4)
	// The last frame is a final status-update.
	last := frames[3]
	assert.Equal(t, "status-update", last["kind"])
	assert.Equal(t, true, last["final"])
}

// JSON-RPC 2.0 §5.1 requires the response id to be null (not omitted) when it
// can't be determined — a parse error or an invalid-request error.
func TestErrorResponse_IDIsNull(t *testing.T) {
	s := NewServer(testDef())

	// Parse error: id can't be recovered from a malformed body.
	out, err := s.HandleBytes([]byte(`{not json`))
	require.NoError(t, err)
	assert.Contains(t, string(out), `"id":null`, "parse-error response must carry id:null")

	// Invalid request (wrong jsonrpc version) with no id likewise renders null.
	out, err = s.HandleBytes([]byte(`{"jsonrpc":"1.0","method":"message/send"}`))
	require.NoError(t, err)
	assert.Contains(t, string(out), `"id":null`, "invalid-request response must carry id:null")
}

// A message/stream sent as a notification (no id) is not a valid streaming
// request — the server answers 204 rather than opening an id-less SSE stream.
func TestMessageStream_NotificationNoID_204(t *testing.T) {
	s := NewServer(testDef())
	mux := http.NewServeMux()
	mux.HandleFunc("POST /", s.RPCHandler())
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := `{"jsonrpc":"2.0","method":"message/stream","params":{"message":{"role":"user","parts":[{"kind":"text","text":"weather?"}]}}}`
	resp, err := http.Post(srv.URL+"/", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

// A Data response streamed over message/stream carries the data part in the
// artifact-update event, alongside the text part.
func TestMessageStream_DataPart(t *testing.T) {
	def := testDef()
	def.Spec.Responses = []types.A2AMessageResponse{{Default: true, Text: "here", Data: map[string]any{"temp": 22}}}
	s := NewServer(def)

	events, rerr := s.StreamResults(streamReq("anything"))
	require.Nil(t, rerr)
	require.Len(t, events, 4)

	au, ok := events[2].(artifactUpdateEvent)
	require.True(t, ok, "third event is the artifact-update")
	parts := au.Artifact.Parts
	require.Len(t, parts, 2, "streamed artifact carries a text part and a data part")
	assert.Equal(t, "text", parts[0].Kind)
	assert.Equal(t, "data", parts[1].Kind)
	assert.NotNil(t, parts[1].Data)
}

// A unary (non-SSE) caller of HandleBytes with method message/stream still gets a
// sensible single result: the completed Task.
func TestMessageStream_UnaryFallback(t *testing.T) {
	s := NewServer(testDef())
	req, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "message/stream",
		"params": map[string]any{"message": map[string]any{
			"role": "user", "parts": []any{map[string]any{"kind": "text", "text": "what is the weather"}}}}})
	out, err := s.HandleBytes(req)
	require.NoError(t, err)

	var r rpcResult
	require.NoError(t, json.Unmarshal(out, &r))
	require.Nil(t, r.Error)
	var task Task
	require.NoError(t, json.Unmarshal(r.Result, &task))
	assert.Equal(t, "task", task.Kind, "unary message/stream falls back to a single Task result")
	assert.Equal(t, "completed", task.Status.State)
	assert.Equal(t, "It is sunny.", task.Artifacts[0].Parts[0].Text)
}

// StreamResults surfaces a JSON-RPC error (not a stream) for a bad envelope or
// malformed params.
func TestStreamResults_ErrorPaths(t *testing.T) {
	s := NewServer(testDef())

	// Wrong jsonrpc version → invalid request, no events.
	bad := &rpcRequest{JSONRPC: "1.0", ID: json.RawMessage(`1`), Method: "message/stream"}
	events, rerr := s.StreamResults(bad)
	require.Nil(t, events)
	require.NotNil(t, rerr)
	assert.Equal(t, errInvalidRequest, rerr.Error.Code)

	// Malformed params → invalid params, no events.
	malformed := &rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "message/stream",
		Params: json.RawMessage(`{"message":"not-an-object"}`)}
	events, rerr = s.StreamResults(malformed)
	require.Nil(t, events)
	require.NotNil(t, rerr)
	assert.Equal(t, errInvalidParams, rerr.Error.Code)
}

func TestMessageSend_BareMessage(t *testing.T) {
	def := testDef()
	def.Spec.Responses = []types.A2AMessageResponse{{Default: true, Text: "quick reply", AsMessage: true}}
	s := NewServer(def)

	r := call(t, s, "message/send", map[string]any{"message": map[string]any{
		"role": "user", "parts": []any{map[string]any{"kind": "text", "text": "hi"}}}})
	require.Nil(t, r.Error)

	var m Message
	require.NoError(t, json.Unmarshal(r.Result, &m))
	assert.Equal(t, "message", m.Kind, "result must be a bare Message, not a Task")
	assert.Equal(t, "agent", m.Role)
	require.NotEmpty(t, m.Parts)
	assert.Equal(t, "quick reply", m.Parts[0].Text)
	assert.Empty(t, s.tasks, "a bare-message reply must not create a task")
}

func TestMessageSend_DataPart(t *testing.T) {
	def := testDef()
	def.Spec.Responses = []types.A2AMessageResponse{{Default: true, Text: "here", Data: map[string]any{"temp": 22}}}
	s := NewServer(def)

	task := sendMessage(t, s, "anything")
	parts := task.Artifacts[0].Parts
	require.Len(t, parts, 2, "artifact carries a text part and a data part")
	assert.Equal(t, "text", parts[0].Kind)
	assert.Equal(t, "data", parts[1].Kind)
	assert.NotNil(t, parts[1].Data)
}

func TestMessageSend_AcceptsFileAndDataParts(t *testing.T) {
	s := NewServer(testDef())
	r := call(t, s, "message/send", map[string]any{"message": map[string]any{
		"role": "user", "parts": []any{
			map[string]any{"kind": "file", "file": map[string]any{"name": "a.png", "mimeType": "image/png", "bytes": "AAAA"}},
			map[string]any{"kind": "data", "data": map[string]any{"k": "v"}},
			map[string]any{"kind": "text", "text": "what is the weather"},
		}}})
	require.Nil(t, r.Error)

	var task Task
	require.NoError(t, json.Unmarshal(r.Result, &task))
	// Matching used the text part.
	assert.Equal(t, "It is sunny.", task.Artifacts[0].Parts[0].Text)
	// The non-text parts round-trip in the stored user message.
	require.Len(t, task.History, 2)
	var sawFile, sawData bool
	for _, p := range task.History[0].Parts {
		if p.Kind == "file" && p.File != nil && p.File.Name == "a.png" {
			sawFile = true
		}
		if p.Kind == "data" && p.Data != nil {
			sawData = true
		}
	}
	assert.True(t, sawFile, "file part should round-trip")
	assert.True(t, sawData, "data part should round-trip")
}

func TestCard_DefaultsRequiredFields(t *testing.T) {
	// A minimal card (only a name) must still serve a spec-valid card: version +
	// description defaulted, skills a non-null array.
	def := &types.A2AServerDefinition{
		APIVersion: types.AgentAPIVersion, Kind: types.A2AServerKind,
		Metadata: types.Metadata{Name: "bare"},
		Spec:     types.A2AServerSpec{Card: types.A2AAgentCard{Name: "Bare Agent"}},
	}
	c := NewServer(def).Card("http://example.test")
	assert.NotEmpty(t, c.Version, "version is a required Agent Card field")
	assert.NotEmpty(t, c.Description, "description is a required Agent Card field")
	assert.NotNil(t, c.Skills, "skills must be a non-null array")

	raw, err := json.Marshal(c)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"skills":[]`, "empty skills must render as []")
	assert.Contains(t, string(raw), `"version":`)
	assert.Contains(t, string(raw), `"description":`)
}
