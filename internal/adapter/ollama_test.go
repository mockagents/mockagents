package adapter

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mockagents/mockagents/internal/drift"
	"github.com/mockagents/mockagents/internal/types"
)

func testOllamaAgent() *types.AgentDefinition {
	return &types.AgentDefinition{
		Metadata: types.Metadata{Name: "ollama-local"},
		Spec: types.AgentSpec{Protocol: ProtocolOllamaChat, Model: "llama3.2", Behavior: types.BehaviorConfig{Scenarios: []types.Scenario{
			{Name: "weather", Match: &types.MatchRule{ContentContains: "weather"}, Response: types.ScenarioResponse{Content: "Checking.", ToolCalls: []types.ToolCallSpec{{Name: "weather", Arguments: map[string]any{"city": "Austin"}}}}},
			{Name: "default", Response: types.ScenarioResponse{Content: "Hello from Ollama."}},
		}}},
	}
}

func ollamaRequest(t *testing.T, h http.HandlerFunc, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func TestOllamaChatNonStreaming(t *testing.T) {
	h := &OllamaHandler{Engine: testEngine(testOllamaAgent())}
	rec := ollamaRequest(t, h.HandleChat, `{"model":"llama3.2","stream":false,"messages":[{"role":"user","content":"hello"}]}`)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status=%d content-type=%q body=%s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	var response OllamaChatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Done || response.Model != "llama3.2" || response.Message.Role != "assistant" || response.Message.Content != "Hello from Ollama." || response.DoneReason != "stop" {
		t.Fatalf("response=%+v", response)
	}
	if response.PromptEvalCount == 0 || response.EvalCount == 0 {
		t.Fatalf("usage missing: %+v", response)
	}
}

func TestOllamaChatDefaultsToNDJSONStreaming(t *testing.T) {
	h := &OllamaHandler{Engine: testEngine(testOllamaAgent())}
	rec := ollamaRequest(t, h.HandleChat, `{"model":"llama3.2","messages":[{"role":"user","content":"weather"}]}`)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/x-ndjson" {
		t.Fatalf("status=%d content-type=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	scanner := bufio.NewScanner(rec.Body)
	var chunks []OllamaChatResponse
	for scanner.Scan() {
		var chunk OllamaChatResponse
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			t.Fatal(err)
		}
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 2 || chunks[0].Done || !chunks[1].Done || chunks[1].DoneReason != "tool_calls" {
		t.Fatalf("chunks=%+v", chunks)
	}
	if got := chunks[0].Message.ToolCalls; len(got) != 1 || got[0].Function.Name != "weather" || got[0].Function.Arguments["city"] != "Austin" {
		t.Fatalf("tool calls=%+v", got)
	}
}

func TestOllamaChatValidationUsesNativeErrorEnvelope(t *testing.T) {
	h := &OllamaHandler{Engine: testEngine(testOllamaAgent())}
	rec := ollamaRequest(t, h.HandleChat, `{"messages":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] != "model is required" {
		t.Fatalf("body=%v err=%v", body, err)
	}
}

func TestOllamaChatDriftBaseline(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "drift", "ollama-chat")
	baselineData, err := os.ReadFile(filepath.Join(dir, "baseline.json"))
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := drift.ParseBaseline(baselineData)
	if err != nil {
		t.Fatal(err)
	}
	readShape := func(name string) map[string]drift.Shape {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		shape, err := drift.Extract(data)
		if err != nil {
			t.Fatal(err)
		}
		shape, err = drift.IgnorePaths(shape, baseline.IgnorePaths)
		if err != nil {
			t.Fatal(err)
		}
		return shape
	}
	report := drift.Compare(baseline.Operation, readShape(baseline.SDK), readShape(baseline.Provider), readShape(baseline.Mock))
	if len(report.Findings) != 0 {
		t.Fatalf("ollama baseline drift: %+v", report.Findings)
	}
}
