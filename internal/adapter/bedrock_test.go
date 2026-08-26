package adapter

import (
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
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

type decodedBedrockEvent struct {
	Type    string
	Payload map[string]any
}

func bedrockStreamEvents(t *testing.T, data []byte) []decodedBedrockEvent {
	t.Helper()
	var events []decodedBedrockEvent
	for len(data) > 0 {
		if len(data) < 16 {
			t.Fatalf("truncated event frame: %d bytes", len(data))
		}
		total := int(binary.BigEndian.Uint32(data[:4]))
		headers := int(binary.BigEndian.Uint32(data[4:8]))
		if total < 16 || total > len(data) || 12+headers > total-4 {
			t.Fatalf("invalid frame total=%d headers=%d remaining=%d", total, headers, len(data))
		}
		if got, want := binary.BigEndian.Uint32(data[8:12]), crc32.ChecksumIEEE(data[:8]); got != want {
			t.Fatalf("prelude crc=%08x want=%08x", got, want)
		}
		if got, want := binary.BigEndian.Uint32(data[total-4:total]), crc32.ChecksumIEEE(data[:total-4]); got != want {
			t.Fatalf("message crc=%08x want=%08x", got, want)
		}
		headerData := data[12 : 12+headers]
		eventType := ""
		for len(headerData) > 0 {
			nameLen := int(headerData[0])
			if len(headerData) < 1+nameLen+3 || headerData[1+nameLen] != 7 {
				t.Fatal("invalid event header")
			}
			name := string(headerData[1 : 1+nameLen])
			valueLen := int(binary.BigEndian.Uint16(headerData[2+nameLen : 4+nameLen]))
			if len(headerData) < 4+nameLen+valueLen {
				t.Fatal("truncated event header")
			}
			value := string(headerData[4+nameLen : 4+nameLen+valueLen])
			if name == ":event-type" {
				eventType = value
			}
			headerData = headerData[4+nameLen+valueLen:]
		}
		var payload map[string]any
		if err := json.Unmarshal(data[12+headers:total-4], &payload); err != nil {
			t.Fatal(err)
		}
		events = append(events, decodedBedrockEvent{Type: eventType, Payload: payload})
		data = data[total:]
	}
	return events
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

func TestBedrockConverseStreamUsesAWSEventStreamOrder(t *testing.T) {
	h := &BedrockHandler{Engine: testEngine(testOllamaAgent())}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /model/{modelId}/converse-stream", h.HandleConverseStream)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/model/amazon.nova-lite-v1:0/converse-stream", strings.NewReader(`{"messages":[{"role":"user","content":[{"text":"hello"}]}]}`)))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/vnd.amazon.eventstream" {
		t.Fatalf("status=%d content-type=%q body=%x", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Bytes())
	}
	events := bedrockStreamEvents(t, rec.Body.Bytes())
	want := []string{"messageStart", "contentBlockDelta", "contentBlockStop", "messageStop", "metadata"}
	if len(events) != len(want) {
		t.Fatalf("events=%v", events)
	}
	for i, name := range want {
		if events[i].Type != name {
			t.Fatalf("event %d=%v want %s", i, events[i], name)
		}
	}
}

func TestBedrockConverseStreamToolBlockSequence(t *testing.T) {
	h := &BedrockHandler{Engine: testEngine(testOllamaAgent())}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /model/{modelId}/converse-stream", h.HandleConverseStream)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/model/amazon.nova-lite-v1:0/converse-stream", strings.NewReader(`{"messages":[{"role":"user","content":[{"text":"weather"}]}]}`)))
	events := bedrockStreamEvents(t, rec.Body.Bytes())
	foundStart := false
	for _, event := range events {
		if event.Type == "contentBlockStart" {
			foundStart = true
		}
	}
	if !foundStart {
		t.Fatalf("tool stream missing contentBlockStart: %v", events)
	}
}

func TestBedrockConverseStreamValidationRemainsJSON(t *testing.T) {
	h := &BedrockHandler{Engine: testEngine(testOllamaAgent())}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /model/{modelId}/converse-stream", h.HandleConverseStream)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/model/amazon.nova-lite-v1:0/converse-stream", strings.NewReader(`{"messages":[]}`)))
	if rec.Code != http.StatusBadRequest || rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("x-amzn-errortype") != "ValidationException" {
		t.Fatalf("status=%d headers=%v body=%s", rec.Code, rec.Header(), rec.Body.String())
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
