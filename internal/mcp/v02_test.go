package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
)

// v02Def adds an autocomplete catalog to the standard testDef so the
// new completion handler has something to return. Kept separate from
// testDef so the existing tests stay untouched.
func v02Def() *types.MCPServerDefinition {
	def := testDef()
	def.Spec.Completions = []types.MCPCompletion{
		{
			RefType: "ref/prompt",
			RefName: "greet",
			ArgName: "name",
			Values:  []string{"alice", "anna", "bob", "charlie"},
		},
		{
			// Wildcard entry — no RefType / RefName so it serves any
			// prompt that uses the "city" argument.
			ArgName: "city",
			Values:  []string{"Tokyo", "Berlin", "Toronto"},
		},
	}
	return def
}

// --- completion/complete ---

func TestCompletionComplete_FullList(t *testing.T) {
	srv := NewServer(v02Def())
	resp := mustHandle(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"completion/complete","params":{"ref":{"type":"ref/prompt","name":"greet"},"argument":{"name":"name","value":""}}}`)
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
	body := resp.Result.(map[string]any)
	comp := body["completion"].(completionValues)
	want := []string{"alice", "anna", "bob", "charlie"}
	if !equalStrings(comp.Values, want) {
		t.Errorf("values = %v, want %v", comp.Values, want)
	}
	if comp.Total != 4 {
		t.Errorf("total = %d, want 4", comp.Total)
	}
}

func TestCompletionComplete_PrefixFilter(t *testing.T) {
	srv := NewServer(v02Def())
	resp := mustHandle(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"completion/complete","params":{"ref":{"type":"ref/prompt","name":"greet"},"argument":{"name":"name","value":"a"}}}`)
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
	body := resp.Result.(map[string]any)
	comp := body["completion"].(completionValues)
	want := []string{"alice", "anna"}
	if !equalStrings(comp.Values, want) {
		t.Errorf("values = %v, want %v", comp.Values, want)
	}
}

func TestCompletionComplete_WildcardEntry(t *testing.T) {
	srv := NewServer(v02Def())
	// The "city" entry has no RefType/RefName so it matches any ref.
	resp := mustHandle(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"completion/complete","params":{"ref":{"type":"ref/prompt","name":"some-other-prompt"},"argument":{"name":"city","value":"To"}}}`)
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
	body := resp.Result.(map[string]any)
	comp := body["completion"].(completionValues)
	want := []string{"Tokyo", "Toronto"}
	if !equalStrings(comp.Values, want) {
		t.Errorf("values = %v, want %v", comp.Values, want)
	}
}

func TestCompletionComplete_UnknownArgReturnsEmpty(t *testing.T) {
	srv := NewServer(v02Def())
	resp := mustHandle(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"completion/complete","params":{"ref":{"type":"ref/prompt","name":"greet"},"argument":{"name":"nonexistent","value":""}}}`)
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
	body := resp.Result.(map[string]any)
	comp := body["completion"].(completionValues)
	if len(comp.Values) != 0 {
		t.Errorf("values = %v, want empty", comp.Values)
	}
}

func TestCompletionComplete_RejectsMissingArgName(t *testing.T) {
	srv := NewServer(v02Def())
	resp := mustHandle(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"completion/complete","params":{"ref":{},"argument":{"value":"x"}}}`)
	if resp.Error == nil {
		t.Fatal("expected InvalidParams error")
	}
	if resp.Error.Code != ErrInvalidParams {
		t.Errorf("code = %d, want %d", resp.Error.Code, ErrInvalidParams)
	}
}

// --- logging/setLevel ---

func TestLoggingSetLevel_RecordsValidLevel(t *testing.T) {
	srv := NewServer(v02Def())
	resp := mustHandle(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"logging/setLevel","params":{"level":"warning"}}`)
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
	if got := srv.LogLevel(); got != "warning" {
		t.Errorf("LogLevel = %q, want warning", got)
	}
}

func TestLoggingSetLevel_RejectsUnknownLevel(t *testing.T) {
	srv := NewServer(v02Def())
	resp := mustHandle(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"logging/setLevel","params":{"level":"bogus"}}`)
	if resp.Error == nil {
		t.Fatal("expected InvalidParams error")
	}
}

// --- sampling / roots return a clear server-initiated error ---

func TestSamplingNotSupportedExplains(t *testing.T) {
	srv := NewServer(v02Def())
	resp := mustHandle(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"sampling/createMessage","params":{}}`)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(resp.Error.Message, "server-initiated") {
		t.Errorf("error message should mention 'server-initiated', got %q", resp.Error.Message)
	}
}

func TestRootsListNotSupported(t *testing.T) {
	srv := NewServer(v02Def())
	resp := mustHandle(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"roots/list","params":{}}`)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

// --- notification queue + transport bundling ---

func TestEmitNotification_QueuesAndDrains(t *testing.T) {
	srv := NewServer(v02Def())
	srv.EmitNotification("notifications/tools/list_changed", nil)
	srv.EmitNotification("notifications/resources/updated", map[string]any{"uri": "mock://x"})

	got := srv.DrainNotifications()
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Method != "notifications/tools/list_changed" {
		t.Errorf("first method = %q", got[0].Method)
	}

	// Second drain returns nothing — queue is FIFO + clears.
	if drained := srv.DrainNotifications(); drained != nil {
		t.Errorf("second drain = %v, want nil", drained)
	}
}

// Round-10 R10-15 replaced the invalid {response,notifications} envelope:
// a regular request advertises pending notifications via header only, and a
// follow-up notification POST drains them as the array body.
func TestHTTPHandler_HeaderAdvertisesThenNotificationPostDrains(t *testing.T) {
	srv := NewServer(v02Def())
	srv.EmitNotification("notifications/tools/list_changed", nil)

	req := httptest.NewRequest(http.MethodPost, "/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	rec := httptest.NewRecorder()
	NewHTTPHandler(srv).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("X-MCP-Pending-Notifications") != "1" {
		t.Errorf("header = %q, want 1", rec.Header().Get("X-MCP-Pending-Notifications"))
	}
	// The body is exactly the JSON-RPC ping response; the queue is intact.
	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.JSONRPC != "2.0" || resp.Error != nil {
		t.Errorf("body is not the plain ping response: %+v", resp)
	}

	// The client polls with a notification POST and receives the array body.
	req2 := httptest.NewRequest(http.MethodPost, "/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	rec2 := httptest.NewRecorder()
	NewHTTPHandler(srv).ServeHTTP(rec2, req2)
	var notifs []*Notification
	if err := json.NewDecoder(rec2.Body).Decode(&notifs); err != nil {
		t.Fatalf("decode notifications: %v", err)
	}
	if len(notifs) != 1 || notifs[0].Method != "notifications/tools/list_changed" {
		t.Errorf("drained notifications = %+v, want the queued list_changed", notifs)
	}
}

func TestHTTPHandler_NotificationRequestEmitsArrayBody(t *testing.T) {
	srv := NewServer(v02Def())
	srv.EmitNotification("notifications/tools/list_changed", nil)

	// notifications/initialized has no id → Handle returns nil → the
	// HTTP handler normally writes 204. With a pending notification it
	// must write a JSON array body instead.
	req := httptest.NewRequest(http.MethodPost, "/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	rec := httptest.NewRecorder()
	NewHTTPHandler(srv).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var notifs []*Notification
	if err := json.NewDecoder(rec.Body).Decode(&notifs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(notifs) != 1 {
		t.Errorf("notifications = %d, want 1", len(notifs))
	}
}

func TestNotifyHandler_AcceptsPostAndQueues(t *testing.T) {
	srv := NewServer(v02Def())
	body := bytes.NewBufferString(
		`{"method":"notifications/resources/updated","params":{"uri":"mock://x"}}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp/notify", body)
	rec := httptest.NewRecorder()
	NewNotifyHandler(srv).ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	q := srv.DrainNotifications()
	if len(q) != 1 {
		t.Fatalf("queue = %d, want 1", len(q))
	}
	if q[0].Method != "notifications/resources/updated" {
		t.Errorf("method = %q", q[0].Method)
	}
}

func TestNotifyHandler_RejectsMissingMethod(t *testing.T) {
	srv := NewServer(v02Def())
	req := httptest.NewRequest(http.MethodPost, "/mcp/notify",
		strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	NewNotifyHandler(srv).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// --- helpers ---

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
