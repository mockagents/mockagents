package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
)

// ProtocolOpenAIBatches is the wire-protocol label recorded for the OpenAI
// Batch surface (POST /v1/batches + retrieve/list/cancel). The Batch API (A-08)
// is the asynchronous, file-driven sibling of the per-request endpoints: a
// client uploads a JSONL of requests via /v1/files, creates a batch over it,
// polls until terminal, then downloads the output file. The mock processes the
// whole file eagerly (deterministically, in-memory) at create time and exposes
// a configurable processing delay so a poll loop can observe the lifecycle.
const ProtocolOpenAIBatches = "openai-batches"

// Batch limits.
const (
	// maxBatchRequests bounds the request lines in one input file, matching
	// OpenAI's per-batch cap and keeping the eager in-memory processing bounded.
	maxBatchRequests = 50000
	// maxBatchesPerTenant bounds stored batches per tenant (FIFO eviction).
	maxBatchesPerTenant = 256
	// batchExpiryWindow is the fixed expires_at offset (OpenAI batches expire
	// 24h after creation). The mock never actually expires them; this only
	// populates the wire field.
	batchExpiryWindow = 24 * time.Hour
	// batchDelayHeader optionally delays a batch's transition to "completed" so
	// a client's poll loop can observe a non-terminal ("in_progress") state.
	// Default (absent/0) completes the batch immediately.
	batchDelayHeader = "X-Mockagents-Batch-Delay-Ms"
	// maxBatchDelayMs caps the simulated processing delay so a bad header value
	// can't park a batch in_progress effectively forever.
	maxBatchDelayMs = 600000 // 10 minutes
)

// --- wire types ---

// Batch is the OpenAI Batch object. Timestamp/file fields are pointers with
// omitempty so an unset stage (e.g. completed_at before completion) is absent
// from the wire rather than a misleading zero, matching how the SDK models them
// as Optional.
type Batch struct {
	ID               string             `json:"id"`
	Object           string             `json:"object"`
	Endpoint         string             `json:"endpoint"`
	Errors           any                `json:"errors"`
	InputFileID      string             `json:"input_file_id"`
	CompletionWindow string             `json:"completion_window"`
	Status           string             `json:"status"`
	OutputFileID     *string            `json:"output_file_id,omitempty"`
	ErrorFileID      *string            `json:"error_file_id,omitempty"`
	CreatedAt        int64              `json:"created_at"`
	InProgressAt     *int64             `json:"in_progress_at,omitempty"`
	ExpiresAt        *int64             `json:"expires_at,omitempty"`
	CompletedAt      *int64             `json:"completed_at,omitempty"`
	FailedAt         *int64             `json:"failed_at,omitempty"`
	CancellingAt     *int64             `json:"cancelling_at,omitempty"`
	CancelledAt      *int64             `json:"cancelled_at,omitempty"`
	RequestCounts    BatchRequestCounts `json:"request_counts"`
	Metadata         map[string]any     `json:"metadata"`
}

// BatchRequestCounts is the per-batch tally the SDK surfaces while polling.
type BatchRequestCounts struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
}

// batchInputLine is one request line of the input JSONL.
type batchInputLine struct {
	CustomID string          `json:"custom_id"`
	Method   string          `json:"method"`
	URL      string          `json:"url"`
	Body     json.RawMessage `json:"body"`
}

// batchOutputLine is one line of the output (or error) JSONL. A dispatched
// request carries Response (with the sub-endpoint's status + body); a request
// that could not be dispatched at all (malformed line, bad endpoint) carries
// Error and lands in the error file instead.
type batchOutputLine struct {
	ID       string            `json:"id"`
	CustomID string            `json:"custom_id"`
	Response *batchOutputResp  `json:"response"`
	Error    *batchOutputError `json:"error"`
}

type batchOutputResp struct {
	StatusCode int             `json:"status_code"`
	RequestID  string          `json:"request_id"`
	Body       json.RawMessage `json:"body"`
}

type batchOutputError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// createBatchRequest is the POST /v1/batches body.
type createBatchRequest struct {
	InputFileID      string         `json:"input_file_id"`
	Endpoint         string         `json:"endpoint"`
	CompletionWindow string         `json:"completion_window"`
	Metadata         map[string]any `json:"metadata"`
}

// --- batch state + store ---

// batchState holds a created batch's immutable processed result plus the small
// mutable bit (cancellation). Status is NOT stored — it is derived on every
// read from the elapsed time vs. delay (and the cancelled flag) so a poll loop
// sees the batch progress without any background goroutine.
type batchState struct {
	mu           sync.Mutex
	base         Batch
	createdAt    time.Time
	delay        time.Duration
	counts       BatchRequestCounts
	outputFileID string
	errorFileID  string
	cancelled    bool
	cancelledAt  time.Time
}

// render projects the stored state into a wire Batch at time now.
func (st *batchState) render(now time.Time) Batch {
	st.mu.Lock()
	defer st.mu.Unlock()
	b := st.base
	ip := st.createdAt.Unix()
	b.InProgressAt = &ip

	if st.cancelled {
		b.Status = "cancelled"
		ca := st.cancelledAt.Unix()
		b.CancelledAt = &ca
		// A cancelled batch reports the work it had finished, but exposes no
		// output/error file (matching the real API's cancel semantics).
		b.RequestCounts = st.counts
		return b
	}

	if now.Sub(st.createdAt) < st.delay {
		b.Status = "in_progress"
		// While in flight the SDK sees the total but not yet the tallies.
		b.RequestCounts = BatchRequestCounts{Total: st.counts.Total}
		return b
	}

	b.Status = "completed"
	completedAt := st.createdAt.Add(st.delay).Unix()
	b.CompletedAt = &completedAt
	b.RequestCounts = st.counts
	if st.outputFileID != "" {
		out := st.outputFileID
		b.OutputFileID = &out
	}
	if st.errorFileID != "" {
		errID := st.errorFileID
		b.ErrorFileID = &errID
	}
	return b
}

// cancel marks the batch cancelled unless it has already completed. It reports
// the resulting status and whether the cancel was a no-op-because-terminal.
func (st *batchState) cancel(now time.Time) (status string, terminal bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.cancelled {
		return "cancelled", true
	}
	if now.Sub(st.createdAt) >= st.delay {
		return "completed", true
	}
	st.cancelled = true
	st.cancelledAt = now
	return "cancelled", false
}

// batchStore is the in-memory, per-tenant batch store (bounded FIFO), mirroring
// fileStore's isolation so one tenant never lists another's batches.
type batchStore struct {
	mu    sync.Mutex
	m     map[string]map[string]*batchState
	order map[string][]string
}

func newBatchStore() *batchStore {
	return &batchStore{
		m:     make(map[string]map[string]*batchState),
		order: make(map[string][]string),
	}
}

func (s *batchStore) put(tenant string, st *batchState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	byID := s.m[tenant]
	if byID == nil {
		byID = make(map[string]*batchState)
		s.m[tenant] = byID
	}
	byID[st.base.ID] = st
	s.order[tenant] = append(s.order[tenant], st.base.ID)
	for len(s.order[tenant]) > maxBatchesPerTenant {
		oldest := s.order[tenant][0]
		s.order[tenant] = s.order[tenant][1:]
		delete(byID, oldest)
	}
}

func (s *batchStore) get(tenant, id string) (*batchState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.m[tenant][id]
	return st, ok
}

func (s *batchStore) list(tenant string) []*batchState {
	s.mu.Lock()
	defer s.mu.Unlock()
	byID := s.m[tenant]
	out := make([]*batchState, 0, len(byID))
	for _, st := range byID {
		out = append(out, st)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].base.CreatedAt != out[j].base.CreatedAt {
			return out[i].base.CreatedAt > out[j].base.CreatedAt
		}
		return out[i].base.ID > out[j].base.ID
	})
	return out
}

// --- handler ---

// BatchesHandler serves the OpenAI Batch API. It shares the file store with
// FilesHandler (to read input and persist output/error files) and dispatches
// each input line back through the live per-request endpoints so a batched
// request is byte-for-byte the same as the synchronous one.
type BatchesHandler struct {
	files     *fileStore
	store     *batchStore
	endpoints map[string]http.HandlerFunc
	// SubrequestMiddleware, when set, wraps the endpoint handler for every
	// batched sub-request. The server sets it to the same quota and spend
	// metering a direct call passes through: sub-requests are dispatched in
	// process, below the HTTP middleware chain, so without it a batch ran up to
	// 50,000 engine calls that neither consumed a tenant's request quota nor
	// accrued its monthly spend.
	SubrequestMiddleware func(http.Handler) http.Handler
}

// NewBatchesHandler builds a BatchesHandler. endpoints maps a supported batch
// endpoint path (e.g. "/v1/chat/completions") to the handler that serves it;
// the batch processor replays each input line through the matching handler.
func NewBatchesHandler(files *fileStore, endpoints map[string]http.HandlerFunc) *BatchesHandler {
	return &BatchesHandler{
		files:     files,
		store:     newBatchStore(),
		endpoints: endpoints,
	}
}

// Name identifies this adapter in logs and diagnostics.
func (h *BatchesHandler) Name() string { return "openai-batches" }

// Routes returns the Batch routes mounted through the adapter Registry.
func (h *BatchesHandler) Routes() []Route {
	return []Route{
		{Pattern: "POST /v1/batches", Handler: h.HandleCreate},
		{Pattern: "GET /v1/batches", Handler: h.HandleList},
		{Pattern: "GET /v1/batches/{id}", Handler: h.HandleRetrieve},
		{Pattern: "POST /v1/batches/{id}/cancel", Handler: h.HandleCancel},
	}
}

// HandleCreate handles POST /v1/batches.
func (h *BatchesHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolOpenAIBatches)
	tenant := engine.TenantIDFromContext(r.Context())

	var req createBatchRequest
	if err := decodeJSONBody(r, &req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("invalid JSON: %s", err))
		return
	}
	defer r.Body.Close()

	if req.InputFileID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "input_file_id is required")
		return
	}
	if req.Endpoint == "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "endpoint is required")
		return
	}
	if _, ok := h.endpoints[req.Endpoint]; !ok {
		writeError(w, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("unsupported endpoint %q; supported endpoints are %s", req.Endpoint, h.supportedEndpoints()))
		return
	}
	completionWindow := req.CompletionWindow
	if completionWindow == "" {
		completionWindow = "24h"
	}

	inputFile, ok := h.files.get(tenant, req.InputFileID)
	if !ok {
		writeError(w, http.StatusNotFound, "invalid_request_error",
			fmt.Sprintf("No such File object: %s", req.InputFileID))
		return
	}

	delay, err := parseBatchDelay(r.Header.Get(batchDelayHeader))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	// Process the whole input file eagerly: deterministic and fast, so the work
	// is done before the create response returns and every later poll just
	// reads the stored result.
	outBytes, errBytes, counts, perr := h.process(tenant, req.Endpoint, inputFile.data)
	if perr != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", perr.Error())
		return
	}

	now := time.Now()
	// Only surface an output/error file when it actually has content: a batch
	// whose every line failed validation has no output, and the real API omits
	// output_file_id in that case (the error file carries the failures).
	outFileID := ""
	if len(outBytes) > 0 {
		outFileID = h.putGeneratedFile(tenant, "batch_output", "batch_"+generateID()+"_output.jsonl", outBytes)
	}
	errFileID := ""
	if len(errBytes) > 0 {
		errFileID = h.putGeneratedFile(tenant, "batch_output", "batch_"+generateID()+"_errors.jsonl", errBytes)
	}

	expires := now.Add(batchExpiryWindow).Unix()
	st := &batchState{
		base: Batch{
			ID:               "batch_" + generateID(),
			Object:           "batch",
			Endpoint:         req.Endpoint,
			Errors:           nil,
			InputFileID:      req.InputFileID,
			CompletionWindow: completionWindow,
			CreatedAt:        now.Unix(),
			ExpiresAt:        &expires,
			Metadata:         req.Metadata,
		},
		createdAt:    now,
		delay:        delay,
		counts:       counts,
		outputFileID: outFileID,
		errorFileID:  errFileID,
	}
	h.store.put(tenant, st)

	writeJSON(w, http.StatusOK, st.render(now))
}

// HandleRetrieve handles GET /v1/batches/{id}.
func (h *BatchesHandler) HandleRetrieve(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolOpenAIBatches)
	tenant := engine.TenantIDFromContext(r.Context())
	id := r.PathValue("id")

	st, ok := h.store.get(tenant, id)
	if !ok {
		writeBatchNotFound(w, id)
		return
	}
	writeJSON(w, http.StatusOK, st.render(time.Now()))
}

// HandleList handles GET /v1/batches.
func (h *BatchesHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolOpenAIBatches)
	tenant := engine.TenantIDFromContext(r.Context())

	now := time.Now()
	states := h.store.list(tenant)
	data := make([]Batch, 0, len(states))
	for _, st := range states {
		data = append(data, st.render(now))
	}
	resp := map[string]any{
		"object":   "list",
		"data":     data,
		"has_more": false,
	}
	if len(data) > 0 {
		resp["first_id"] = data[0].ID
		resp["last_id"] = data[len(data)-1].ID
	}
	writeJSON(w, http.StatusOK, resp)
}

// HandleCancel handles POST /v1/batches/{id}/cancel.
func (h *BatchesHandler) HandleCancel(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolOpenAIBatches)
	tenant := engine.TenantIDFromContext(r.Context())
	id := r.PathValue("id")

	st, ok := h.store.get(tenant, id)
	if !ok {
		writeBatchNotFound(w, id)
		return
	}
	now := time.Now()
	if _, terminal := st.cancel(now); terminal {
		writeError(w, http.StatusConflict, "invalid_request_error",
			fmt.Sprintf("Cannot cancel a batch with status %q", st.render(now).Status))
		return
	}
	writeJSON(w, http.StatusOK, st.render(now))
}

// --- processing ---

// process replays every line of input through the batch's endpoint, returning
// the output JSONL, the error JSONL (empty if none), the tallies, and a
// hard error only when the whole batch is invalid (empty / too many requests).
func (h *BatchesHandler) process(tenant, endpoint string, input []byte) (out, errOut []byte, counts BatchRequestCounts, err error) {
	lines := splitJSONLines(input)
	if len(lines) == 0 {
		return nil, nil, counts, errors.New("input file has no requests")
	}
	if len(lines) > maxBatchRequests {
		return nil, nil, counts, fmt.Errorf("input file has %d requests, which exceeds the limit of %d", len(lines), maxBatchRequests)
	}

	var outBuf, errBuf bytes.Buffer
	seen := make(map[string]bool, len(lines))

	for _, raw := range lines {
		counts.Total++
		var in batchInputLine
		if jerr := json.Unmarshal(raw, &in); jerr != nil {
			writeJSONL(&errBuf, batchOutputLine{
				ID:    "batch_req_" + generateID(),
				Error: &batchOutputError{Code: "invalid_json", Message: "line is not valid JSON"},
			})
			counts.Failed++
			continue
		}
		reason, ok := validateInputLine(in, endpoint, seen)
		// Reserve the custom_id as soon as it is seen (even on an otherwise
		// invalid line) so a later line can't reuse it — the real API dedups
		// custom_id regardless of a line's other validity.
		if in.CustomID != "" {
			seen[in.CustomID] = true
		}
		if !ok {
			writeJSONL(&errBuf, batchOutputLine{
				ID:       "batch_req_" + generateID(),
				CustomID: in.CustomID,
				Error:    &batchOutputError{Code: "invalid_request", Message: reason},
			})
			counts.Failed++
			continue
		}

		status, body := h.dispatch(tenant, endpoint, in.Body)
		writeJSONL(&outBuf, batchOutputLine{
			ID:       "batch_req_" + generateID(),
			CustomID: in.CustomID,
			Response: &batchOutputResp{
				StatusCode: status,
				RequestID:  "req_" + generateID(),
				Body:       body,
			},
		})
		if status/100 == 2 {
			counts.Completed++
		} else {
			counts.Failed++
		}
	}

	return outBuf.Bytes(), errBuf.Bytes(), counts, nil
}

// validateInputLine checks one parsed line. It returns a human reason and false
// when the line must be rejected into the error file. seen holds the custom_ids
// already accepted in this batch (duplicate detection).
func validateInputLine(in batchInputLine, endpoint string, seen map[string]bool) (reason string, ok bool) {
	if in.CustomID == "" {
		return "missing custom_id", false
	}
	if seen[in.CustomID] {
		return fmt.Sprintf("duplicate custom_id %q", in.CustomID), false
	}
	if in.Method != "" && !strings.EqualFold(in.Method, http.MethodPost) {
		return fmt.Sprintf("method must be POST, got %q", in.Method), false
	}
	if in.URL != endpoint {
		return fmt.Sprintf("request url %q must match the batch endpoint %q", in.URL, endpoint), false
	}
	if len(bytes.TrimSpace(in.Body)) == 0 {
		return "request body is required", false
	}
	return "", true
}

// dispatch replays one request body through the live endpoint handler and
// returns its HTTP status and (whitespace-trimmed) JSON body. Routing through
// the real handler guarantees a batched request is identical to the
// synchronous one. The sub-request runs on a detached context carrying only the
// tenant (so a client disconnect mid-create can't abort the batch) and a fresh
// RequestMeta (so it never clobbers the batch's own log annotation).
func (h *BatchesHandler) dispatch(tenant, endpoint string, body json.RawMessage) (int, json.RawMessage) {
	handler := h.endpoints[endpoint]
	// Batched requests can never stream: an SSE response would write event text
	// into the captured body and break the output JSONL framing (and the real
	// Batch API rejects streaming too). Force stream off before dispatching.
	body = disableStreaming(body)
	ctx := engine.WithTenantID(context.Background(), tenant)
	subReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		b, _ := json.Marshal(map[string]any{"error": map[string]string{
			"type": "server_error", "message": "could not build sub-request",
		}})
		return http.StatusInternalServerError, b
	}
	subReq.Header.Set("Content-Type", "application/json")
	subReq.ContentLength = int64(len(body))
	subReq, _ = engine.WithRequestMeta(subReq)

	rec := &batchResponseRecorder{}
	serveSubrequest(h.SubrequestMiddleware, handler, rec, subReq)

	status := rec.status
	if status == 0 {
		status = http.StatusOK
	}
	// Trim the trailing newline writeJSON appends so the body stays a single
	// JSONL token (an embedded newline would split the output line).
	trimmed := bytes.TrimSpace(rec.body.Bytes())
	out := make(json.RawMessage, len(trimmed))
	copy(out, trimmed)
	if len(out) == 0 {
		out = json.RawMessage("null")
	}
	return status, out
}

// disableStreaming forces "stream":false on a request body so a batched line
// can never trigger an SSE response (which would corrupt the JSONL output). A
// body that isn't a JSON object is returned unchanged — the endpoint handler
// will reject it on its own.
func disableStreaming(body json.RawMessage) json.RawMessage {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil || m == nil {
		return body
	}
	if _, ok := m["stream"]; !ok {
		return body
	}
	m["stream"] = json.RawMessage("false")
	if out, err := json.Marshal(m); err == nil {
		return out
	}
	return body
}

// putGeneratedFile stores a batch-generated file and returns its id.
func (h *BatchesHandler) putGeneratedFile(tenant, purpose, filename string, data []byte) string {
	e := &fileEntry{
		ID:        "file-" + generateID(),
		Bytes:     int64(len(data)),
		CreatedAt: time.Now().Unix(),
		Filename:  filename,
		Purpose:   purpose,
		data:      data,
	}
	h.files.put(tenant, e)
	return e.ID
}

// supportedEndpoints renders the configured endpoint paths for an error message.
func (h *BatchesHandler) supportedEndpoints() string {
	eps := make([]string, 0, len(h.endpoints))
	for ep := range h.endpoints {
		eps = append(eps, ep)
	}
	sort.Strings(eps)
	return strings.Join(eps, ", ")
}

// --- helpers ---

// batchResponseRecorder is a minimal in-memory http.ResponseWriter used to
// capture a dispatched sub-request's status and body without going through the
// network or the server's outer middleware (httptest is test-only; this keeps
// the shipped binary free of it).
type batchResponseRecorder struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (rec *batchResponseRecorder) Header() http.Header {
	if rec.header == nil {
		rec.header = make(http.Header)
	}
	return rec.header
}

func (rec *batchResponseRecorder) Write(b []byte) (int, error) { return rec.body.Write(b) }

func (rec *batchResponseRecorder) WriteHeader(status int) {
	if rec.status == 0 {
		rec.status = status
	}
}

// splitJSONLines splits a JSONL payload into its non-blank lines, tolerating
// both \n and \r\n. Blank lines (common as a trailing newline) are skipped so
// they don't count as empty requests.
func splitJSONLines(data []byte) [][]byte {
	var out [][]byte
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		out = append(out, line)
	}
	return out
}

// writeJSONL marshals v and appends it as one newline-terminated JSONL line.
func writeJSONL(buf *bytes.Buffer, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	buf.Write(b)
	buf.WriteByte('\n')
}

// parseBatchDelay reads the optional simulated-processing-delay header (ms),
// clamped to [0, maxBatchDelayMs]. An empty header means no delay.
func parseBatchDelay(h string) (time.Duration, error) {
	if h == "" {
		return 0, nil
	}
	ms, err := strconv.Atoi(h)
	if err != nil || ms < 0 {
		return 0, fmt.Errorf("invalid %s header: must be a non-negative integer of milliseconds", batchDelayHeader)
	}
	if ms > maxBatchDelayMs {
		ms = maxBatchDelayMs
	}
	return time.Duration(ms) * time.Millisecond, nil
}

// writeBatchNotFound writes the OpenAI 404 envelope for an unknown batch id.
func writeBatchNotFound(w http.ResponseWriter, id string) {
	writeError(w, http.StatusNotFound, "invalid_request_error", fmt.Sprintf("No such Batch object: %s", id))
}

// serveSubrequest runs one batched sub-request through handler, wrapped in mw
// when one is configured.
func serveSubrequest(mw func(http.Handler) http.Handler, handler http.HandlerFunc, w http.ResponseWriter, r *http.Request) {
	if mw == nil {
		handler(w, r)
		return
	}
	mw(handler).ServeHTTP(w, r)
}
