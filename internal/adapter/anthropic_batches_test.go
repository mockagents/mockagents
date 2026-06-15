package adapter

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// anthropicBatchTestRig wires a Message Batches handler whose requests dispatch
// through a real Anthropic /v1/messages handler — the same path a synchronous
// request takes.
type anthropicBatchTestRig struct {
	messages *AnthropicHandler
	batches  *AnthropicBatchesHandler
}

func newAnthropicBatchTestRig() *anthropicBatchTestRig {
	h := &AnthropicHandler{Engine: testEngine(testAnthropicAgent())}
	return &anthropicBatchTestRig{
		messages: h,
		batches:  NewAnthropicBatchesHandler(h.HandleMessages),
	}
}

// msgParams builds a valid /v1/messages params blob for one request.
func msgParams(content string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{
		"model":      "claude-3-opus",
		"max_tokens": 1024,
		"messages":   []map[string]any{{"role": "user", "content": content}},
	})
	return b
}

func batchItem(customID string, params json.RawMessage) anthropicBatchRequestItem {
	return anthropicBatchRequestItem{CustomID: customID, Params: params}
}

func (rig *anthropicBatchTestRig) create(t *testing.T, tenant string, header map[string]string, items ...anthropicBatchRequestItem) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(createAnthropicBatchRequest{Requests: items})
	req := httptest.NewRequest("POST", "/v1/messages/batches", bytes.NewReader(body))
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

func decodeAnthropicBatch(t *testing.T, rec *httptest.ResponseRecorder) AnthropicBatch {
	t.Helper()
	var b AnthropicBatch
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &b), rec.Body.String())
	return b
}

func (rig *anthropicBatchTestRig) results(t *testing.T, tenant, id string) (*httptest.ResponseRecorder, []anthropicBatchResultLine) {
	t.Helper()
	req := httptest.NewRequest("GET", "/v1/messages/batches/"+id+"/results", nil)
	req.SetPathValue("id", id)
	if tenant != "" {
		req = req.WithContext(withTenant(req, tenant))
	}
	rec := httptest.NewRecorder()
	rig.batches.HandleResults(rec, req)
	var lines []anthropicBatchResultLine
	if rec.Code == http.StatusOK {
		for _, raw := range splitJSONLines(rec.Body.Bytes()) {
			var l anthropicBatchResultLine
			require.NoError(t, json.Unmarshal(raw, &l))
			lines = append(lines, l)
		}
	}
	return rec, lines
}

// --- end-to-end ---

func TestAnthropicBatch_EndToEnd(t *testing.T) {
	rig := newAnthropicBatchTestRig()
	rec := rig.create(t, "", nil,
		batchItem("req-1", msgParams("hello")),
		batchItem("req-2", msgParams("anything else")),
	)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	b := decodeAnthropicBatch(t, rec)
	assert.Equal(t, "message_batch", b.Type)
	assert.Regexp(t, `^msgbatch_`, b.ID)
	// delay 0 -> ended immediately.
	assert.Equal(t, "ended", b.ProcessingStatus)
	assert.Equal(t, AnthropicBatchRequestCounts{Succeeded: 2}, b.RequestCounts)
	require.NotNil(t, b.EndedAt)
	require.NotNil(t, b.ResultsURL)
	assert.Contains(t, *b.ResultsURL, "/v1/messages/batches/"+b.ID+"/results")
	// Nullable fields that are not set must serialize as explicit null.
	assert.Nil(t, b.CancelInitiatedAt)
	assert.Nil(t, b.ArchivedAt)

	// Retrieve mirrors create.
	rreq := httptest.NewRequest("GET", "/v1/messages/batches/"+b.ID, nil)
	rreq.SetPathValue("id", b.ID)
	rrec := httptest.NewRecorder()
	rig.batches.HandleRetrieve(rrec, rreq)
	require.Equal(t, http.StatusOK, rrec.Code)
	assert.Equal(t, "ended", decodeAnthropicBatch(t, rrec).ProcessingStatus)

	// Results: one succeeded line per request, each carrying a real Message.
	resRec, lines := rig.results(t, "", b.ID)
	require.Equal(t, http.StatusOK, resRec.Code)
	require.Len(t, lines, 2)
	byID := map[string]anthropicBatchResultLine{}
	for _, l := range lines {
		byID[l.CustomID] = l
	}
	require.Contains(t, byID, "req-1")
	require.Contains(t, byID, "req-2")
	assert.Equal(t, "succeeded", byID["req-1"].Result.Type)
	require.NotNil(t, byID["req-1"].Result.Message)
	var msg map[string]any
	require.NoError(t, json.Unmarshal(byID["req-1"].Result.Message, &msg))
	assert.Equal(t, "message", msg["type"])
}

func TestAnthropicBatch_ErroredRequest(t *testing.T) {
	// A request whose params are invalid for /v1/messages (missing model) is not
	// a structural batch error; it dispatches to a 400 and lands as an errored
	// result, not a failed create.
	rig := newAnthropicBatchTestRig()
	badParams, _ := json.Marshal(map[string]any{
		"max_tokens": 10,
		"messages":   []map[string]any{{"role": "user", "content": "hi"}},
	})
	rec := rig.create(t, "", nil,
		batchItem("ok", msgParams("hello")),
		batchItem("bad", badParams),
	)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	b := decodeAnthropicBatch(t, rec)
	assert.Equal(t, AnthropicBatchRequestCounts{Succeeded: 1, Errored: 1}, b.RequestCounts)

	_, lines := rig.results(t, "", b.ID)
	require.Len(t, lines, 2)
	for _, l := range lines {
		switch l.CustomID {
		case "ok":
			assert.Equal(t, "succeeded", l.Result.Type)
		case "bad":
			assert.Equal(t, "errored", l.Result.Type)
			require.NotNil(t, l.Result.Error)
			var env anthropicErrorEnvelope
			require.NoError(t, json.Unmarshal(l.Result.Error, &env))
			assert.Equal(t, "error", env.Type)
			assert.Equal(t, "invalid_request_error", env.Error.Type)
		}
	}
}

func TestAnthropicBatch_ResultsAreSingleLines(t *testing.T) {
	// Each dispatched message body must be trimmed so it never injects a newline
	// that would break the JSONL framing.
	rig := newAnthropicBatchTestRig()
	b := decodeAnthropicBatch(t, rig.create(t, "", nil, batchItem("r1", msgParams("hello"))))
	resRec, _ := rig.results(t, "", b.ID)
	content := bytes.TrimRight(resRec.Body.Bytes(), "\n")
	assert.NotContains(t, string(content), "\n", "each result record must be one JSONL line")
}

// --- validation ---

func TestAnthropicBatch_Validation(t *testing.T) {
	rig := newAnthropicBatchTestRig()
	cases := []struct {
		name  string
		items []anthropicBatchRequestItem
	}{
		{"empty", nil},
		{"missing custom_id", []anthropicBatchRequestItem{batchItem("", msgParams("x"))}},
		{"missing params", []anthropicBatchRequestItem{{CustomID: "a"}}},
		{"duplicate custom_id", []anthropicBatchRequestItem{
			batchItem("dup", msgParams("a")),
			batchItem("dup", msgParams("b")),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := rig.create(t, "", nil, tc.items...)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			var env anthropicErrorEnvelope
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
			assert.Equal(t, "error", env.Type)
			assert.Equal(t, "invalid_request_error", env.Error.Type)
		})
	}
}

func TestAnthropicBatch_InvalidDelayHeader(t *testing.T) {
	rig := newAnthropicBatchTestRig()
	rec := rig.create(t, "", map[string]string{batchDelayHeader: "-5"}, batchItem("a", msgParams("hello")))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAnthropicBatch_InvalidJSON(t *testing.T) {
	rig := newAnthropicBatchTestRig()
	req := httptest.NewRequest("POST", "/v1/messages/batches", bytes.NewReader([]byte("{not json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	rig.batches.HandleCreate(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// --- lifecycle (deterministic, via render/cancel/resultsBytes directly) ---

func TestAnthropicBatch_Render_DelayLifecycle(t *testing.T) {
	created := time.Unix(1_700_000_000, 0)
	st := &anthropicBatchState{
		base:       AnthropicBatch{ID: "b", Type: "message_batch"},
		createdAt:  created,
		delay:      100 * time.Millisecond,
		total:      2,
		succeeded:  2,
		results:    []byte(`{"custom_id":"x","result":{"type":"succeeded"}}` + "\n"),
		resultsURL: "http://x/results",
	}

	// Before the delay: in_progress, everything still processing, no results.
	mid := st.render(created.Add(50 * time.Millisecond))
	assert.Equal(t, "in_progress", mid.ProcessingStatus)
	assert.Equal(t, AnthropicBatchRequestCounts{Processing: 2}, mid.RequestCounts)
	assert.Nil(t, mid.EndedAt)
	assert.Nil(t, mid.ResultsURL)
	if _, ended := st.resultsBytes(created.Add(50 * time.Millisecond)); ended {
		t.Fatal("results must not be available before the batch ends")
	}

	// After the delay: ended, results_url + succeeded tally exposed.
	done := st.render(created.Add(200 * time.Millisecond))
	assert.Equal(t, "ended", done.ProcessingStatus)
	assert.Equal(t, AnthropicBatchRequestCounts{Succeeded: 2}, done.RequestCounts)
	require.NotNil(t, done.EndedAt)
	require.NotNil(t, done.ResultsURL)
	data, ended := st.resultsBytes(created.Add(200 * time.Millisecond))
	assert.True(t, ended)
	assert.NotEmpty(t, data)
}

func TestAnthropicBatch_Cancel_Lifecycle(t *testing.T) {
	created := time.Unix(1_700_000_000, 0)
	st := &anthropicBatchState{
		base:       AnthropicBatch{ID: "b", Type: "message_batch"},
		createdAt:  created,
		delay:      time.Hour, // keep it in flight so the cancel is observable
		total:      2,
		succeeded:  2,
		customIDs:  []string{"r1", "r2"},
		resultsURL: "http://x/results",
	}

	terminal := st.cancel(created.Add(time.Second))
	assert.False(t, terminal)

	// While the (simulated) window is open: canceling, with cancel_initiated_at.
	canceling := st.render(created.Add(2 * time.Second))
	assert.Equal(t, "canceling", canceling.ProcessingStatus)
	require.NotNil(t, canceling.CancelInitiatedAt)
	assert.Equal(t, AnthropicBatchRequestCounts{Processing: 2}, canceling.RequestCounts)

	// Past the window: ended with every request canceled, and the results stream
	// reports each as canceled.
	ended := st.render(created.Add(time.Hour + time.Second))
	assert.Equal(t, "ended", ended.ProcessingStatus)
	assert.Equal(t, AnthropicBatchRequestCounts{Canceled: 2}, ended.RequestCounts)
	data, ok := st.resultsBytes(created.Add(time.Hour + time.Second))
	require.True(t, ok)
	var n int
	for _, raw := range splitJSONLines(data) {
		var l anthropicBatchResultLine
		require.NoError(t, json.Unmarshal(raw, &l))
		assert.Equal(t, "canceled", l.Result.Type)
		n++
	}
	assert.Equal(t, 2, n)

	// A second cancel is an idempotent no-op (still not "already terminal").
	assert.False(t, st.cancel(created.Add(3*time.Second)))
}

func TestAnthropicBatch_CancelAfterEndedIsNoOp(t *testing.T) {
	created := time.Unix(1_700_000_000, 0)
	st := &anthropicBatchState{base: AnthropicBatch{ID: "b"}, createdAt: created, delay: 0}
	// delay 0 -> already ended; cancel reports terminal and leaves status ended.
	assert.True(t, st.cancel(created.Add(time.Second)))
	assert.Equal(t, "ended", st.render(created.Add(time.Second)).ProcessingStatus)
}

func TestAnthropicBatch_HandleCancel_HTTP(t *testing.T) {
	rig := newAnthropicBatchTestRig()
	// A long delay keeps the batch in flight so the HTTP cancel transitions it.
	b := decodeAnthropicBatch(t, rig.create(t, "", map[string]string{batchDelayHeader: "600000"}, batchItem("r1", msgParams("hello"))))
	require.Equal(t, "in_progress", b.ProcessingStatus)

	req := httptest.NewRequest("POST", "/v1/messages/batches/"+b.ID+"/cancel", nil)
	req.SetPathValue("id", b.ID)
	rec := httptest.NewRecorder()
	rig.batches.HandleCancel(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	cb := decodeAnthropicBatch(t, rec)
	assert.Equal(t, "canceling", cb.ProcessingStatus)
	require.NotNil(t, cb.CancelInitiatedAt)
}

// --- results gating ---

func TestAnthropicBatch_ResultsNotReadyWhileInProgress(t *testing.T) {
	rig := newAnthropicBatchTestRig()
	b := decodeAnthropicBatch(t, rig.create(t, "", map[string]string{batchDelayHeader: "600000"}, batchItem("r1", msgParams("hello"))))
	require.Equal(t, "in_progress", b.ProcessingStatus)
	rec, _ := rig.results(t, "", b.ID)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "results must not be available while processing")
}

// --- delete ---

func TestAnthropicBatch_Delete(t *testing.T) {
	rig := newAnthropicBatchTestRig()
	b := decodeAnthropicBatch(t, rig.create(t, "", nil, batchItem("r1", msgParams("hello"))))
	require.Equal(t, "ended", b.ProcessingStatus)

	del := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("DELETE", "/v1/messages/batches/"+b.ID, nil)
		req.SetPathValue("id", b.ID)
		rec := httptest.NewRecorder()
		rig.batches.HandleDelete(rec, req)
		return rec
	}
	rec := del()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "message_batch_deleted", body["type"])

	// Now gone.
	rreq := httptest.NewRequest("GET", "/v1/messages/batches/"+b.ID, nil)
	rreq.SetPathValue("id", b.ID)
	rrec := httptest.NewRecorder()
	rig.batches.HandleRetrieve(rrec, rreq)
	assert.Equal(t, http.StatusNotFound, rrec.Code)
}

func TestAnthropicBatch_DeleteWhileProcessingRejected(t *testing.T) {
	rig := newAnthropicBatchTestRig()
	b := decodeAnthropicBatch(t, rig.create(t, "", map[string]string{batchDelayHeader: "600000"}, batchItem("r1", msgParams("hello"))))
	require.Equal(t, "in_progress", b.ProcessingStatus)

	req := httptest.NewRequest("DELETE", "/v1/messages/batches/"+b.ID, nil)
	req.SetPathValue("id", b.ID)
	rec := httptest.NewRecorder()
	rig.batches.HandleDelete(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "an in-progress batch cannot be deleted")
}

// --- list / not-found / isolation ---

func TestAnthropicBatch_List(t *testing.T) {
	rig := newAnthropicBatchTestRig()
	rig.create(t, "", nil, batchItem("a", msgParams("hello")))
	rig.create(t, "", nil, batchItem("b", msgParams("hello")))

	req := httptest.NewRequest("GET", "/v1/messages/batches", nil)
	rec := httptest.NewRecorder()
	rig.batches.HandleList(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data    []AnthropicBatch `json:"data"`
		HasMore bool             `json:"has_more"`
		FirstID *string          `json:"first_id"`
		LastID  *string          `json:"last_id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Len(t, resp.Data, 2)
	assert.False(t, resp.HasMore)
	require.NotNil(t, resp.FirstID)
	require.NotNil(t, resp.LastID)
}

func TestAnthropicBatch_RetrieveNotFound(t *testing.T) {
	rig := newAnthropicBatchTestRig()
	req := httptest.NewRequest("GET", "/v1/messages/batches/msgbatch_nope", nil)
	req.SetPathValue("id", "msgbatch_nope")
	rec := httptest.NewRecorder()
	rig.batches.HandleRetrieve(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
	var env anthropicErrorEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	assert.Equal(t, "not_found_error", env.Error.Type)
}

func TestAnthropicBatch_TenantIsolation(t *testing.T) {
	rig := newAnthropicBatchTestRig()
	b := decodeAnthropicBatch(t, rig.create(t, "tenant-1", nil, batchItem("r1", msgParams("hello"))))

	// Tenant 2 cannot retrieve tenant 1's batch.
	req := httptest.NewRequest("GET", "/v1/messages/batches/"+b.ID, nil)
	req.SetPathValue("id", b.ID)
	req = req.WithContext(withTenant(req, "tenant-2"))
	rec := httptest.NewRecorder()
	rig.batches.HandleRetrieve(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// Tenant 2's list is empty.
	lreq := httptest.NewRequest("GET", "/v1/messages/batches", nil)
	lreq = lreq.WithContext(withTenant(lreq, "tenant-2"))
	lrec := httptest.NewRecorder()
	rig.batches.HandleList(lrec, lreq)
	var resp struct {
		Data []AnthropicBatch `json:"data"`
	}
	require.NoError(t, json.Unmarshal(lrec.Body.Bytes(), &resp))
	assert.Empty(t, resp.Data)
}
