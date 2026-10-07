package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func fake(t *testing.T, h http.HandlerFunc) Options {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return Options{BaseURL: srv.URL, APIKey: "k"}
}

func TestOpenAIRequestAndToolCalls(t *testing.T) {
	var got map[string]any
	opts := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer k" || r.Header.Get("X-Session-Id") != "s1" {
			t.Errorf("bad request %s %v", r.URL.Path, r.Header)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("X-Mockagents-Hallucination", "fabricated_fact")
		_, _ = io.WriteString(w, `{"model":"m","choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"calc","arguments":"{\"x\":1}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":4}}`)
	})
	resp, err := NewOpenAI(opts).Complete(context.Background(), &Request{
		Model: "m", System: "sys", SessionID: "s1",
		Messages: []Message{{Role: "user", Content: "hi"}, {Role: "assistant", ToolCalls: []ToolCall{{ID: "c0", Name: "calc", Arguments: "{}"}}}, {Role: "tool", ToolCallID: "c0", Content: "1"}},
		Tools:    []ToolSpec{{Name: "calc", Parameters: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.FinishReason != "tool_calls" || len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Arguments != `{"x":1}` || resp.Usage.OutputTokens != 4 {
		t.Errorf("resp %+v", resp)
	}
	if resp.Headers["X-Mockagents-Hallucination"] != "fabricated_fact" {
		t.Errorf("mock headers not captured: %v", resp.Headers)
	}
	msgs := got["messages"].([]any)
	if len(msgs) != 4 || msgs[0].(map[string]any)["role"] != "system" || msgs[3].(map[string]any)["tool_call_id"] != "c0" {
		t.Errorf("wire messages %v", msgs)
	}
}

func TestOpenAIStreamAccumulatesAndDetectsTruncation(t *testing.T) {
	full := "data: {\"choices\":[{\"delta\":{\"content\":\"Hel\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	truncated := "data: {\"choices\":[{\"delta\":{\"content\":\"Hel\"}}]}\n\n"
	body := full
	opts := fake(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	})
	var deltas []string
	resp, err := NewOpenAI(opts).Complete(context.Background(), &Request{Model: "m", Stream: true, OnDelta: func(s string) { deltas = append(deltas, s) },
		Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil || resp.Content != "Hello" || strings.Join(deltas, "|") != "Hel|lo" || resp.FinishReason != "stop" {
		t.Fatalf("resp=%+v deltas=%v err=%v", resp, deltas, err)
	}
	body = truncated
	_, err = NewOpenAI(opts).Complete(context.Background(), &Request{Model: "m", Stream: true, Messages: []Message{{Role: "user", Content: "hi"}}})
	var se *StreamError
	if !errors.As(err, &se) || se.Partial != "Hel" {
		t.Fatalf("expected a StreamError, got %v", err)
	}
	if retryable, reason, _ := Classify(err); !retryable || reason != "stream_integrity" {
		t.Errorf("stream errors must be retryable: %v %s", retryable, reason)
	}
}

func TestAnthropicMergesToolResultsAndUserText(t *testing.T) {
	var got anRequest
	opts := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "k" || r.Header.Get("anthropic-version") == "" {
			t.Errorf("headers %v", r.Header)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, `{"model":"m","content":[{"type":"text","text":"ok"},{"type":"tool_use","id":"t1","name":"lookup","input":{"id":1}}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":2}}`)
	})
	resp, err := NewAnthropic(opts).Complete(context.Background(), &Request{Model: "m", Messages: []Message{
		{Role: "user", Content: "ticket"},
		{Role: "assistant", Content: "calling", ToolCalls: []ToolCall{{ID: "t0", Name: "refund", Arguments: `{"a":1}`}}},
		{Role: "tool", ToolCallID: "t0", Content: `{"status":"declined"}`, IsError: true},
		{Role: "user", Content: "The refund was declined by a human reviewer."},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 3 {
		t.Fatalf("roles must alternate; got %d messages: %+v", len(got.Messages), got.Messages)
	}
	last := got.Messages[2]
	if last.Role != "user" || len(last.Content) != 2 || last.Content[0].Type != "tool_result" || !last.Content[0].IsError || last.Content[1].Type != "text" {
		t.Errorf("tool_result + text must share one user turn: %+v", last)
	}
	if resp.FinishReason != "tool_calls" || resp.Content != "ok" || resp.ToolCalls[0].Arguments != `{"id":1}` {
		t.Errorf("resp %+v", resp)
	}
}

func TestAnthropicMalformedArgsBecomeEmptyObject(t *testing.T) {
	req := (&Anthropic{}).buildRequest(&Request{Messages: []Message{{Role: "assistant", ToolCalls: []ToolCall{{ID: "x", Name: "n", Arguments: `{"broken"`}}}}})
	if string(req.Messages[0].Content[0].Input) != `{}` {
		t.Errorf("invalid JSON input must not be sent as-is: %s", req.Messages[0].Content[0].Input)
	}
}

func TestGemini(t *testing.T) {
	opts := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/slm-classifier:generateContent" || r.Header.Get("x-goog-api-key") != "k" {
			t.Errorf("bad request %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"{\"category\":\"billing\"}"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":6}}`)
	})
	resp, err := NewGemini(opts).Complete(context.Background(), &Request{Model: "slm-classifier", Messages: []Message{{Role: "user", Content: "x"}}})
	if err != nil || resp.FinishReason != "stop" || !strings.Contains(resp.Content, "billing") || resp.Usage.InputTokens != 5 {
		t.Fatalf("resp=%+v err=%v", resp, err)
	}
	if _, err := NewGemini(opts).Complete(context.Background(), &Request{Tools: []ToolSpec{{Name: "x"}}}); err == nil {
		t.Error("tools are not supported by the demo's gemini client")
	}
}

func TestErrorClassification(t *testing.T) {
	status := 429
	opts := fake(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"error":{"type":"rate_limit_error","message":"slow down"}}`)
	})
	_, err := NewAnthropic(opts).Complete(context.Background(), &Request{Model: "m"})
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 429 || ae.Type != "rate_limit_error" || ae.RetryAfter != 2*time.Second {
		t.Fatalf("err %v", err)
	}
	if r, reason, ra := Classify(err); !r || reason != "rate_limited" || ra != 2*time.Second {
		t.Errorf("429 classification: %v %s %v", r, reason, ra)
	}
	for code, want := range map[int]bool{500: true, 502: true, 503: true, 529: true, 400: false, 401: false, 403: false, 404: false} {
		status = code
		_, err := NewOpenAI(opts).Complete(context.Background(), &Request{Model: "m"})
		if r, _, _ := Classify(err); r != want {
			t.Errorf("HTTP %d retryable=%v want %v", code, r, want)
		}
	}
}

func TestTransportAndTimeoutClassification(t *testing.T) {
	// Connection refused -> transport error -> retryable.
	_, err := NewOpenAI(Options{BaseURL: "http://127.0.0.1:1"}).Complete(context.Background(), &Request{Model: "m"})
	if r, reason, _ := Classify(err); !r || reason != "transport" {
		t.Errorf("transport: %v %s (%v)", r, reason, err)
	}
	// Attempt timeout -> retryable; parent cancellation -> permanent.
	slow := fake(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	})
	actx, cancel := WithAttemptTimeout(context.Background(), 50*time.Millisecond)
	_, err = NewOpenAI(slow).Complete(actx, &Request{Model: "m"})
	cancel()
	if r, reason, _ := Classify(err); !r || reason != "attempt_timeout" {
		t.Errorf("attempt timeout: %v %s (%v)", r, reason, err)
	}
	parent, cancelParent := context.WithCancel(context.Background())
	cancelParent()
	_, err = NewOpenAI(slow).Complete(parent, &Request{Model: "m"})
	if r, _, _ := Classify(err); r {
		t.Errorf("a cancelled caller must not retry: %v", err)
	}
}

func TestReadSSEHandlesCRLFAndComments(t *testing.T) {
	var got []string
	err := readSSE(strings.NewReader(": keep-alive\r\nevent: a\r\ndata: 1\r\n\r\ndata: 2\r\ndata: 3\r\n\r\n"), func(ev sseEvent) (bool, error) {
		got = append(got, fmt.Sprintf("%s=%s", ev.Event, ev.Data))
		return false, nil
	})
	if err != nil || strings.Join(got, ";") != "a=1;=2\n3" {
		t.Fatalf("got %q err %v", got, err)
	}
}
