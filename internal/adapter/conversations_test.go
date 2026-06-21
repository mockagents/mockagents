package adapter

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// convRig is a Conversations handler + a Responses handler sharing one store, so
// a /v1/responses turn can read and extend a conversation created via the API.
type convRig struct {
	conv  *ConversationsHandler
	resp  *ResponsesHandler
	store *conversationStore
}

func newConvRig() *convRig {
	store := newConversationStore()
	return &convRig{
		conv:  NewConversationsHandler(store),
		resp:  NewResponsesHandler(testEngine(responsesAgent()), store),
		store: store,
	}
}

func (rig *convRig) create(t *testing.T, tenant, body string) Conversation {
	t.Helper()
	req := httptest.NewRequest("POST", "/v1/conversations", strings.NewReader(body))
	if tenant != "" {
		req = req.WithContext(withTenant(req, tenant))
	}
	rec := httptest.NewRecorder()
	rig.conv.HandleCreate(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var c Conversation
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &c))
	return c
}

// listItems lists in ascending (chronological) order so callers can assert on a
// stable insertion order; the default (desc) + pagination are covered separately.
func (rig *convRig) listItems(t *testing.T, tenant, id string) []map[string]any {
	t.Helper()
	req := httptest.NewRequest("GET", "/v1/conversations/"+id+"/items?order=asc", nil)
	req.SetPathValue("id", id)
	if tenant != "" {
		req = req.WithContext(withTenant(req, tenant))
	}
	rec := httptest.NewRecorder()
	rig.conv.HandleListItems(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var env struct {
		Object string           `json:"object"`
		Data   []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	assert.Equal(t, "list", env.Object)
	return env.Data
}

// turn fires a /v1/responses request bound to a conversation id.
func (rig *convRig) turn(t *testing.T, tenant, convID, input string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"model":"gpt-4o","input":%q,"conversation":%q}`, input, convID)
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if tenant != "" {
		req = req.WithContext(withTenant(req, tenant))
	}
	rec := httptest.NewRecorder()
	rig.resp.HandleResponses(rec, req)
	return rec
}

func TestConversation_CreateRetrieveDelete(t *testing.T) {
	rig := newConvRig()
	c := rig.create(t, "", `{"metadata":{"topic":"demo"}}`)
	assert.Regexp(t, `^conv_`, c.ID)
	assert.Equal(t, "conversation", c.Object)
	assert.Equal(t, "demo", c.Metadata["topic"])

	// Retrieve mirrors create.
	rreq := httptest.NewRequest("GET", "/v1/conversations/"+c.ID, nil)
	rreq.SetPathValue("id", c.ID)
	rrec := httptest.NewRecorder()
	rig.conv.HandleRetrieve(rrec, rreq)
	require.Equal(t, http.StatusOK, rrec.Code)

	// Delete, then retrieve is 404.
	dreq := httptest.NewRequest("DELETE", "/v1/conversations/"+c.ID, nil)
	dreq.SetPathValue("id", c.ID)
	drec := httptest.NewRecorder()
	rig.conv.HandleDelete(drec, dreq)
	require.Equal(t, http.StatusOK, drec.Code)
	var del map[string]any
	require.NoError(t, json.Unmarshal(drec.Body.Bytes(), &del))
	assert.Equal(t, "conversation.deleted", del["object"])
	assert.Equal(t, true, del["deleted"])

	rrec2 := httptest.NewRecorder()
	rreq2 := httptest.NewRequest("GET", "/v1/conversations/"+c.ID, nil)
	rreq2.SetPathValue("id", c.ID)
	rig.conv.HandleRetrieve(rrec2, rreq2)
	assert.Equal(t, http.StatusNotFound, rrec2.Code)
}

func TestConversation_CreateWithItemsAndEmptyBody(t *testing.T) {
	rig := newConvRig()
	// Empty body is a valid empty conversation.
	c := rig.create(t, "", ``)
	assert.Regexp(t, `^conv_`, c.ID)
	assert.Empty(t, rig.listItems(t, "", c.ID))

	// Seed items at create time; each gets an id + object on read.
	c2 := rig.create(t, "", `{"items":[{"type":"message","role":"user","content":"hi"}]}`)
	items := rig.listItems(t, "", c2.ID)
	require.Len(t, items, 1)
	assert.Equal(t, "message", items[0]["type"])
	assert.Equal(t, "conversation.item", items[0]["object"])
	assert.Regexp(t, `^msg_`, items[0]["id"])
}

func TestConversation_ItemsCRUD(t *testing.T) {
	rig := newConvRig()
	c := rig.create(t, "", ``)

	// Create items.
	req := httptest.NewRequest("POST", "/v1/conversations/"+c.ID+"/items",
		strings.NewReader(`{"items":[{"type":"message","role":"user","content":"first"},{"type":"message","role":"assistant","content":"second"}]}`))
	req.SetPathValue("id", c.ID)
	rec := httptest.NewRecorder()
	rig.conv.HandleCreateItems(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	items := rig.listItems(t, "", c.ID)
	require.Len(t, items, 2)
	firstID, _ := items[0]["id"].(string)
	require.NotEmpty(t, firstID)

	// Get a single item.
	greq := httptest.NewRequest("GET", "/v1/conversations/"+c.ID+"/items/"+firstID, nil)
	greq.SetPathValue("id", c.ID)
	greq.SetPathValue("item_id", firstID)
	grec := httptest.NewRecorder()
	rig.conv.HandleGetItem(grec, greq)
	require.Equal(t, http.StatusOK, grec.Code)

	// Delete it; list shrinks.
	dreq := httptest.NewRequest("DELETE", "/v1/conversations/"+c.ID+"/items/"+firstID, nil)
	dreq.SetPathValue("id", c.ID)
	dreq.SetPathValue("item_id", firstID)
	drec := httptest.NewRecorder()
	rig.conv.HandleDeleteItem(drec, dreq)
	require.Equal(t, http.StatusOK, drec.Code)
	assert.Len(t, rig.listItems(t, "", c.ID), 1)

	// Deleting an unknown item is 404.
	d2 := httptest.NewRecorder()
	dreq2 := httptest.NewRequest("DELETE", "/v1/conversations/"+c.ID+"/items/nope", nil)
	dreq2.SetPathValue("id", c.ID)
	dreq2.SetPathValue("item_id", "nope")
	rig.conv.HandleDeleteItem(d2, dreq2)
	assert.Equal(t, http.StatusNotFound, d2.Code)
}

func TestConversation_RespondsTurnAccumulatesState(t *testing.T) {
	rig := newConvRig()
	c := rig.create(t, "", ``)

	// Turn 1: input + assistant output get appended to the conversation.
	rec1 := rig.turn(t, "", c.ID, "hello")
	require.Equal(t, http.StatusOK, rec1.Code, rec1.Body.String())
	after1 := rig.listItems(t, "", c.ID)
	require.Len(t, after1, 2, "turn 1 should append the user input + assistant reply")
	assert.Equal(t, "user", after1[0]["role"])
	assert.Equal(t, "assistant", after1[1]["role"])

	// Turn 2 on the same conversation: prior items are replayed and this turn is
	// appended too, so the conversation keeps growing.
	rec2 := rig.turn(t, "", c.ID, "and again")
	require.Equal(t, http.StatusOK, rec2.Code, rec2.Body.String())
	after2 := rig.listItems(t, "", c.ID)
	require.Len(t, after2, 4, "turn 2 should append on top of turn 1's items")
}

func TestConversation_ResponsesUnknownConversationIs404(t *testing.T) {
	rig := newConvRig()
	rec := rig.turn(t, "", "conv_missing", "hi")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestConversation_ResponsesConversationAndPreviousIDConflict(t *testing.T) {
	rig := newConvRig()
	c := rig.create(t, "", ``)
	body := fmt.Sprintf(`{"model":"gpt-4o","input":"hi","conversation":%q,"previous_response_id":"resp_x"}`, c.ID)
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	rig.resp.HandleResponses(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestConversation_ObjectFormConversationParam(t *testing.T) {
	// The conversation param may be an object {"id": "..."}.
	rig := newConvRig()
	c := rig.create(t, "", ``)
	body := fmt.Sprintf(`{"model":"gpt-4o","input":"hello","conversation":{"id":%q}}`, c.ID)
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	rig.resp.HandleResponses(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, rig.listItems(t, "", c.ID), 2)
}

func TestConversation_TenantIsolation(t *testing.T) {
	rig := newConvRig()
	c := rig.create(t, "tenant-1", ``)
	// Tenant 2 cannot see tenant 1's conversation.
	req := httptest.NewRequest("GET", "/v1/conversations/"+c.ID, nil)
	req.SetPathValue("id", c.ID)
	req = req.WithContext(withTenant(req, "tenant-2"))
	rec := httptest.NewRecorder()
	rig.conv.HandleRetrieve(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestConversation_UpdateMetadata(t *testing.T) {
	rig := newConvRig()
	c := rig.create(t, "", `{"metadata":{"topic":"orig"}}`)

	update := func(body string) Conversation {
		req := httptest.NewRequest("POST", "/v1/conversations/"+c.ID, strings.NewReader(body))
		req.SetPathValue("id", c.ID)
		rec := httptest.NewRecorder()
		rig.conv.HandleUpdate(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var got Conversation
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		return got
	}

	// An empty-body update must NOT wipe existing metadata (F-103).
	assert.Equal(t, "orig", update(``).Metadata["topic"])
	// A present metadata object replaces it.
	assert.Equal(t, "new", update(`{"metadata":{"topic":"new"}}`).Metadata["topic"])
}

func (rig *convRig) listItemsRaw(t *testing.T, id, query string) map[string]any {
	t.Helper()
	req := httptest.NewRequest("GET", "/v1/conversations/"+id+"/items"+query, nil)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	rig.conv.HandleListItems(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var env map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	return env
}

func TestConversation_ListItemsPagination(t *testing.T) {
	rig := newConvRig()
	c := rig.create(t, "", ``)

	createBody := `{"items":[{"type":"message","role":"user","content":"a"},{"type":"message","role":"user","content":"b"},{"type":"message","role":"user","content":"c"},{"type":"message","role":"user","content":"d"},{"type":"message","role":"user","content":"e"}]}`
	creq := httptest.NewRequest("POST", "/v1/conversations/"+c.ID+"/items", strings.NewReader(createBody))
	creq.SetPathValue("id", c.ID)
	crec := httptest.NewRecorder()
	rig.conv.HandleCreateItems(crec, creq)
	require.Equal(t, http.StatusOK, crec.Code, crec.Body.String())
	var created struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(crec.Body.Bytes(), &created))
	require.Len(t, created.Data, 5)
	oldestID := created.Data[0]["id"].(string)
	newestID := created.Data[4]["id"].(string)

	// Default order is desc (newest first), no more pages.
	def := rig.listItemsRaw(t, c.ID, "")
	defData := def["data"].([]any)
	require.Len(t, defData, 5)
	assert.Equal(t, false, def["has_more"])
	assert.Equal(t, newestID, defData[0].(map[string]any)["id"], "desc → newest first")

	// limit caps the page and sets has_more.
	lim := rig.listItemsRaw(t, c.ID, "?limit=2")
	assert.Len(t, lim["data"].([]any), 2)
	assert.Equal(t, true, lim["has_more"])

	// order=asc → oldest first.
	asc := rig.listItemsRaw(t, c.ID, "?order=asc")
	assert.Equal(t, oldestID, asc["data"].([]any)[0].(map[string]any)["id"])

	// after cursor (asc) returns the items following the oldest → 4 of 5.
	after := rig.listItemsRaw(t, c.ID, "?order=asc&after="+oldestID)
	assert.Len(t, after["data"].([]any), 4)
}

func TestConversation_ResponseEchoesConversationField(t *testing.T) {
	rig := newConvRig()
	c := rig.create(t, "", ``)
	rec := rig.turn(t, "", c.ID, "hello")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	conv, ok := resp["conversation"].(map[string]any)
	require.True(t, ok, "response must echo a conversation object")
	assert.Equal(t, c.ID, conv["id"])
}

func TestConversation_StoreFalseStillAppends(t *testing.T) {
	rig := newConvRig()
	c := rig.create(t, "", ``)
	// A conversation's items have their own lifecycle, so store:false must NOT
	// skip appending them.
	body := fmt.Sprintf(`{"model":"gpt-4o","input":"hello","conversation":%q,"store":false}`, c.ID)
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	rig.resp.HandleResponses(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, rig.listItems(t, "", c.ID), 2, "store:false must still append to the conversation")
}
