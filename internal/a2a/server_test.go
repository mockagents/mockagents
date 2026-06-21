package a2a

import (
	"encoding/json"
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
	assert.False(t, c.Capabilities.Streaming, "streaming must not be advertised (not served yet)")
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

func TestUnknownAndStreamMethods(t *testing.T) {
	s := NewServer(testDef())
	for _, m := range []string{"bogus/method", "message/stream"} {
		r := call(t, s, m, map[string]any{})
		require.NotNil(t, r.Error, m)
		assert.Equal(t, errMethodNotFound, r.Error.Code, m)
	}
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
