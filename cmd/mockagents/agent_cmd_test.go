package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordingServer captures the last request the CLI sent so tests can assert
// method/path/headers/body, and replies with a canned status + body.
type recordingServer struct {
	srv       *httptest.Server
	method    string
	path      string
	auth      string
	ct        string
	body      string
	replyCode int
	replyBody string
}

func newRecordingServer(t *testing.T, replyCode int, replyBody string) *recordingServer {
	t.Helper()
	rs := &recordingServer{replyCode: replyCode, replyBody: replyBody}
	rs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.method = r.Method
		rs.path = r.URL.Path
		rs.auth = r.Header.Get("Authorization")
		rs.ct = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		rs.body = string(b)
		w.WriteHeader(rs.replyCode)
		_, _ = w.Write([]byte(rs.replyBody))
	}))
	t.Cleanup(rs.srv.Close)
	return rs
}

// resetAgentFlags restores the package-level flag vars between cases so state
// doesn't leak across subtests.
func resetAgentFlags(server string) {
	agentServerURL = server
	agentAPIKey = ""
	agentReplace = false
}

func writeTempAgent(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name+".yaml")
	body := "apiVersion: mockagents/v1\nkind: Agent\nmetadata:\n  name: " + name +
		"\nspec:\n  protocol: openai-chat-completions\n  model: m-" + name +
		"\n  behavior:\n    scenarios:\n      - name: default\n        response:\n          content: hi\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAdd_PostsCreate(t *testing.T) {
	rs := newRecordingServer(t, http.StatusCreated, `{"status":"created"}`)
	resetAgentFlags(rs.srv.URL)
	file := writeTempAgent(t, "cli-bot")

	if err := runAdd(nil, []string{file}); err != nil {
		t.Fatalf("runAdd: %v", err)
	}
	if rs.method != http.MethodPost {
		t.Errorf("method = %s, want POST", rs.method)
	}
	if rs.path != "/api/v1/agents" {
		t.Errorf("path = %s, want /api/v1/agents", rs.path)
	}
	if rs.ct != "application/yaml" {
		t.Errorf("content-type = %s, want application/yaml", rs.ct)
	}
	if !strings.Contains(rs.body, "name: cli-bot") {
		t.Errorf("body did not carry the agent: %s", rs.body)
	}
}

func TestAdd_ReplacePutsToNamePath(t *testing.T) {
	rs := newRecordingServer(t, http.StatusOK, `{"status":"updated"}`)
	resetAgentFlags(rs.srv.URL)
	agentReplace = true
	file := writeTempAgent(t, "up-bot")

	if err := runAdd(nil, []string{file}); err != nil {
		t.Fatalf("runAdd --replace: %v", err)
	}
	if rs.method != http.MethodPut {
		t.Errorf("method = %s, want PUT", rs.method)
	}
	if rs.path != "/api/v1/agents/up-bot" {
		t.Errorf("path = %s, want /api/v1/agents/up-bot", rs.path)
	}
}

func TestAdd_SendsAPIKey(t *testing.T) {
	rs := newRecordingServer(t, http.StatusCreated, `{}`)
	resetAgentFlags(rs.srv.URL)
	agentAPIKey = "mas_secret"
	file := writeTempAgent(t, "auth-bot")

	if err := runAdd(nil, []string{file}); err != nil {
		t.Fatalf("runAdd: %v", err)
	}
	if rs.auth != "Bearer mas_secret" {
		t.Errorf("auth = %q, want Bearer mas_secret", rs.auth)
	}
}

func TestAdd_ServerRejection(t *testing.T) {
	rs := newRecordingServer(t, http.StatusConflict, `{"error":"agent \"dup\" already exists"}`)
	resetAgentFlags(rs.srv.URL)
	file := writeTempAgent(t, "dup")

	err := runAdd(nil, []string{file})
	if err == nil {
		t.Fatal("expected an error on 409")
	}
	if !strings.Contains(err.Error(), "HTTP 409") || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error did not surface server message: %v", err)
	}
}

func TestAdd_Unreachable(t *testing.T) {
	// Point at a closed port (httptest server created then immediately closed).
	rs := newRecordingServer(t, 200, "")
	url := rs.srv.URL
	rs.srv.Close()
	resetAgentFlags(url)
	file := writeTempAgent(t, "x")

	err := runAdd(nil, []string{file})
	if err == nil || !strings.Contains(err.Error(), "could not reach the server") {
		t.Errorf("expected unreachable error, got %v", err)
	}
}

func TestRm_DeletesByName(t *testing.T) {
	rs := newRecordingServer(t, http.StatusOK, `{"status":"deleted"}`)
	resetAgentFlags(rs.srv.URL)

	if err := runRm(nil, []string{"gone-bot"}); err != nil {
		t.Fatalf("runRm: %v", err)
	}
	if rs.method != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", rs.method)
	}
	if rs.path != "/api/v1/agents/gone-bot" {
		t.Errorf("path = %s, want /api/v1/agents/gone-bot", rs.path)
	}
}

func TestRm_NotFound(t *testing.T) {
	rs := newRecordingServer(t, http.StatusNotFound, `{"error":"agent \"ghost\" not found"}`)
	resetAgentFlags(rs.srv.URL)

	err := runRm(nil, []string{"ghost"})
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("expected 404 error, got %v", err)
	}
}

func TestAgentNameFromBytes(t *testing.T) {
	name, err := agentNameFromBytes([]byte("metadata:\n  name: foo-bar\n"))
	if err != nil || name != "foo-bar" {
		t.Fatalf("name=%q err=%v", name, err)
	}
	if _, err := agentNameFromBytes([]byte("metadata: {}\n")); err == nil {
		t.Error("expected error on empty name")
	}
}
