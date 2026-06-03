package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/storage"
)

func sampleRow() LogWithCost {
	return LogWithCost{
		InteractionLog: storage.InteractionLog{
			ID:             42,
			Timestamp:      "2026-06-03T20:22:50Z",
			TenantID:       "ten_acme",
			RequestMethod:  "POST",
			RequestPath:    "/v1/chat/completions",
			ResponseStatus: 200,
			LatencyMs:      37,
			ResponseBody:   `{"id":"chatcmpl-abc","choices":[{"message":{"role":"assistant","content":"hello there"}}],"usage":{"prompt_tokens":12,"completion_tokens":8}}`,
			AgentName:      "echo",
			SessionID:      "req-deadbeef",
		},
		PromptTokens:     12,
		CompletionTokens: 8,
		Model:            "gpt-4o",
		CostUSD:          0.00042,
	}
}

// TestAppendLogFrame_WireFormat is the PERF-11 guard: the buffer+encoder framing
// must produce exactly the same bytes the old json.Marshal + fmt.Sprintf path
// did — an `event: log` line, a `data: <json>` line, and a blank terminator —
// and the JSON must round-trip. This is the fast unit-level check that backs the
// end-to-end SSE tests.
func TestAppendLogFrame_WireFormat(t *testing.T) {
	row := sampleRow()

	var frame bytes.Buffer
	enc := json.NewEncoder(&frame)
	if err := appendLogFrame(&frame, enc, row); err != nil {
		t.Fatalf("appendLogFrame: %v", err)
	}
	got := frame.String()

	// Byte-for-byte equivalent of the pre-PERF-11 fmt.Fprintf framing.
	marshaled, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	want := fmt.Sprintf("event: log\ndata: %s\n\n", marshaled)
	if got != want {
		t.Fatalf("frame mismatch:\n got=%q\nwant=%q", got, want)
	}

	// Structure: three SSE lines (event, data, blank terminator).
	if !strings.HasPrefix(got, "event: log\ndata: ") || !strings.HasSuffix(got, "\n\n") {
		t.Fatalf("frame is not a well-formed SSE log frame: %q", got)
	}
	// The data payload must round-trip back to the same row.
	lines := strings.SplitN(got, "\n", 3)
	var back LogWithCost
	if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[1], "data: ")), &back); err != nil {
		t.Fatalf("data payload did not round-trip: %v", err)
	}
	if back.ID != row.ID || back.AgentName != row.AgentName || back.Model != row.Model {
		t.Fatalf("round-trip lost fields: got %+v", back)
	}
}

// TestAppendLogFrame_Reuse verifies the buffer is reset each call, so a second
// (shorter) row never carries bytes left over from a previous (longer) one.
func TestAppendLogFrame_Reuse(t *testing.T) {
	var frame bytes.Buffer
	enc := json.NewEncoder(&frame)

	long := sampleRow()
	long.AgentName = strings.Repeat("x", 500)
	if err := appendLogFrame(&frame, enc, long); err != nil {
		t.Fatal(err)
	}
	firstLen := frame.Len()

	short := sampleRow()
	short.AgentName = "a"
	if err := appendLogFrame(&frame, enc, short); err != nil {
		t.Fatal(err)
	}
	if frame.Len() >= firstLen {
		t.Fatalf("buffer not reset: short frame len %d >= long frame len %d", frame.Len(), firstLen)
	}
	if strings.Contains(frame.String(), strings.Repeat("x", 500)) {
		t.Fatal("short frame carries stale bytes from the previous row")
	}
}

// BenchmarkLogFrame_New measures the PERF-11 framing: one reused buffer + reused
// encoder per connection, zero per-row envelope allocation beyond what the JSON
// encoder needs internally.
func BenchmarkLogFrame_New(b *testing.B) {
	row := sampleRow()
	var frame bytes.Buffer
	enc := json.NewEncoder(&frame)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = appendLogFrame(&frame, enc, row)
		_ = frame.Bytes()
	}
}

// BenchmarkLogFrame_Old reproduces the pre-PERF-11 path (json.Marshal + a
// reflect-based fmt.Sprintf envelope per row) for an honest comparison.
func BenchmarkLogFrame_Old(b *testing.B) {
	row := sampleRow()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buf, _ := json.Marshal(row)
		_ = []byte(fmt.Sprintf("event: log\ndata: %s\n\n", buf))
	}
}
