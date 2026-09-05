package recording

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIImportEnvelopeShapeThenReplay(t *testing.T) {
	jsonl := `{"request":{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]},"response":{"id":"chatcmpl-1","choices":[{"message":{"content":"envelope reply"}}]}}`
	its, res, err := ImportOpenAIStored(strings.NewReader(jsonl))
	if err != nil || res.Imported != 1 {
		t.Fatalf("imported=%d err=%v reasons=%v", res.Imported, err, res.SkipReasons)
	}
	if its[0].Path != "/v1/chat/completions" || its[0].Method != "POST" {
		t.Errorf("wrong method/path: %s %s", its[0].Method, its[0].Path)
	}

	cass := New("")
	if err := cass.AppendAll(its); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewReplay(cass))
	defer srv.Close()
	resp, body := post(t, srv.URL, `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	if resp.StatusCode != 200 || !strings.Contains(body, "envelope reply") {
		t.Fatalf("envelope import did not replay: status=%d body=%s", resp.StatusCode, body)
	}
}

func TestOpenAIImportFlatShapeWithInputRoutesToResponses(t *testing.T) {
	// A Responses-API stored completion (input, no messages) keeps the `input`
	// key and targets /v1/responses so it hash-matches the originating client.
	jsonl := `{"id":"resp-2","model":"gpt-4o","input":[{"role":"user","content":"flat"}],"temperature":0.7,"choices":[{"message":{"content":"flat reply"}}]}`
	its, res, err := ImportOpenAIStored(strings.NewReader(jsonl))
	if err != nil || res.Imported != 1 {
		t.Fatalf("imported=%d err=%v reasons=%v", res.Imported, err, res.SkipReasons)
	}
	if its[0].Path != "/v1/responses" {
		t.Errorf("input shape should route to /v1/responses, got %s", its[0].Path)
	}
	var req map[string]any
	if err := json.Unmarshal(its[0].RequestBody, &req); err != nil {
		t.Fatalf("reconstructed request not JSON: %v", err)
	}
	if req["model"] != "gpt-4o" {
		t.Errorf("model not carried into request: %v", req["model"])
	}
	if _, ok := req["input"]; !ok {
		t.Errorf("input key must be preserved (not renamed to messages): %v", req)
	}
	if req["temperature"] != 0.7 {
		t.Errorf("sampling param not copied: %v", req["temperature"])
	}
}

func TestOpenAIImportFlatShapeMessagesRoutesToChat(t *testing.T) {
	jsonl := `{"model":"gpt-4o","messages":[{"role":"user","content":"hey"}],"choices":[{"message":{"content":"r"}}]}`
	its, res, err := ImportOpenAIStored(strings.NewReader(jsonl))
	if err != nil || res.Imported != 1 {
		t.Fatalf("imported=%d err=%v", res.Imported, err)
	}
	if its[0].Path != "/v1/chat/completions" {
		t.Errorf("messages shape should route to /v1/chat/completions, got %s", its[0].Path)
	}
}

func TestOpenAIImportFlatShapeWithMessages(t *testing.T) {
	jsonl := `{"model":"gpt-4o","messages":[{"role":"user","content":"hey"}],"choices":[{"message":{"content":"r"}}]}`
	_, res, err := ImportOpenAIStored(strings.NewReader(jsonl))
	if err != nil || res.Imported != 1 {
		t.Fatalf("imported=%d err=%v", res.Imported, err)
	}
}

func TestOpenAIImportMissingInputSkipped(t *testing.T) {
	// Flat shape with choices but no input/messages → cannot reconstruct.
	jsonl := `{"model":"gpt-4o","choices":[{"message":{"content":"r"}}]}`
	_, res, err := ImportOpenAIStored(strings.NewReader(jsonl))
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 0 || res.Skipped != 1 {
		t.Fatalf("missing-input line should be skipped: imported=%d skipped=%d", res.Imported, res.Skipped)
	}
}

func TestOpenAIImportUnrecognizedShapeSkipped(t *testing.T) {
	jsonl := `{"foo":"bar"}`
	_, res, err := ImportOpenAIStored(strings.NewReader(jsonl))
	if err != nil || res.Skipped != 1 {
		t.Fatalf("unrecognized line should skip: skipped=%d err=%v", res.Skipped, err)
	}
}

func TestOpenAIImportMalformedLineSkippedRestProcessed(t *testing.T) {
	jsonl := "not json at all\n" +
		`{"request":{"model":"x"},"response":{"choices":[]}}` + "\n"
	_, res, err := ImportOpenAIStored(strings.NewReader(jsonl))
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 1 || res.Skipped != 1 {
		t.Fatalf("bad line should skip, good line process: imported=%d skipped=%d", res.Imported, res.Skipped)
	}
}

func TestOpenAIImportEnvelopeMissingResponse(t *testing.T) {
	jsonl := `{"request":{"model":"x"}}`
	_, res, err := ImportOpenAIStored(strings.NewReader(jsonl))
	if err != nil || res.Skipped != 1 {
		t.Fatalf("envelope without response should skip: skipped=%d err=%v", res.Skipped, err)
	}
}

func TestAppendAllWritesOnceAndReloads(t *testing.T) {
	path := t.TempDir() + "/out.jsonl"
	cass := New(path)
	its := []*Interaction{
		{Method: "POST", Path: "/v1/chat/completions", RequestBody: json.RawMessage(`{"m":1}`), ResponseStatus: 200, ResponseBody: json.RawMessage(`{"a":1}`)},
		{Method: "POST", Path: "/v1/chat/completions", RequestBody: json.RawMessage(`{"m":2}`), ResponseStatus: 200, ResponseBody: json.RawMessage(`{"a":2}`)},
		{Method: "POST", Path: "/v1/chat/completions", RequestBody: json.RawMessage(`{"m":1}`), ResponseStatus: 200, ResponseBody: json.RawMessage(`{"a":3}`)},
	}
	if err := cass.AppendAll(its); err != nil {
		t.Fatal(err)
	}
	if cass.Len() != 3 {
		t.Fatalf("len = %d, want 3", cass.Len())
	}
	// Hashes were assigned; the two {"m":1} requests share a hash sequence.
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Len() != 3 {
		t.Fatalf("reloaded len = %d, want 3", reloaded.Len())
	}
	seq := reloaded.LookupSequence(HashRequest("POST", "/v1/chat/completions", json.RawMessage(`{"m":1}`)))
	if len(seq) != 2 {
		t.Fatalf("duplicate-hash sequence len = %d, want 2", len(seq))
	}
	if !strings.Contains(string(seq[0].ResponseBody), `"a":1`) || !strings.Contains(string(seq[1].ResponseBody), `"a":3`) {
		t.Errorf("AppendAll did not preserve insertion order: %s / %s", seq[0].ResponseBody, seq[1].ResponseBody)
	}
}

// storedLine renders one importable stored-completion line.
func storedLine(id, content string) string {
	return `{"id":"` + id + `","model":"gpt-4o","messages":[{"role":"user","content":"` + content +
		`"}],"choices":[{"message":{"content":"reply ` + content + `"}}]}` + "\n"
}

// oversizedLine is a syntactically fine JSON line that is past MaxCassetteLine —
// one very long conversation in an export.
func oversizedLine() string {
	var b strings.Builder
	b.Grow(MaxCassetteLine + 1024)
	b.WriteString(`{"id":"huge","model":"gpt-4o","messages":[{"role":"user","content":"`)
	b.WriteString(strings.Repeat("x", MaxCassetteLine+1))
	b.WriteString(`"}],"choices":[{"message":{"content":"reply"}}]}` + "\n")
	return b.String()
}

func TestOpenAIImportOversizedLineSkippedRestImported(t *testing.T) {
	var b strings.Builder
	for i := range 5 {
		b.WriteString(storedLine(fmt.Sprintf("before-%d", i), fmt.Sprintf("before-%d", i)))
	}
	b.WriteString(oversizedLine())
	for i := range 5 {
		b.WriteString(storedLine(fmt.Sprintf("after-%d", i), fmt.Sprintf("after-%d", i)))
	}

	its, res, err := ImportOpenAIStored(strings.NewReader(b.String()))
	if err != nil {
		t.Fatalf("one oversized line should not fail the import: %v", err)
	}
	if res.Imported != 10 {
		t.Fatalf("expected the 10 valid lines, imported %d (skipped %d: %v)", res.Imported, res.Skipped, res.SkipReasons)
	}
	if len(its) != 10 {
		t.Fatalf("expected 10 interactions, got %d", len(its))
	}
	if res.Skipped != 1 {
		t.Fatalf("expected 1 skip, got %d: %v", res.Skipped, res.SkipReasons)
	}
	if len(res.SkipReasons) != 1 || !strings.Contains(res.SkipReasons[0], "line 6:") {
		t.Errorf("skip reason should name line 6, got %v", res.SkipReasons)
	}
	if !strings.Contains(res.SkipReasons[0], "larger than") {
		t.Errorf("skip reason should say why, got %v", res.SkipReasons)
	}
	// The lines after the oversized one are the ones a Scanner would have lost.
	if !strings.Contains(string(its[9].RequestBody), "after-4") {
		t.Errorf("last interaction is %s, expected the final line of the file", its[9].RequestBody)
	}
}

func TestOpenAIImportOversizedFinalLineSkipped(t *testing.T) {
	// No trailing newline: the oversized line runs straight into EOF.
	in := storedLine("ok-0", "ok-0") + strings.TrimSuffix(oversizedLine(), "\n")

	_, res, err := ImportOpenAIStored(strings.NewReader(in))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Imported != 1 || res.Skipped != 1 {
		t.Fatalf("imported=%d skipped=%d reasons=%v", res.Imported, res.Skipped, res.SkipReasons)
	}
	if len(res.SkipReasons) != 1 || !strings.Contains(res.SkipReasons[0], "line 2:") {
		t.Errorf("skip reason should name line 2, got %v", res.SkipReasons)
	}
}

func TestOpenAIImportBlankLinesKeepLineNumbersHonest(t *testing.T) {
	in := storedLine("ok-0", "ok-0") + "\n" + "   \n" + "{not json\n"

	_, res, err := ImportOpenAIStored(strings.NewReader(in))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Imported != 1 || res.Skipped != 1 {
		t.Fatalf("imported=%d skipped=%d reasons=%v", res.Imported, res.Skipped, res.SkipReasons)
	}
	if !strings.Contains(res.SkipReasons[0], "line 4:") {
		t.Errorf("blank lines should still count toward the reported line number, got %v", res.SkipReasons)
	}
}

func TestReadCappedLine(t *testing.T) {
	const max = 8
	tests := []struct {
		name    string
		in      string
		want    []string
		tooLong []bool
	}{
		{
			name:    "plain lines",
			in:      "aa\nbb\ncc\n",
			want:    []string{"aa\n", "bb\n", "cc\n", ""},
			tooLong: []bool{false, false, false, false},
		},
		{
			name:    "no trailing newline",
			in:      "aa\nbb",
			want:    []string{"aa\n", "bb"},
			tooLong: []bool{false, false},
		},
		{
			name:    "oversized line in the middle is drained",
			in:      "aa\n" + strings.Repeat("x", max+1) + "\nbb\n",
			want:    []string{"aa\n", "", "bb\n", ""},
			tooLong: []bool{false, true, false, false},
		},
		{
			name:    "oversized line at EOF",
			in:      "aa\n" + strings.Repeat("x", max+1),
			want:    []string{"aa\n", ""},
			tooLong: []bool{false, true},
		},
		{
			name:    "line exactly at the cap is kept",
			in:      strings.Repeat("x", max-1) + "\n",
			want:    []string{strings.Repeat("x", max-1) + "\n", ""},
			tooLong: []bool{false, false},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// A tiny reader buffer forces the ErrBufferFull path that the real
			// 64 KiB buffer only hits on genuinely large lines.
			br := bufio.NewReaderSize(strings.NewReader(tc.in), 16)
			for i := range tc.want {
				line, tooLong, err := readCappedLine(br, max)
				if string(line) != tc.want[i] {
					t.Errorf("read %d: line=%q want %q", i, line, tc.want[i])
				}
				if tooLong != tc.tooLong[i] {
					t.Errorf("read %d: tooLong=%v want %v", i, tooLong, tc.tooLong[i])
				}
				if err != nil && !errors.Is(err, io.EOF) {
					t.Fatalf("read %d: unexpected error %v", i, err)
				}
			}
		})
	}
}
