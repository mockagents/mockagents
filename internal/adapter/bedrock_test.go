package adapter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/drift"
)

func bedrockRequest(t *testing.T, h *BedrockHandler, model, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /model/{modelId}/converse", h.HandleConverse)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/model/"+model+"/converse", strings.NewReader(body)))
	return rec
}

func TestBedrockConverseTextResponse(t *testing.T) {
	h := &BedrockHandler{Engine: testEngine(testOllamaAgent())}
	rec := bedrockRequest(t, h, "anthropic.claude-3-sonnet-20240229-v1:0", `{"messages":[{"role":"user","content":[{"text":"hello"}]}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response BedrockConverseResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.StopReason != "end_turn" || response.Output.Message.Role != "assistant" || response.Output.Message.Content[0].Text != "Hello from Ollama." {
		t.Fatalf("response=%+v", response)
	}
	if response.Usage.InputTokens == 0 || response.Usage.OutputTokens == 0 || response.Usage.TotalTokens != response.Usage.InputTokens+response.Usage.OutputTokens {
		t.Fatalf("usage=%+v", response.Usage)
	}
}

func TestBedrockConverseImagesAndToolUse(t *testing.T) {
	h := &BedrockHandler{Engine: testEngine(testOllamaAgent())}
	body := `{"messages":[{"role":"user","content":[{"text":"weather"},{"image":{"format":"png","source":{"bytes":"aGVsbG8="}}}]}],"toolConfig":{"tools":[{"toolSpec":{"name":"weather","inputSchema":{"json":{"type":"object"}}}}]}}`
	rec := bedrockRequest(t, h, "amazon.nova-lite-v1:0", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-MockAgents-Image-Count") != "1" {
		t.Fatalf("image count=%q", rec.Header().Get("X-MockAgents-Image-Count"))
	}
	var response BedrockConverseResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.StopReason != "tool_use" || len(response.Output.Message.Content) != 2 {
		t.Fatalf("response=%+v", response)
	}
	call := response.Output.Message.Content[1].ToolUse
	if call == nil || call.Name != "weather" || call.ToolUseID == "" || call.Input["city"] != "Austin" {
		t.Fatalf("tool use=%+v", call)
	}
}

func TestBedrockConverseValidationUsesNativeError(t *testing.T) {
	h := &BedrockHandler{Engine: testEngine(testOllamaAgent())}
	rec := bedrockRequest(t, h, "amazon.nova-lite-v1:0", `{"messages":[]}`)
	if rec.Code != http.StatusBadRequest || rec.Header().Get("x-amzn-errortype") != "ValidationException" {
		t.Fatalf("status=%d type=%q", rec.Code, rec.Header().Get("x-amzn-errortype"))
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["message"] != "messages is required" {
		t.Fatalf("body=%v err=%v", body, err)
	}
}

func TestBedrockConverseDriftBaseline(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "drift", "bedrock-converse")
	data, err := os.ReadFile(filepath.Join(dir, "baseline.json"))
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := drift.ParseBaseline(data)
	if err != nil {
		t.Fatal(err)
	}
	read := func(name string) map[string]drift.Shape {
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
	report := drift.Compare(baseline.Operation, read(baseline.SDK), read(baseline.Provider), read(baseline.Mock))
	if len(report.Findings) != 0 {
		t.Fatalf("bedrock baseline drift: %+v", report.Findings)
	}
}
