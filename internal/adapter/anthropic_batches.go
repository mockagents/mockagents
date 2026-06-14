package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
)

// ProtocolAnthropicBatches is the wire-protocol label recorded for the Anthropic
// Message Batches surface (POST /v1/messages/batches + retrieve/list/cancel/
// delete/results). It is the asynchronous sibling of /v1/messages (A-08).
//
// Unlike OpenAI's file-driven Batch API, Anthropic batches carry their requests
// INLINE in the create body ({"requests":[{custom_id, params}, ...]}) and expose
// results from a dedicated GET .../results endpoint as JSONL — there is no Files
// prerequisite. The mock processes the whole batch eagerly and deterministically
// at create time (replaying each request's params through the live /v1/messages
// handler so a batched request is byte-for-byte the same as the synchronous one)
// and derives processing_status from elapsed time vs. an optional delay so a poll
// loop can observe the in_progress -> ended lifecycle without any goroutine.
const ProtocolAnthropicBatches = "anthropic-batches"

const (
	// maxAnthropicBatchRequests bounds the inline requests in one batch, matching
	// Anthropic's per-batch cap and keeping the eager in-memory processing bounded.
	maxAnthropicBatchRequests = 100000
	// maxAnthropicBatchesPerTenant bounds stored batches per tenant (FIFO eviction).
	maxAnthropicBatchesPerTenant = 256
	// anthropicBatchExpiryWindow is the fixed expires_at offset (Anthropic batch
	// results are retained 29 days). The mock never actually expires them; this
	// only populates the wire field.
	anthropicBatchExpiryWindow = 29 * 24 * time.Hour
)

// --- wire types ---

// AnthropicBatch is the Message Batch object. The nullable timestamp/url fields
// are pointers WITHOUT omitempty so they serialize as explicit `null` until set,
// matching the real API (which always includes the keys) and how the SDK models
// them as Optional.
type AnthropicBatch struct {
	ID                string                      `json:"id"`
	Type              string                      `json:"type"` // always "message_batch"
	ProcessingStatus  string                      `json:"processing_status"`
	RequestCounts     AnthropicBatchRequestCounts `json:"request_counts"`
	EndedAt           *string                     `json:"ended_at"`
	CreatedAt         string                      `json:"created_at"`
	ExpiresAt         string                      `json:"expires_at"`
	ArchivedAt        *string                     `json:"archived_at"`
	CancelInitiatedAt *string                     `json:"cancel_initiated_at"`
	ResultsURL        *string                     `json:"results_url"`
}

// AnthropicBatchRequestCounts is the per-batch tally the SDK surfaces while
// polling. The five buckets are mutually exclusive and sum to the request total.
type AnthropicBatchRequestCounts struct {
	Processing int `json:"processing"`
	Succeeded  int `json:"succeeded"`
	Errored    int `json:"errored"`
	Canceled   int `json:"canceled"`
	Expired    int `json:"expired"`
}

// createAnthropicBatchRequest is the POST /v1/messages/batches body.
type createAnthropicBatchRequest struct {
	Requests []anthropicBatchRequestItem `json:"requests"`
}

// anthropicBatchRequestItem is one inline request: a unique custom_id plus the
// /v1/messages params (an opaque blob replayed verbatim through that handler).
type anthropicBatchRequestItem struct {
	CustomID string          `json:"custom_id"`
	Params   json.RawMessage `json:"params"`
}

// anthropicBatchResultLine is one line of the results JSONL.
type anthropicBatchResultLine struct {
	CustomID string               `json:"custom_id"`
	Result   anthropicBatchResult `json:"result"`
}

// anthropicBatchResult is the discriminated result for one request. A succeeded
// request carries the /v1/messages Message; an errored one carries that
// endpoint's error envelope; canceled/expired carry neither.
type anthropicBatchResult struct {
	Type    string          `json:"type"` // succeeded | errored | canceled | expired
	Message json.RawMessage `json:"message,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

// --- batch state + store ---

// anthropicBatchState holds a created batch's immutable processed result plus
// the small mutable bit (cancellation). processing_status is NOT stored — it is
// derived on every read from elapsed time vs. delay (and the cancelled flag) so
// a poll loop sees the batch progress without any background goroutine.
type anthropicBatchState struct {
	mu              sync.Mutex
	base            AnthropicBatch
	createdAt       time.Time
	delay           time.Duration
	total           int
	succeeded       int
	errored         int
	results         []byte   // eager succeeded/errored JSONL
	customIDs       []string // request order, for synthesizing canceled results
	resultsURL      string
	cancelled       bool
	cancelInitiated time.Time
}

// render projects the stored state into a wire AnthropicBatch at time now.
func (st *anthropicBatchState) render(now time.Time) AnthropicBatch {
	st.mu.Lock()
	defer st.mu.Unlock()
	b := st.base
	ended := now.Sub(st.createdAt) >= st.delay

	if st.cancelled {
		ci := rfc3339(st.cancelInitiated)
		b.CancelInitiatedAt = &ci
		if !ended {
			// The cancel was accepted but the (simulated) processing window has
			// not elapsed yet: the SDK sees a transient "canceling" state.
			b.ProcessingStatus = "canceling"
			b.RequestCounts = AnthropicBatchRequestCounts{Processing: st.total}
			return b
		}
		// Cancelled and the window has elapsed: terminal, every request canceled.
		b.ProcessingStatus = "ended"
		ea := rfc3339(st.createdAt.Add(st.delay))
		b.EndedAt = &ea
		b.RequestCounts = AnthropicBatchRequestCounts{Canceled: st.total}
		url := st.resultsURL
		b.ResultsURL = &url
		return b
	}

	if !ended {
		b.ProcessingStatus = "in_progress"
		// While in flight the SDK sees everything still processing.
		b.RequestCounts = AnthropicBatchRequestCounts{Processing: st.total}
		return b
	}

	b.ProcessingStatus = "ended"
	ea := rfc3339(st.createdAt.Add(st.delay))
	b.EndedAt = &ea
	b.RequestCounts = AnthropicBatchRequestCounts{Succeeded: st.succeeded, Errored: st.errored}
	url := st.resultsURL
	b.ResultsURL = &url
	return b
}

// resultsBytes returns the results JSONL and whether the batch has ended (and so
// the results are available). A cancelled batch reports every request canceled.
func (st *anthropicBatchState) resultsBytes(now time.Time) (data []byte, ended bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if now.Sub(st.createdAt) < st.delay {
		return nil, false
	}
	if st.cancelled {
		var buf bytes.Buffer
		for _, cid := range st.customIDs {
			writeJSONL(&buf, anthropicBatchResultLine{
				CustomID: cid,
				Result:   anthropicBatchResult{Type: "canceled"},
			})
		}
		return buf.Bytes(), true
	}
	return st.results, true
}

// cancel marks the batch cancelled unless it has already ended. It reports
// whether the cancel was a no-op because the batch was already terminal.
func (st *anthropicBatchState) cancel(now time.Time) (terminal bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.cancelled {
		return false
	}
	if now.Sub(st.createdAt) >= st.delay {
		return true
	}
	st.cancelled = true
	st.cancelInitiated = now
	return false
}

// hasEnded reports whether the batch is in a terminal state at time now (used to
// gate deletion, which the real API only allows once processing has finished).
func (st *anthropicBatchState) hasEnded(now time.Time) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	// A cancelled batch is only terminal once its processing window has elapsed
	// (it is "canceling" until then) — matching render's status derivation.
	return now.Sub(st.createdAt) >= st.delay
}

// anthropicBatchStore is the in-memory, per-tenant batch store (bounded FIFO),
// mirroring batchStore's isolation so one tenant never lists another's batches.
type anthropicBatchStore struct {
	mu    sync.Mutex
	m     map[string]map[string]*anthropicBatchState
	order map[string][]string
}

func newAnthropicBatchStore() *anthropicBatchStore {
	return &anthropicBatchStore{
		m:     make(map[string]map[string]*anthropicBatchState),
		order: make(map[string][]string),
	}
}

func (s *anthropicBatchStore) put(tenant string, st *anthropicBatchState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	byID := s.m[tenant]
	if byID == nil {
		byID = make(map[string]*anthropicBatchState)
		s.m[tenant] = byID
	}
	byID[st.base.ID] = st
	s.order[tenant] = append(s.order[tenant], st.base.ID)
	for len(s.order[tenant]) > maxAnthropicBatchesPerTenant {
		oldest := s.order[tenant][0]
		s.order[tenant] = s.order[tenant][1:]
		delete(byID, oldest)
	}
}

func (s *anthropicBatchStore) get(tenant, id string) (*anthropicBatchState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.m[tenant][id]
	return st, ok
}

func (s *anthropicBatchStore) delete(tenant, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	byID := s.m[tenant]
	if _, ok := byID[id]; !ok {
		return false
	}
	delete(byID, id)
	if order := s.order[tenant]; len(order) > 0 {
		for i, oid := range order {
			if oid == id {
				s.order[tenant] = append(order[:i:i], order[i+1:]...)
				break
			}
		}
	}
	return true
}

func (s *anthropicBatchStore) list(tenant string) []*anthropicBatchState {
	s.mu.Lock()
	defer s.mu.Unlock()
	byID := s.m[tenant]
	out := make([]*anthropicBatchState, 0, len(byID))
	for _, st := range byID {
		out = append(out, st)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].createdAt.Equal(out[j].createdAt) {
			return out[i].createdAt.After(out[j].createdAt)
		}
		return out[i].base.ID > out[j].base.ID
	})
	return out
}

// --- handler ---

// AnthropicBatchesHandler serves the Anthropic Message Batches API. It dispatches
// each inline request's params back through the live /v1/messages handler so a
// batched request is byte-for-byte the same as the synchronous one.
type AnthropicBatchesHandler struct {
	messages http.HandlerFunc
	store    *anthropicBatchStore
}

// NewAnthropicBatchesHandler builds a handler that replays batched requests
// through messages (the live POST /v1/messages handler).
func NewAnthropicBatchesHandler(messages http.HandlerFunc) *AnthropicBatchesHandler {
	return &AnthropicBatchesHandler{messages: messages, store: newAnthropicBatchStore()}
}

// Name identifies this adapter in logs and diagnostics.
func (h *AnthropicBatchesHandler) Name() string { return "anthropic-batches" }

// Routes returns the Message Batches routes mounted through the adapter Registry.
func (h *AnthropicBatchesHandler) Routes() []Route {
	return []Route{
		{Pattern: "POST /v1/messages/batches", Handler: h.HandleCreate},
		{Pattern: "GET /v1/messages/batches", Handler: h.HandleList},
		{Pattern: "GET /v1/messages/batches/{id}", Handler: h.HandleRetrieve},
		{Pattern: "POST /v1/messages/batches/{id}/cancel", Handler: h.HandleCancel},
		{Pattern: "DELETE /v1/messages/batches/{id}", Handler: h.HandleDelete},
		{Pattern: "GET /v1/messages/batches/{id}/results", Handler: h.HandleResults},
	}
}

// HandleCreate handles POST /v1/messages/batches.
func (h *AnthropicBatchesHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolAnthropicBatches)
	tenant := engine.TenantIDFromContext(r.Context())

	var req createAnthropicBatchRequest
	if err := decodeJSONBody(r, &req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeAnthropicError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body too large")
			return
		}
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("invalid JSON: %s", err))
		return
	}
	defer r.Body.Close()

	// Anthropic validates the whole batch up front: a structural problem
	// (no requests, too many, missing/duplicate custom_id, missing params)
	// rejects the entire create rather than producing per-request errors.
	if reason, ok := validateAnthropicBatch(req.Requests); !ok {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", reason)
		return
	}

	delay, err := parseBatchDelay(r.Header.Get(batchDelayHeader))
	if err != nil {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	// Process the whole batch eagerly: deterministic and fast, so the work is
	// done before the create response returns and every later poll/results read
	// just serves the stored result.
	results, succeeded, errored := h.process(tenant, req.Requests)

	now := time.Now()
	id := "msgbatch_" + generateID()
	customIDs := make([]string, len(req.Requests))
	for i, it := range req.Requests {
		customIDs[i] = it.CustomID
	}
	st := &anthropicBatchState{
		base: AnthropicBatch{
			ID:        id,
			Type:      "message_batch",
			CreatedAt: rfc3339(now),
			ExpiresAt: rfc3339(now.Add(anthropicBatchExpiryWindow)),
		},
		createdAt:  now,
		delay:      delay,
		total:      len(req.Requests),
		succeeded:  succeeded,
		errored:    errored,
		results:    results,
		customIDs:  customIDs,
		resultsURL: batchResultsURL(r, id),
	}
	h.store.put(tenant, st)

	writeJSON(w, http.StatusOK, st.render(now))
}

// HandleRetrieve handles GET /v1/messages/batches/{id}.
func (h *AnthropicBatchesHandler) HandleRetrieve(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolAnthropicBatches)
	tenant := engine.TenantIDFromContext(r.Context())
	id := r.PathValue("id")

	st, ok := h.store.get(tenant, id)
	if !ok {
		writeAnthropicBatchNotFound(w, id)
		return
	}
	writeJSON(w, http.StatusOK, st.render(time.Now()))
}

// HandleList handles GET /v1/messages/batches.
func (h *AnthropicBatchesHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolAnthropicBatches)
	tenant := engine.TenantIDFromContext(r.Context())

	now := time.Now()
	states := h.store.list(tenant)
	data := make([]AnthropicBatch, 0, len(states))
	for _, st := range states {
		data = append(data, st.render(now))
	}
	resp := map[string]any{
		"data":     data,
		"has_more": false,
		"first_id": nil,
		"last_id":  nil,
	}
	if len(data) > 0 {
		resp["first_id"] = data[0].ID
		resp["last_id"] = data[len(data)-1].ID
	}
	writeJSON(w, http.StatusOK, resp)
}

// HandleCancel handles POST /v1/messages/batches/{id}/cancel.
func (h *AnthropicBatchesHandler) HandleCancel(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolAnthropicBatches)
	tenant := engine.TenantIDFromContext(r.Context())
	id := r.PathValue("id")

	st, ok := h.store.get(tenant, id)
	if !ok {
		writeAnthropicBatchNotFound(w, id)
		return
	}
	now := time.Now()
	// Cancel is idempotent and always returns the (possibly already-terminal)
	// batch — the real API never 4xxs a cancel of a finished batch.
	st.cancel(now)
	writeJSON(w, http.StatusOK, st.render(now))
}

// HandleDelete handles DELETE /v1/messages/batches/{id}. A batch can only be
// deleted once it has finished processing (the real API requires a cancel first
// for an in-flight batch).
func (h *AnthropicBatchesHandler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolAnthropicBatches)
	tenant := engine.TenantIDFromContext(r.Context())
	id := r.PathValue("id")

	st, ok := h.store.get(tenant, id)
	if !ok {
		writeAnthropicBatchNotFound(w, id)
		return
	}
	if !st.hasEnded(time.Now()) {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("Message Batch %s cannot be deleted while it is still processing; cancel it first", id))
		return
	}
	h.store.delete(tenant, id)
	writeJSON(w, http.StatusOK, map[string]any{
		"id":   id,
		"type": "message_batch_deleted",
	})
}

// HandleResults handles GET /v1/messages/batches/{id}/results, streaming the
// per-request results as JSONL once the batch has ended.
func (h *AnthropicBatchesHandler) HandleResults(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolAnthropicBatches)
	tenant := engine.TenantIDFromContext(r.Context())
	id := r.PathValue("id")

	st, ok := h.store.get(tenant, id)
	if !ok {
		writeAnthropicBatchNotFound(w, id)
		return
	}
	data, ended := st.resultsBytes(time.Now())
	if !ended {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("Results for Message Batch %s are not yet available; the batch is still processing", id))
		return
	}
	// Anthropic serves results as newline-delimited JSON (.jsonl).
	w.Header().Set("Content-Type", "application/x-jsonl")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// --- processing ---

// process replays every request's params through /v1/messages, returning the
// results JSONL and the succeeded/errored tallies.
func (h *AnthropicBatchesHandler) process(tenant string, requests []anthropicBatchRequestItem) (results []byte, succeeded, errored int) {
	var buf bytes.Buffer
	for _, item := range requests {
		status, body := h.dispatch(tenant, item.Params)
		line := anthropicBatchResultLine{CustomID: item.CustomID}
		if status/100 == 2 {
			line.Result = anthropicBatchResult{Type: "succeeded", Message: body}
			succeeded++
		} else {
			line.Result = anthropicBatchResult{Type: "errored", Error: body}
			errored++
		}
		writeJSONL(&buf, line)
	}
	return buf.Bytes(), succeeded, errored
}

// dispatch replays one request's params through the live /v1/messages handler
// and returns its HTTP status and (whitespace-trimmed) JSON body. Routing through
// the real handler guarantees a batched request is identical to the synchronous
// one. The sub-request runs on a detached context carrying only the tenant (so a
// client disconnect mid-create can't abort the batch) and a fresh RequestMeta (so
// it never clobbers the batch's own log annotation). A placeholder API key is set
// because /v1/messages enforces an inline key — the batch itself is already
// authorized, so its replayed requests inherit that authorization.
func (h *AnthropicBatchesHandler) dispatch(tenant string, params json.RawMessage) (int, json.RawMessage) {
	// Batched requests can never stream: an SSE response would write event text
	// into the captured body and break the JSONL framing (and the real Batch API
	// rejects streaming too). Force stream off before dispatching.
	body := disableStreaming(params)
	ctx := engine.WithTenantID(context.Background(), tenant)
	subReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "/v1/messages", bytes.NewReader(body))
	if err != nil {
		b, _ := json.Marshal(anthropicErrorEnvelope{
			Type:  "error",
			Error: anthropicErrorBody{Type: "api_error", Message: "could not build sub-request"},
		})
		return http.StatusInternalServerError, b
	}
	subReq.Header.Set("Content-Type", "application/json")
	subReq.Header.Set("X-Api-Key", "mockagents-batch")
	subReq.ContentLength = int64(len(body))
	subReq, _ = engine.WithRequestMeta(subReq)

	rec := &batchResponseRecorder{}
	h.messages(rec, subReq)

	status := rec.status
	if status == 0 {
		status = http.StatusOK
	}
	// Trim the trailing newline writeJSON appends so the body stays a single
	// JSONL token (an embedded newline would split the result line).
	trimmed := bytes.TrimSpace(rec.body.Bytes())
	out := make(json.RawMessage, len(trimmed))
	copy(out, trimmed)
	if len(out) == 0 {
		out = json.RawMessage("null")
	}
	return status, out
}

// --- helpers ---

// validateAnthropicBatch checks the inline request set up front. It returns a
// human reason and false when the whole create must be rejected.
func validateAnthropicBatch(requests []anthropicBatchRequestItem) (reason string, ok bool) {
	if len(requests) == 0 {
		return "requests is required and must contain at least one request", false
	}
	if len(requests) > maxAnthropicBatchRequests {
		return fmt.Sprintf("batch has %d requests, which exceeds the limit of %d", len(requests), maxAnthropicBatchRequests), false
	}
	seen := make(map[string]bool, len(requests))
	for _, it := range requests {
		if it.CustomID == "" {
			return "each request requires a custom_id", false
		}
		if seen[it.CustomID] {
			return fmt.Sprintf("duplicate custom_id %q; custom_id values must be unique within a batch", it.CustomID), false
		}
		seen[it.CustomID] = true
		if trimmed := bytes.TrimSpace(it.Params); len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
			return fmt.Sprintf("request %q is missing params", it.CustomID), false
		}
	}
	return "", true
}

// batchResultsURL builds the absolute results URL the SDK fetches, derived from
// the create request so it points at this mock rather than the real API host.
func batchResultsURL(r *http.Request, id string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	return fmt.Sprintf("%s://%s/v1/messages/batches/%s/results", scheme, r.Host, id)
}

// rfc3339 formats t as the UTC RFC3339 timestamp string Anthropic uses for its
// created_at/ended_at/expires_at fields (OpenAI batches use unix seconds).
func rfc3339(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// writeAnthropicBatchNotFound writes the Anthropic 404 envelope for an unknown
// batch id.
func writeAnthropicBatchNotFound(w http.ResponseWriter, id string) {
	writeAnthropicError(w, http.StatusNotFound, "not_found_error", fmt.Sprintf("Message Batch %s not found", id))
}
