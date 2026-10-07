package adapter

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
)

// Fuzz targets for the three primary LLM wire adapters. Each drives the
// real HTTP handler (decode -> engine -> encode) with an arbitrary request
// body against a small in-process engine, so the whole request path is
// exercised exactly as a client would hit it — minus the network.
//
// Shared invariants (checkFuzzResponse):
//   - the handler never panics;
//   - the status code is a valid final HTTP status (200..599);
//   - a non-2xx response body is valid JSON (every provider's SDK parses
//     the error envelope, so a non-JSON error is a fidelity bug);
//   - a 2xx response is either a JSON document or, for text/event-stream,
//     a sequence of SSE frames whose every data: payload is JSON (or the
//     OpenAI "[DONE]" sentinel).

// fuzzStrictAgent is an OpenAI-protocol agent with every strict-tools
// dimension on, so fuzzed tool/tool_choice/tool-result shapes reach the
// round-11 validation paths (engine/strict.go) as well as the lenient ones.
func fuzzStrictAgent() *types.AgentDefinition {
	a := testOpenAIAgent()
	a.Metadata.Name = "strict-agent"
	a.Spec.Model = "gpt-4o-strict"
	a.Spec.Behavior.StrictTools = &types.StrictToolsConfig{Level: "strict"}
	a.Spec.Tools[0].Parameters = types.JSONSchemaObject{
		"type": "object",
		"properties": map[string]any{
			"city": map[string]any{"type": "string", "minLength": 1},
		},
		"required":             []any{"city"},
		"additionalProperties": false,
	}
	return a
}

func fuzzAdapterAgents() []*types.AgentDefinition {
	return []*types.AgentDefinition{testOpenAIAgent(), testAnthropicAgent(), testGeminiAgent(), fuzzStrictAgent()}
}

// checkFuzzResponse enforces the shared response invariants above.
func checkFuzzResponse(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	code := rec.Code
	if code < 200 || code > 599 {
		t.Fatalf("invalid HTTP status %d", code)
	}
	body := rec.Body.Bytes()
	if code < 200 || code >= 300 {
		if !json.Valid(body) {
			t.Fatalf("status %d with non-JSON error body: %q", code, body)
		}
		return
	}
	if strings.HasPrefix(rec.Header().Get("Content-Type"), "text/event-stream") {
		checkSSEDataIsJSON(t, body)
		return
	}
	if !json.Valid(body) {
		t.Fatalf("status %d with non-JSON body: %q", code, body)
	}
}

// checkSSEDataIsJSON asserts every data: line in an SSE body carries JSON
// (or the [DONE] sentinel). Line endings may be LF or CRLF.
func checkSSEDataIsJSON(t *testing.T, body []byte) {
	t.Helper()
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 64*1024), 16<<20)
	for sc.Scan() {
		line := strings.TrimSuffix(sc.Text(), "\r")
		payload, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		payload = strings.TrimPrefix(payload, " ")
		if payload == "[DONE]" {
			continue
		}
		if !json.Valid([]byte(payload)) {
			t.Fatalf("SSE data line is not JSON: %q\nfull body: %q", payload, body)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scanning SSE body: %v", err)
	}
}

func FuzzOpenAIChatCompletions(f *testing.F) {
	seeds := []string{
		`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`,
		`{"model":"gpt-4o","messages":[{"role":"user","content":"check weather"}],"stream":true}`,
		`{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"text","text":"hi"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}]}`,
		`{"model":"gpt-4o","messages":[{"role":"user","content":"weather"},{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"NYC\"}"}}]},{"role":"tool","tool_call_id":"call_1","content":"{\"temp\":72}"}]}`,
		`{"model":"gpt-4o-strict","messages":[{"role":"user","content":"weather"}],"tools":[{"type":"function","function":{"name":"get_weather","strict":true,"parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}],"tool_choice":"required","parallel_tool_calls":false}`,
		`{"model":"gpt-4o-strict","messages":[{"role":"tool","tool_call_id":"nope","content":"x"}],"tool_choice":{"type":"function","function":{"name":"missing"}}}`,
		`{"model":"gpt-4o","messages":[],"stream":true}`,
		`{"model":"","messages":[{"role":"user","content":"x"}]}`,
		`{"model":"unknown-model","messages":[{"role":"user","content":"x"}]}`,
		`{"model":"gpt-4o","messages":[{"role":"user"}],"response_format":{"type":"json_schema","json_schema":{"name":"x","schema":{"type":"object"}}}}`,
		`{"model":1}`,
		`{`,
		`null`,
		``,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	h := &OpenAIHandler{Engine: testEngine(fuzzAdapterAgents()...)}
	f.Fuzz(func(t *testing.T, body []byte) {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.HandleChatCompletions(rec, req)
		checkFuzzResponse(t, rec)
	})
}

func FuzzAnthropicMessages(f *testing.F) {
	seeds := []string{
		`{"model":"claude-3-opus","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`,
		`{"model":"claude-3-opus","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"search for x"}]}`,
		`{"model":"claude-3-opus","max_tokens":64,"system":[{"type":"text","text":"be nice","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":[{"type":"text","text":"hi"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]}]}`,
		`{"model":"claude-3-opus","max_tokens":64,"messages":[{"role":"user","content":"search"},{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"search","input":{"q":"x"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok"}]}]}`,
		`{"model":"claude-3-opus","max_tokens":64,"tools":[{"name":"search","input_schema":{"type":"object"}}],"tool_choice":{"type":"tool","name":"search"},"messages":[{"role":"user","content":"x"}]}`,
		`{"model":"claude-3-opus","messages":[{"role":"user","content":"no max tokens"}]}`,
		`{"model":"claude-3-opus","max_tokens":-1,"messages":[]}`,
		`{"model":"claude-3-opus","max_tokens":64,"thinking":{"type":"enabled","budget_tokens":1024},"messages":[{"role":"user","content":"x"}]}`,
		`{"messages":"x"}`,
		`[]`,
		``,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	h := &AnthropicHandler{Engine: testEngine(fuzzAdapterAgents()...)}
	f.Fuzz(func(t *testing.T, body []byte) {
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Anthropic-Version", "2023-06-01")
		rec := httptest.NewRecorder()
		h.HandleMessages(rec, req)
		checkFuzzResponse(t, rec)
	})
}

func FuzzGeminiGenerateContent(f *testing.F) {
	type seed struct {
		modelMethod string
		sse         bool
		body        string
	}
	seeds := []seed{
		{"gemini-1.5-pro:generateContent", false, `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`},
		{"gemini-1.5-pro:streamGenerateContent", true, `{"contents":[{"role":"user","parts":[{"text":"weather?"}]}]}`},
		{"gemini-1.5-pro:streamGenerateContent", false, `{"contents":[{"role":"user","parts":[{"text":"weather?"}]}]}`},
		{"gemini-1.5-pro:generateContent", false, `{"systemInstruction":{"parts":[{"text":"sys"}]},"contents":[{"role":"user","parts":[{"inlineData":{"mimeType":"image/png","data":"AAAA"}},{"text":"what"}]}],"tools":[{"functionDeclarations":[{"name":"get_weather","parameters":{"type":"OBJECT"}}]}],"toolConfig":{"functionCallingConfig":{"mode":"ANY","allowedFunctionNames":["get_weather"]}}}`},
		{"gemini-1.5-pro:generateContent", false, `{"contents":[{"role":"model","parts":[{"functionCall":{"name":"get_weather","args":{"city":"x"}}}]},{"role":"user","parts":[{"functionResponse":{"name":"get_weather","response":{"tempC":1}}}]}]}`},
		{"gemini-1.5-pro:generateContent", false, `{"contents":[]}`},
		{"gemini-1.5-pro", false, `{"contents":[{"parts":[{"text":"x"}]}]}`},
		{":generateContent", false, `{}`},
		{"unknown:generateContent", false, `{"contents":[{"parts":[{"text":"x"}]}]}`},
		{"gemini-1.5-pro:countTokens", false, `{"contents":[{"parts":[{"text":"x"}]}]}`},
		{"gemini-1.5-pro:generateContent", false, `{"contents":[{"parts":null}]}`},
		{"gemini-1.5-pro:generateContent", false, ``},
	}
	for _, s := range seeds {
		f.Add(s.modelMethod, s.sse, []byte(s.body))
	}
	h := &GeminiHandler{Engine: testEngine(fuzzAdapterAgents()...)}
	f.Fuzz(func(t *testing.T, modelMethod string, sse bool, body []byte) {
		target := "/v1beta/models/fuzz"
		if sse {
			target += "?alt=sse"
		}
		req := httptest.NewRequest(http.MethodPost, target, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("modelmethod", modelMethod)
		rec := httptest.NewRecorder()
		h.HandleGenerate(rec, req)
		checkFuzzResponse(t, rec)
	})
}
