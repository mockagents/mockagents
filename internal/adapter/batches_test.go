package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withTenant returns r's context with the tenant id set, as the auth/tenancy
// middleware would in multi-tenant mode.
func withTenant(r *http.Request, tenant string) context.Context {
	return engine.WithTenantID(r.Context(), tenant)
}

// batchTestRig wires a shared file store, a Files handler, and a Batches handler
// whose endpoints dispatch through the real chat + embeddings handlers — the
// same path a synchronous request takes.
type batchTestRig struct {
	files   *fileStore
	filesH  *FilesHandler
	batches *BatchesHandler
}

func newBatchTestRig() *batchTestRig {
	files := newFileStore()
	oai := &OpenAIHandler{Engine: testEngine(testOpenAIAgent())}
	emb := &EmbeddingsHandler{}
	return &batchTestRig{
		files:  files,
		filesH: NewFilesHandler(files),
		batches: NewBatchesHandler(files, map[string]http.HandlerFunc{
			"/v1/chat/completions": oai.HandleChatCompletions,
			"/v1/embeddings":       emb.HandleEmbeddings,
		}),
	}
}

// uploadInput stores an input JSONL directly in the file store and returns its id.
func (rig *batchTestRig) uploadInput(t *testing.T, tenant string, lines ...string) string {
	t.Helper()
	data := []byte(strings.Join(lines, "\n") + "\n")
	id := rig.batches.putGeneratedFile(tenant, "batch", "in.jsonl", data)
	return id
}

func chatLine(customID, content string) string {
	body := map[string]any{
		"model":    "gpt-4o",
		"messages": []map[string]any{{"role": "user", "content": content}},
	}
	return inputLine(customID, "/v1/chat/completions", body)
}

func inputLine(customID, url string, body any) string {
	b, _ := json.Marshal(body)
	line, _ := json.Marshal(map[string]any{
		"custom_id": customID,
		"method":    "POST",
		"url":       url,
		"body":      json.RawMessage(b),
	})
	return string(line)
}

func (rig *batchTestRig) create(t *testing.T, tenant, inputFileID, endpoint string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(createBatchRequest{InputFileID: inputFileID, Endpoint: endpoint, CompletionWindow: "24h"})
	req := httptest.NewRequest("POST", "/v1/batches", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	if tenant != "" {
		req = req.WithContext(withTenant(req, tenant))
	}
	rec := httptest.NewRecorder()
	rig.batches.HandleCreate(rec, req)
	return rec
}

func decodeBatch(t *testing.T, rec *httptest.ResponseRecorder) Batch {
	t.Helper()
	var b Batch
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &b), rec.Body.String())
	return b
}

// downloadFileLines fetches a file's content and splits it into JSONL lines.
func (rig *batchTestRig) downloadFileLines(t *testing.T, tenant, fileID string) []batchOutputLine {
	t.Helper()
	req := httptest.NewRequest("GET", "/v1/files/"+fileID+"/content", nil)
	req.SetPathValue("id", fileID)
	if tenant != "" {
		req = req.WithContext(withTenant(req, tenant))
	}
	rec := httptest.NewRecorder()
	rig.filesH.HandleContent(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var out []batchOutputLine
	for _, raw := range splitJSONLines(rec.Body.Bytes()) {
		var l batchOutputLine
		require.NoError(t, json.Unmarshal(raw, &l))
		out = append(out, l)
	}
	return out
}

// --- end-to-end ---

func TestBatch_EndToEnd_ChatCompletions(t *testing.T) {
	rig := newBatchTestRig()
	in := rig.uploadInput(t, "",
		chatLine("req-1", "hello"),
		chatLine("req-2", "what is the weather"),
	)

	rec := rig.create(t, "", in, "/v1/chat/completions", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	b := decodeBatch(t, rec)
	assert.Equal(t, "batch", b.Object)
	assert.Regexp(t, `^batch_`, b.ID)
	// delay 0 -> completed immediately.
	assert.Equal(t, "completed", b.Status)
	require.NotNil(t, b.OutputFileID)
	assert.Nil(t, b.ErrorFileID)
	assert.Equal(t, BatchRequestCounts{Total: 2, Completed: 2, Failed: 0}, b.RequestCounts)

	// Retrieve mirrors create.
	rreq := httptest.NewRequest("GET", "/v1/batches/"+b.ID, nil)
	rreq.SetPathValue("id", b.ID)
	rrec := httptest.NewRecorder()
	rig.batches.HandleRetrieve(rrec, rreq)
	require.Equal(t, http.StatusOK, rrec.Code)
	assert.Equal(t, "completed", decodeBatch(t, rrec).Status)

	// Output file: one line per request, each a real chat completion.
	lines := rig.downloadFileLines(t, "", *b.OutputFileID)
	require.Len(t, lines, 2)
	ids := map[string]bool{}
	for _, l := range lines {
		ids[l.CustomID] = true
		require.NotNil(t, l.Response, "custom_id %s", l.CustomID)
		assert.Equal(t, 200, l.Response.StatusCode)
		assert.Regexp(t, `^req_`, l.Response.RequestID)
		var chat ChatCompletionResponse
		require.NoError(t, json.Unmarshal(l.Response.Body, &chat))
		assert.Equal(t, "chat.completion", chat.Object)
		require.Len(t, chat.Choices, 1)
	}
	assert.True(t, ids["req-1"] && ids["req-2"])
}

func TestBatch_EndToEnd_Embeddings(t *testing.T) {
	rig := newBatchTestRig()
	in := rig.uploadInput(t, "",
		inputLine("e-1", "/v1/embeddings", map[string]any{"model": "text-embedding-3-small", "input": "hello"}),
	)
	rec := rig.create(t, "", in, "/v1/embeddings", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	b := decodeBatch(t, rec)
	require.Equal(t, "completed", b.Status)
	require.NotNil(t, b.OutputFileID)
	lines := rig.downloadFileLines(t, "", *b.OutputFileID)
	require.Len(t, lines, 1)
	require.NotNil(t, lines[0].Response)
	assert.Equal(t, 200, lines[0].Response.StatusCode)
}

func TestBatch_OutputBodyIsSingleLine(t *testing.T) {
	// The dispatched body must be trimmed so it never injects a newline that
	// would break the JSONL framing.
	rig := newBatchTestRig()
	in := rig.uploadInput(t, "", chatLine("req-1", "hello"))
	b := decodeBatch(t, rig.create(t, "", in, "/v1/chat/completions", nil))
	require.NotNil(t, b.OutputFileID)

	req := httptest.NewRequest("GET", "/v1/files/"+*b.OutputFileID+"/content", nil)
	req.SetPathValue("id", *b.OutputFileID)
	rec := httptest.NewRecorder()
	rig.filesH.HandleContent(rec, req)
	content := bytes.TrimRight(rec.Body.Bytes(), "\n")
	assert.NotContains(t, string(content), "\n", "each output record must be one JSONL line")
}

// --- validation / error file ---

func TestBatch_InvalidLinesGoToErrorFile(t *testing.T) {
	rig := newBatchTestRig()
	in := rig.uploadInput(t, "",
		chatLine("ok-1", "hello"), // valid
		`{not json}`,              // malformed
		inputLine("dup", "/v1/chat/completions", map[string]any{"model": "gpt-4o", "messages": []any{}}), // valid line, body
		inputLine("dup", "/v1/chat/completions", map[string]any{"model": "gpt-4o", "messages": []any{}}), // duplicate custom_id
		inputLine("wrong-ep", "/v1/embeddings", map[string]any{"input": "x"}),                            // endpoint mismatch
	)
	b := decodeBatch(t, rig.create(t, "", in, "/v1/chat/completions", nil))
	require.Equal(t, "completed", b.Status)
	assert.Equal(t, 5, b.RequestCounts.Total)
	// malformed + duplicate + endpoint-mismatch are failures; the empty-messages
	// chat line dispatches to a 400 (also a failure). Only ok-1 completes.
	assert.Equal(t, 1, b.RequestCounts.Completed)
	assert.Equal(t, 4, b.RequestCounts.Failed)
	require.NotNil(t, b.ErrorFileID, "malformed/duplicate/mismatch lines produce an error file")

	errLines := rig.downloadFileLines(t, "", *b.ErrorFileID)
	// malformed (1) + duplicate (1) + endpoint-mismatch (1) = 3 error-file lines.
	require.Len(t, errLines, 3)
	for _, l := range errLines {
		require.NotNil(t, l.Error)
		assert.Nil(t, l.Response)
	}
}

func TestBatch_AllLinesFail_NoOutputFile(t *testing.T) {
	// When every line fails validation there is no output, so the batch must
	// not surface an output_file_id (only an error file).
	rig := newBatchTestRig()
	in := rig.uploadInput(t, "",
		`{not json}`,
		inputLine("x", "/v1/embeddings", map[string]any{"input": "y"}), // endpoint mismatch
	)
	b := decodeBatch(t, rig.create(t, "", in, "/v1/chat/completions", nil))
	require.Equal(t, "completed", b.Status)
	assert.Equal(t, BatchRequestCounts{Total: 2, Completed: 0, Failed: 2}, b.RequestCounts)
	assert.Nil(t, b.OutputFileID, "no output file when every line failed")
	require.NotNil(t, b.ErrorFileID)
}

func TestBatch_InputFileNotFound(t *testing.T) {
	rig := newBatchTestRig()
	rec := rig.create(t, "", "file-nope", "/v1/chat/completions", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "No such File object")
}

func TestBatch_UnsupportedEndpoint(t *testing.T) {
	rig := newBatchTestRig()
	in := rig.uploadInput(t, "", chatLine("a", "hello"))
	rec := rig.create(t, "", in, "/v1/moderations", nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "unsupported endpoint")
}

func TestBatch_EmptyInput(t *testing.T) {
	rig := newBatchTestRig()
	id := rig.batches.putGeneratedFile("", "batch", "empty.jsonl", []byte("\n\n  \n"))
	rec := rig.create(t, "", id, "/v1/chat/completions", nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "no requests")
}

func TestBatch_RetrieveNotFound(t *testing.T) {
	rig := newBatchTestRig()
	req := httptest.NewRequest("GET", "/v1/batches/batch_missing", nil)
	req.SetPathValue("id", "batch_missing")
	rec := httptest.NewRecorder()
	rig.batches.HandleRetrieve(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "No such Batch object")
}

func TestBatch_List(t *testing.T) {
	rig := newBatchTestRig()
	in := rig.uploadInput(t, "", chatLine("a", "hello"))
	rig.create(t, "", in, "/v1/chat/completions", nil)
	rig.create(t, "", in, "/v1/chat/completions", nil)

	req := httptest.NewRequest("GET", "/v1/batches", nil)
	rec := httptest.NewRecorder()
	rig.batches.HandleList(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var list struct {
		Object  string  `json:"object"`
		Data    []Batch `json:"data"`
		HasMore bool    `json:"has_more"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	assert.Equal(t, "list", list.Object)
	assert.Len(t, list.Data, 2)
	assert.False(t, list.HasMore)
}

// --- lifecycle (deterministic, via render/cancel directly) ---

func TestBatch_Render_DelayLifecycle(t *testing.T) {
	created := time.Unix(1_700_000_000, 0)
	st := &batchState{
		base:         Batch{ID: "batch_x", Object: "batch", CreatedAt: created.Unix()},
		createdAt:    created,
		delay:        100 * time.Millisecond,
		counts:       BatchRequestCounts{Total: 3, Completed: 3},
		outputFileID: "file-out",
	}
	// Before the delay elapses: in_progress, no output file, total-only counts.
	mid := st.render(created.Add(50 * time.Millisecond))
	assert.Equal(t, "in_progress", mid.Status)
	assert.Nil(t, mid.OutputFileID)
	assert.Equal(t, BatchRequestCounts{Total: 3}, mid.RequestCounts)

	// After the delay: completed, output file + full counts exposed.
	done := st.render(created.Add(150 * time.Millisecond))
	assert.Equal(t, "completed", done.Status)
	require.NotNil(t, done.OutputFileID)
	assert.Equal(t, "file-out", *done.OutputFileID)
	assert.Equal(t, BatchRequestCounts{Total: 3, Completed: 3}, done.RequestCounts)
}

func TestBatch_Cancel(t *testing.T) {
	created := time.Now()
	st := &batchState{
		base:      Batch{ID: "batch_x", Object: "batch", CreatedAt: created.Unix()},
		createdAt: created,
		delay:     time.Hour, // keep it in flight so cancel is allowed
		counts:    BatchRequestCounts{Total: 1},
	}
	status, terminal := st.cancel(created.Add(time.Second))
	assert.Equal(t, "cancelled", status)
	assert.False(t, terminal)
	assert.Equal(t, "cancelled", st.render(created.Add(time.Second)).Status)

	// Second cancel is a terminal no-op.
	_, terminal2 := st.cancel(created.Add(2 * time.Second))
	assert.True(t, terminal2)
}

func TestBatch_CancelCompletedIsConflict(t *testing.T) {
	created := time.Now()
	st := &batchState{base: Batch{ID: "b"}, createdAt: created, delay: 0}
	_, terminal := st.cancel(created.Add(time.Second)) // delay 0 -> already completed
	assert.True(t, terminal)
}

func TestBatch_HandleCancel_HTTP(t *testing.T) {
	rig := newBatchTestRig()
	in := rig.uploadInput(t, "", chatLine("a", "hello"))
	// A long delay keeps the batch in_progress so the HTTP cancel succeeds.
	b := decodeBatch(t, rig.create(t, "", in, "/v1/chat/completions", map[string]string{batchDelayHeader: "600000"}))
	require.Equal(t, "in_progress", b.Status)

	req := httptest.NewRequest("POST", "/v1/batches/"+b.ID+"/cancel", nil)
	req.SetPathValue("id", b.ID)
	rec := httptest.NewRecorder()
	rig.batches.HandleCancel(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "cancelled", decodeBatch(t, rec).Status)
}

func TestBatch_InvalidDelayHeader(t *testing.T) {
	rig := newBatchTestRig()
	in := rig.uploadInput(t, "", chatLine("a", "hello"))
	rec := rig.create(t, "", in, "/v1/chat/completions", map[string]string{batchDelayHeader: "-5"})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestBatch_TenantIsolation(t *testing.T) {
	rig := newBatchTestRig()
	in1 := rig.uploadInput(t, "t1", chatLine("a", "hello"))
	b1 := decodeBatch(t, rig.create(t, "t1", in1, "/v1/chat/completions", nil))

	// Tenant t2 cannot retrieve t1's batch.
	req := httptest.NewRequest("GET", "/v1/batches/"+b1.ID, nil)
	req.SetPathValue("id", b1.ID)
	req = req.WithContext(withTenant(req, "t2"))
	rec := httptest.NewRecorder()
	rig.batches.HandleRetrieve(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestBatch_StreamingNeutralized(t *testing.T) {
	// A line that asks for stream:true must still yield a single JSON completion
	// (not SSE) in the output file.
	rig := newBatchTestRig()
	body := map[string]any{
		"model":    "gpt-4o",
		"messages": []map[string]any{{"role": "user", "content": "hello"}},
		"stream":   true,
	}
	in := rig.uploadInput(t, "", inputLine("s-1", "/v1/chat/completions", body))
	b := decodeBatch(t, rig.create(t, "", in, "/v1/chat/completions", nil))
	require.Equal(t, "completed", b.Status)
	require.NotNil(t, b.OutputFileID)
	lines := rig.downloadFileLines(t, "", *b.OutputFileID)
	require.Len(t, lines, 1)
	require.NotNil(t, lines[0].Response)
	assert.Equal(t, 200, lines[0].Response.StatusCode)
	var chat ChatCompletionResponse
	require.NoError(t, json.Unmarshal(lines[0].Response.Body, &chat), "body must be a single JSON completion, not SSE")
	assert.Equal(t, "chat.completion", chat.Object)
}

func TestDisableStreaming(t *testing.T) {
	got := disableStreaming(json.RawMessage(`{"model":"x","stream":true}`))
	var m map[string]any
	require.NoError(t, json.Unmarshal(got, &m))
	assert.Equal(t, false, m["stream"])

	// No stream key -> unchanged.
	orig := json.RawMessage(`{"model":"x"}`)
	assert.Equal(t, orig, disableStreaming(orig))

	// Non-object -> unchanged.
	notObj := json.RawMessage(`"hi"`)
	assert.Equal(t, notObj, disableStreaming(notObj))
}

func TestSplitJSONLines(t *testing.T) {
	got := splitJSONLines([]byte("a\r\nb\n\n  \nc"))
	require.Len(t, got, 3)
	assert.Equal(t, "a", string(got[0]))
	assert.Equal(t, "b", string(got[1]))
	assert.Equal(t, "c", string(got[2]))
}

func TestParseBatchDelay(t *testing.T) {
	d, err := parseBatchDelay("")
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), d)

	d, err = parseBatchDelay("250")
	require.NoError(t, err)
	assert.Equal(t, 250*time.Millisecond, d)

	_, err = parseBatchDelay("nope")
	assert.Error(t, err)

	// Clamped to the max.
	d, _ = parseBatchDelay("99999999")
	assert.Equal(t, time.Duration(maxBatchDelayMs)*time.Millisecond, d)
}
