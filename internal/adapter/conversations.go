package adapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
)

// ProtocolOpenAIConversations is the wire-protocol label recorded for the OpenAI
// Conversations API surface (NF-02). Conversations are the stateful companion to
// the Responses API and the replacement for Assistants Threads (the Assistants
// API sunsets 2026-08-26): a client creates a conversation, then drives a
// multi-turn loop by passing its id on each /v1/responses call instead of
// chaining previous_response_id. The mock stores the conversation's Items and
// replays them as prior turns, and appends each turn's new input + the assistant
// output back onto the conversation (independent of the Responses `store` flag,
// which only governs retention of the standalone Response object).
const ProtocolOpenAIConversations = "openai-conversations"

const (
	// maxConversationsPerTenant bounds stored conversations per tenant (FIFO),
	// mirroring the files/batches stores so a long-lived mock can't grow without
	// bound.
	maxConversationsPerTenant = 256
	// maxConversationItems caps the items in one conversation; appends past the
	// cap drop the oldest (a mock conversation is for testing, not archival).
	maxConversationItems = 4096
)

// --- wire types ---

// Conversation is the OpenAI Conversation object returned by the create/retrieve
// routes. Items are not embedded here (the real API serves them via the
// /items sub-resource).
type Conversation struct {
	ID        string         `json:"id"`
	Object    string         `json:"object"` // always "conversation"
	CreatedAt int64          `json:"created_at"`
	Metadata  map[string]any `json:"metadata"`
}

// conversationItem is one stored item. The Responses Items type is a wide union;
// the mock models the three kinds that actually drive a multi-turn loop —
// "message", "function_call", and "function_call_output" — storing them
// faithfully so the items endpoints round-trip, and flattening them to engine
// messages for the Responses replay. Fields are omitempty so each kind only
// serializes the fields it uses.
type conversationItem struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Object    string          `json:"object,omitempty"` // "conversation.item" on read
	CreatedAt int64           `json:"created_at,omitempty"`
	Role      string          `json:"role,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	Status    string          `json:"status,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments string          `json:"arguments,omitempty"`
	Output    json.RawMessage `json:"output,omitempty"`
}

// createConversationRequest is the POST /v1/conversations body. Both fields are
// optional: a bare {} creates an empty conversation.
type createConversationRequest struct {
	Metadata map[string]any     `json:"metadata,omitempty"`
	Items    []conversationItem `json:"items,omitempty"`
}

// updateConversationRequest is the POST /v1/conversations/{id} body. Metadata is
// a pointer so an absent field (or an empty body) is distinguishable from an
// explicit value: only a present object replaces the stored metadata, so an
// empty-body update is a no-op rather than wiping it (review finding F-103).
type updateConversationRequest struct {
	Metadata *map[string]any `json:"metadata,omitempty"`
}

// createItemsRequest is the POST /v1/conversations/{id}/items body.
type createItemsRequest struct {
	Items []conversationItem `json:"items"`
}

// --- conversation state + store ---

// conversationState is one conversation's mutable contents (its metadata + the
// ordered item list). Guarded by its own mutex so concurrent Responses turns and
// items-API calls on the same conversation are serialized.
type conversationState struct {
	mu        sync.Mutex
	id        string
	createdAt int64
	metadata  map[string]any
	items     []conversationItem
}

func (st *conversationState) wire() Conversation {
	st.mu.Lock()
	defer st.mu.Unlock()
	md := st.metadata
	if md == nil {
		md = map[string]any{}
	}
	return Conversation{ID: st.id, Object: "conversation", CreatedAt: st.createdAt, Metadata: md}
}

// appendItems adds items (assigning ids/objects), evicting the oldest past the
// cap. It is used both by the items API and by the Responses turn-append.
func (st *conversationState) appendItems(items []conversationItem) []conversationItem {
	st.mu.Lock()
	defer st.mu.Unlock()
	added := make([]conversationItem, 0, len(items))
	for _, it := range items {
		it = normalizeItem(it)
		st.items = append(st.items, it)
		added = append(added, it)
	}
	if len(st.items) > maxConversationItems {
		st.items = st.items[len(st.items)-maxConversationItems:]
	}
	return added
}

func (st *conversationState) listItems() []conversationItem {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make([]conversationItem, len(st.items))
	copy(out, st.items)
	return out
}

func (st *conversationState) getItem(itemID string) (conversationItem, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, it := range st.items {
		if it.ID == itemID {
			return it, true
		}
	}
	return conversationItem{}, false
}

func (st *conversationState) deleteItem(itemID string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i, it := range st.items {
		if it.ID == itemID {
			st.items = append(st.items[:i:i], st.items[i+1:]...)
			return true
		}
	}
	return false
}

func (st *conversationState) setMetadata(md map[string]any) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.metadata = md
}

// messages flattens the conversation's items into engine messages for replay as
// prior turns, reusing the same item→message mapping as parseResponsesInput.
func (st *conversationState) messages() []engine.RequestMessage {
	st.mu.Lock()
	defer st.mu.Unlock()
	return conversationItemsToMessages(st.items)
}

// conversationStore is the in-memory, per-tenant conversation store (bounded
// FIFO), mirroring fileStore/batchStore isolation so one tenant never sees
// another's conversations. Shared between ConversationsHandler and
// ResponsesHandler (the Responses `conversation` param reads + appends here).
type conversationStore struct {
	mu    sync.Mutex
	m     map[string]map[string]*conversationState
	order map[string][]string
}

func newConversationStore() *conversationStore {
	return &conversationStore{
		m:     make(map[string]map[string]*conversationState),
		order: make(map[string][]string),
	}
}

func (s *conversationStore) put(tenant string, st *conversationState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	byID := s.m[tenant]
	if byID == nil {
		byID = make(map[string]*conversationState)
		s.m[tenant] = byID
	}
	byID[st.id] = st
	s.order[tenant] = append(s.order[tenant], st.id)
	for len(s.order[tenant]) > maxConversationsPerTenant {
		oldest := s.order[tenant][0]
		s.order[tenant] = s.order[tenant][1:]
		delete(byID, oldest)
	}
}

func (s *conversationStore) get(tenant, id string) (*conversationState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.m[tenant][id]
	return st, ok
}

func (s *conversationStore) delete(tenant, id string) bool {
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

// --- handler ---

// ConversationsHandler serves the OpenAI Conversations API. It shares its store
// with ResponsesHandler so a Responses request carrying a `conversation` id can
// replay and extend the same conversation.
type ConversationsHandler struct {
	store *conversationStore
}

// NewConversationsHandler builds a handler over store (injected so ResponsesHandler
// can share it).
func NewConversationsHandler(store *conversationStore) *ConversationsHandler {
	return &ConversationsHandler{store: store}
}

// Name identifies this adapter in logs and diagnostics.
func (h *ConversationsHandler) Name() string { return "openai-conversations" }

// Routes returns the Conversations routes mounted through the adapter Registry.
func (h *ConversationsHandler) Routes() []Route {
	return []Route{
		{Pattern: "POST /v1/conversations", Handler: h.HandleCreate},
		{Pattern: "GET /v1/conversations/{id}", Handler: h.HandleRetrieve},
		{Pattern: "POST /v1/conversations/{id}", Handler: h.HandleUpdate},
		{Pattern: "DELETE /v1/conversations/{id}", Handler: h.HandleDelete},
		{Pattern: "GET /v1/conversations/{id}/items", Handler: h.HandleListItems},
		{Pattern: "POST /v1/conversations/{id}/items", Handler: h.HandleCreateItems},
		{Pattern: "GET /v1/conversations/{id}/items/{item_id}", Handler: h.HandleGetItem},
		{Pattern: "DELETE /v1/conversations/{id}/items/{item_id}", Handler: h.HandleDeleteItem},
	}
}

// HandleCreate handles POST /v1/conversations.
func (h *ConversationsHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolOpenAIConversations)
	tenant := engine.TenantIDFromContext(r.Context())

	var req createConversationRequest
	// An empty body is valid (create an empty conversation), so tolerate it.
	if err := decodeJSONBody(r, &req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body too large")
			return
		}
		if !isEmptyBodyDecodeErr(err) {
			writeError(w, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("invalid JSON: %s", err))
			return
		}
	}
	defer r.Body.Close()

	st := &conversationState{
		id:        "conv_" + generateID(),
		createdAt: time.Now().Unix(),
		metadata:  req.Metadata,
	}
	if len(req.Items) > 0 {
		st.appendItems(req.Items)
	}
	h.store.put(tenant, st)
	writeJSON(w, http.StatusOK, st.wire())
}

// HandleRetrieve handles GET /v1/conversations/{id}.
func (h *ConversationsHandler) HandleRetrieve(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolOpenAIConversations)
	st, ok := h.store.get(engine.TenantIDFromContext(r.Context()), r.PathValue("id"))
	if !ok {
		writeConversationNotFound(w, r.PathValue("id"))
		return
	}
	writeJSON(w, http.StatusOK, st.wire())
}

// HandleUpdate handles POST /v1/conversations/{id} (metadata replace).
func (h *ConversationsHandler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolOpenAIConversations)
	st, ok := h.store.get(engine.TenantIDFromContext(r.Context()), r.PathValue("id"))
	if !ok {
		writeConversationNotFound(w, r.PathValue("id"))
		return
	}
	var req updateConversationRequest
	if err := decodeJSONBody(r, &req); err != nil && !isEmptyBodyDecodeErr(err) {
		writeError(w, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("invalid JSON: %s", err))
		return
	}
	defer r.Body.Close()
	if req.Metadata != nil {
		st.setMetadata(*req.Metadata)
	}
	writeJSON(w, http.StatusOK, st.wire())
}

// HandleDelete handles DELETE /v1/conversations/{id}.
func (h *ConversationsHandler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolOpenAIConversations)
	id := r.PathValue("id")
	if !h.store.delete(engine.TenantIDFromContext(r.Context()), id) {
		writeConversationNotFound(w, id)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      id,
		"object":  "conversation.deleted",
		"deleted": true,
	})
}

// HandleListItems handles GET /v1/conversations/{id}/items.
func (h *ConversationsHandler) HandleListItems(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolOpenAIConversations)
	st, ok := h.store.get(engine.TenantIDFromContext(r.Context()), r.PathValue("id"))
	if !ok {
		writeConversationNotFound(w, r.PathValue("id"))
		return
	}

	// List query params (matching the real API): order (default "desc"), an
	// `after` item-id cursor, and limit (default 20, 1..100). The store keeps
	// items in insertion (chronological) order.
	items := st.listItems()
	if q := r.URL.Query().Get("order"); q != "asc" {
		reverseItems(items) // default + "desc" → newest first
	}
	if after := r.URL.Query().Get("after"); after != "" {
		items = itemsAfter(items, after)
	}
	limit := parseListLimit(r.URL.Query().Get("limit"), 20)
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	writeJSON(w, http.StatusOK, itemListEnvelope(items, hasMore))
}

// HandleCreateItems handles POST /v1/conversations/{id}/items.
func (h *ConversationsHandler) HandleCreateItems(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolOpenAIConversations)
	st, ok := h.store.get(engine.TenantIDFromContext(r.Context()), r.PathValue("id"))
	if !ok {
		writeConversationNotFound(w, r.PathValue("id"))
		return
	}
	var req createItemsRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("invalid JSON: %s", err))
		return
	}
	defer r.Body.Close()
	if len(req.Items) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "items is required and must not be empty")
		return
	}
	added := st.appendItems(req.Items)
	writeJSON(w, http.StatusOK, itemListEnvelope(added, false))
}

// HandleGetItem handles GET /v1/conversations/{id}/items/{item_id}.
func (h *ConversationsHandler) HandleGetItem(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolOpenAIConversations)
	st, ok := h.store.get(engine.TenantIDFromContext(r.Context()), r.PathValue("id"))
	if !ok {
		writeConversationNotFound(w, r.PathValue("id"))
		return
	}
	it, ok := st.getItem(r.PathValue("item_id"))
	if !ok {
		writeError(w, http.StatusNotFound, "invalid_request_error",
			fmt.Sprintf("No such item: '%s'", r.PathValue("item_id")))
		return
	}
	writeJSON(w, http.StatusOK, it)
}

// HandleDeleteItem handles DELETE /v1/conversations/{id}/items/{item_id}. The
// real API returns the updated conversation object.
func (h *ConversationsHandler) HandleDeleteItem(w http.ResponseWriter, r *http.Request) {
	stampProtocol(r, ProtocolOpenAIConversations)
	st, ok := h.store.get(engine.TenantIDFromContext(r.Context()), r.PathValue("id"))
	if !ok {
		writeConversationNotFound(w, r.PathValue("id"))
		return
	}
	if !st.deleteItem(r.PathValue("item_id")) {
		writeError(w, http.StatusNotFound, "invalid_request_error",
			fmt.Sprintf("No such item: '%s'", r.PathValue("item_id")))
		return
	}
	writeJSON(w, http.StatusOK, st.wire())
}

// --- helpers ---

// itemListEnvelope wraps items in the standard OpenAI list shape.
func itemListEnvelope(items []conversationItem, hasMore bool) map[string]any {
	resp := map[string]any{
		"object":   "list",
		"data":     items,
		"has_more": hasMore,
	}
	if len(items) > 0 {
		resp["first_id"] = items[0].ID
		resp["last_id"] = items[len(items)-1].ID
	}
	return resp
}

// reverseItems reverses a slice in place (the store hands back a copy, so this
// never mutates stored state).
func reverseItems(items []conversationItem) {
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
}

// itemsAfter returns the items following the one whose id == afterID, in the
// given order. An unknown cursor is a lenient no-op (returns all).
func itemsAfter(items []conversationItem, afterID string) []conversationItem {
	for i, it := range items {
		if it.ID == afterID {
			return items[i+1:]
		}
	}
	return items
}

// parseListLimit parses the `limit` query param, defaulting and clamping to the
// real API's 1..100 range.
func parseListLimit(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return def
	}
	if n > 100 {
		return 100
	}
	return n
}

// normalizeItem fills the id/object/type/status defaults the read shape needs.
func normalizeItem(it conversationItem) conversationItem {
	if it.Type == "" {
		it.Type = "message"
	}
	if it.ID == "" {
		it.ID = itemIDPrefix(it.Type) + generateID()
	}
	it.Object = "conversation.item"
	if it.CreatedAt == 0 {
		it.CreatedAt = time.Now().Unix()
	}
	if it.Type == "message" && it.Role == "" {
		it.Role = "user"
	}
	if it.Status == "" {
		it.Status = "completed"
	}
	return it
}

func itemIDPrefix(itemType string) string {
	switch itemType {
	case "function_call":
		return "fc_"
	case "function_call_output":
		return "fco_"
	default:
		return "msg_"
	}
}

// conversationItemsToMessages flattens stored items into engine messages via the
// shared responsesItemToMessage mapping (responses.go), so a replayed
// conversation produces the exact same history as the same items sent inline to
// /v1/responses (review finding X-001 — single source of truth for the mapping).
func conversationItemsToMessages(items []conversationItem) []engine.RequestMessage {
	msgs := make([]engine.RequestMessage, 0, len(items))
	for _, it := range items {
		msgs = append(msgs, responsesItemToMessage(it.Type, it.Role, it.Content, it.Output, it.Name, it.Arguments))
	}
	return msgs
}

// messageItem / outputItems build conversationItems from a Responses turn so it
// can be appended to the conversation when store != false.
func userMessageItem(role, content string) conversationItem {
	c, _ := json.Marshal(content)
	return normalizeItem(conversationItem{Type: "message", Role: role, Content: c})
}

func assistantItemsFromResponse(resp *engine.Response) []conversationItem {
	out := make([]conversationItem, 0, 1+len(resp.ToolCalls))
	if resp.Content != "" {
		out = append(out, userMessageItem("assistant", resp.Content))
	}
	for _, tc := range resp.ToolCalls {
		args := tc.ArgumentsJSON() // raw verbatim or structured, nil → "{}" (R9-6)
		out = append(out, normalizeItem(conversationItem{
			Type:      "function_call",
			Name:      tc.Name,
			Arguments: args,
		}))
	}
	return out
}

func writeConversationNotFound(w http.ResponseWriter, id string) {
	writeError(w, http.StatusNotFound, "invalid_request_error", fmt.Sprintf("Conversation with id '%s' not found.", id))
}

// resolveConversationID extracts the conversation id from the Responses
// `conversation` param, which may be a bare id string or an object {"id": ...}.
// An empty/absent value returns "" (no conversation referenced).
func resolveConversationID(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	var obj struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.ID != "" {
		return obj.ID, nil
	}
	return "", fmt.Errorf("conversation must be a conversation id string or an object with an 'id'")
}

// isEmptyBodyDecodeErr reports whether a decodeJSONBody error is just an empty
// request body (json.Unmarshal of zero bytes), which the create/update routes
// treat as "no fields supplied" rather than a malformed request.
func isEmptyBodyDecodeErr(err error) bool {
	return err != nil && err.Error() == "unexpected end of JSON input"
}
