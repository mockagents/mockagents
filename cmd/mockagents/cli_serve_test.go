package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mockagents/mockagents/internal/a2a"
	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/recording"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The serve commands (record, replay, mcp) bind 127.0.0.1 on a free port,
// are driven over real HTTP, and are stopped through the notifySignals seam
// with the same signal an operator's Ctrl-C delivers.

const leakedSecret = "sk-abcdefghijklmnopqrstuvwxyz0123456789"

// fakeUpstream is a stand-in provider API that records the auth it saw.
type fakeUpstream struct {
	srv   *httptest.Server
	mu    sync.Mutex
	auth  []string
	calls int
}

func newFakeUpstream(t *testing.T) *fakeUpstream {
	t.Helper()
	u := &fakeUpstream{}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		u.mu.Lock()
		u.auth = append(u.auth, r.Header.Get("Authorization"))
		u.calls++
		u.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","echo":` + strconv.Quote(string(body)) +
			`,"choices":[{"index":0,"message":{"role":"assistant","content":"upstream says ` + leakedSecret + `"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(u.srv.Close)
	return u
}

func (u *fakeUpstream) snapshot() (int, []string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.calls, append([]string(nil), u.auth...)
}

const chatBody = `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`

func TestCLIRecordThenReplay(t *testing.T) {
	up := newFakeUpstream(t)
	cassette := filepath.Join(t.TempDir(), "c.jsonl")

	// record: proxy to the upstream with a forwarded key and redaction on.
	port := freePort(t)
	s, early, ok := serveCLI(t, loopbackURL(port, "/healthz"),
		"record", "--upstream", up.srv.URL, "--cassette", cassette, "--port", strconv.Itoa(port),
		"--api-key", "sk-operator-key", "--redact")
	require.True(t, ok, "record exited early: %+v", early)

	code, body := httpDo(t, http.MethodPost, loopbackURL(port, "/v1/chat/completions"), "application/json", chatBody,
		"Authorization", "Bearer client-key")
	require.Equal(t, http.StatusOK, code, body)
	assert.Contains(t, body, "upstream says", "the client gets the upstream response")
	_, health := httpDo(t, http.MethodGet, loopbackURL(port, "/healthz"), "", "")
	assert.Equal(t, "ok: 1 interactions recorded\n", health)

	res := s.stop(os.Interrupt)
	require.NoError(t, res.Err)
	assert.Contains(t, res.Stdout, "redaction enabled")
	assert.Contains(t, res.Stdout, "mockagents record listening on 127.0.0.1:"+strconv.Itoa(port))
	assert.Contains(t, res.Stdout, "recorded 1 interactions to "+cassette)

	_, auths := up.snapshot()
	require.Len(t, auths, 1)
	assert.Equal(t, "Bearer sk-operator-key", auths[0], "--api-key replaces the client's Authorization")

	raw, err := os.ReadFile(cassette)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), leakedSecret, "--redact masks secrets before they reach the cassette")
	cass, err := recording.Load(cassette)
	require.NoError(t, err)
	require.Equal(t, 1, cass.Len())

	// replay: the recorded exchange is served offline; a miss is a 404.
	port = freePort(t)
	s, early, ok = serveCLI(t, loopbackURL(port, "/healthz"),
		"replay", "--cassette", cassette, "--port", strconv.Itoa(port))
	require.True(t, ok, "replay exited early: %+v", early)
	code, body = httpDo(t, http.MethodPost, loopbackURL(port, "/v1/chat/completions"), "application/json", chatBody)
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, "upstream says")
	code, _ = httpDo(t, http.MethodPost, loopbackURL(port, "/v1/chat/completions"), "application/json",
		`{"model":"gpt-4o","messages":[{"role":"user","content":"something else"}]}`)
	assert.Equal(t, http.StatusNotFound, code)
	_, health = httpDo(t, http.MethodGet, loopbackURL(port, "/healthz"), "", "")
	assert.Equal(t, "ok: 1 interactions loaded\n", health)
	res = s.stop(os.Interrupt)
	require.NoError(t, res.Err)
	assert.Contains(t, res.Stdout, "(cassette="+cassette+", 1 interactions)")
	calls, _ := up.snapshot()
	assert.Equal(t, 1, calls, "replay never contacts the upstream")

	// --strict: a miss is a 503, and --match-ignore lets a varying field match.
	port = freePort(t)
	s, early, ok = serveCLI(t, loopbackURL(port, "/healthz"),
		"replay", "--cassette", cassette, "--port", strconv.Itoa(port), "--strict", "--match-ignore", "temperature")
	require.True(t, ok, "replay --strict exited early: %+v", early)
	code, _ = httpDo(t, http.MethodPost, loopbackURL(port, "/v1/chat/completions"), "application/json",
		`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}],"temperature":0.9}`)
	assert.Equal(t, http.StatusOK, code, "an ignored field does not cause a miss")
	code, _ = httpDo(t, http.MethodPost, loopbackURL(port, "/v1/messages"), "application/json", `{"model":"x"}`)
	assert.Equal(t, http.StatusServiceUnavailable, code)
	res = s.stop(os.Interrupt)
	require.NoError(t, res.Err)
	assert.Contains(t, res.Stdout, "match-ignore=[temperature]")
}

func TestCLIReplayRecordModes(t *testing.T) {
	up := newFakeUpstream(t)

	t.Run("new_episodes records a miss", func(t *testing.T) {
		cassette := filepath.Join(t.TempDir(), "c.jsonl")
		port := freePort(t)
		s, early, ok := serveCLI(t, loopbackURL(port, "/healthz"),
			"replay", "--cassette", cassette, "--port", strconv.Itoa(port),
			"--record-mode", "new_episodes", "--upstream", up.srv.URL, "--redact-pattern", "upstream says")
		require.True(t, ok, "exited early: %+v", early)
		code, body := httpDo(t, http.MethodPost, loopbackURL(port, "/v1/chat/completions"), "application/json", chatBody)
		require.Equal(t, http.StatusOK, code, body)
		res := s.stop(os.Interrupt)
		require.NoError(t, res.Err)
		assert.Contains(t, res.Stdout, "record-mode=new_episodes")
		cass, err := recording.Load(cassette)
		require.NoError(t, err)
		assert.Equal(t, 1, cass.Len(), "the miss was recorded")
		raw, _ := os.ReadFile(cassette)
		assert.NotContains(t, string(raw), "upstream says", "--redact-pattern applies to recorded bodies")
	})

	t.Run("all forwards every request", func(t *testing.T) {
		cassette := filepath.Join(t.TempDir(), "c.jsonl")
		port := freePort(t)
		before, _ := up.snapshot()
		s, early, ok := serveCLI(t, loopbackURL(port, "/healthz"),
			"replay", "--cassette", cassette, "--port", strconv.Itoa(port),
			"--record-mode", "all", "--upstream", up.srv.URL, "--match-ignore", "seed")
		require.True(t, ok, "exited early: %+v", early)
		for i := 0; i < 2; i++ {
			code, _ := httpDo(t, http.MethodPost, loopbackURL(port, "/v1/chat/completions"), "application/json", chatBody)
			require.Equal(t, http.StatusOK, code)
		}
		res := s.stop(os.Interrupt)
		require.NoError(t, res.Err)
		assert.Contains(t, res.Stdout, "--match-ignore is ignored in --record-mode=all")
		after, _ := up.snapshot()
		assert.Equal(t, before+2, after, "even a repeated request is forwarded")
	})
}

func TestCLIReplayAndRecordErrors(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.jsonl")
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"record needs an upstream", []string{"record"}, "--upstream is required"},
		{"record bad redact pattern", []string{"record", "--upstream", "http://127.0.0.1:1", "--cassette", filepath.Join(dir, "r.jsonl"), "--redact-pattern", "("}, "compiling redact patterns"},
		{"record bad upstream URL", []string{"record", "--upstream", "::not a url", "--cassette", filepath.Join(dir, "r2.jsonl")}, "building proxy"},
		{"replay unknown record mode", []string{"replay", "--record-mode", "sometimes"}, "sometimes"},
		{"replay strict with upstream", []string{"replay", "--strict", "--upstream", "http://127.0.0.1:1"}, "--strict cannot be combined with --upstream"},
		{"replay empty cassette", []string{"replay", "--cassette", empty}, "is empty or does not exist"},
		{"replay new_episodes without upstream", []string{"replay", "--cassette", empty, "--record-mode", "new_episodes"}, "--upstream is required for --record-mode=new_episodes"},
		{"replay all bad redact pattern", []string{"replay", "--cassette", empty, "--record-mode", "all", "--upstream", "http://127.0.0.1:1", "--redact-pattern", "("}, "compiling redact patterns"},
		{"replay new_episodes bad upstream", []string{"replay", "--cassette", empty, "--record-mode", "new_episodes", "--upstream", "::bad"}, "building record proxy"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := runCLI(t, tc.args...)
			assert.Equal(t, 2, res.Code)
			assert.ErrorContains(t, res.Err, tc.want)
		})
	}
}

func TestCLIServeReportsBindFailure(t *testing.T) {
	// A port already in use surfaces as the command's error instead of hanging.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	port := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)

	res := runCLI(t, "record", "--upstream", "http://127.0.0.1:1", "--cassette", filepath.Join(t.TempDir(), "c.jsonl"), "--port", port)
	assert.Equal(t, 2, res.Code)
	assert.Error(t, res.Err)

	res = runCLI(t, "replay", "--cassette", filepath.Join(t.TempDir(), "c.jsonl"), "--port", port,
		"--record-mode", "new_episodes", "--upstream", "http://127.0.0.1:1")
	assert.Equal(t, 2, res.Code)
	assert.Error(t, res.Err)
}

// ---- mcp -------------------------------------------------------------------

func mcpAgentsDir(t *testing.T, extra map[string]string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "examples", "weather-mcp.yaml"))
	require.NoError(t, err)
	files := map[string]string{"weather-mcp.yaml": string(src)}
	for k, v := range extra {
		files[k] = v
	}
	return writeTree(t, files)
}

func rpc(t *testing.T, url, method string) map[string]any {
	t.Helper()
	code, body := httpDo(t, http.MethodPost, url, "application/json",
		`{"jsonrpc":"2.0","id":1,"method":"`+method+`","params":{}}`)
	require.Equal(t, http.StatusOK, code, body)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &out), body)
	return out
}

func toolNames(t *testing.T, resp map[string]any) []string {
	t.Helper()
	result, ok := resp["result"].(map[string]any)
	require.True(t, ok, "no result: %v", resp)
	var names []string
	for _, tool := range result["tools"].([]any) {
		names = append(names, tool.(map[string]any)["name"].(string))
	}
	return names
}

func TestCLIMCP_HTTP(t *testing.T) {
	dir := mcpAgentsDir(t, map[string]string{"alpha.yaml": validAgentYAML})
	port := freePort(t)
	s, early, ok := serveCLI(t, loopbackURL(port, "/healthz"),
		"mcp", "--agents-dir", dir, "--port", strconv.Itoa(port), "--manage")
	require.True(t, ok, "mcp exited early: %+v", early)

	names := toolNames(t, rpc(t, loopbackURL(port, "/mcp/rpc"), "tools/list"))
	assert.Contains(t, names, "get_forecast", "the declarative tool is served")
	assert.Contains(t, names, "list_agents", "--manage adds the management tools")

	res := s.stop(os.Interrupt)
	require.NoError(t, res.Err)
	assert.Contains(t, res.Stdout, "listening on 127.0.0.1:"+strconv.Itoa(port)+" (server=weather-mcp)")
	assert.Contains(t, res.Stderr, "agent-management tools enabled (1 agents loaded")
}

func TestCLIMCP_Stdio(t *testing.T) {
	dir := mcpAgentsDir(t, nil)
	in := filepath.Join(t.TempDir(), "stdin.jsonl")
	require.NoError(t, os.WriteFile(in, []byte(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`+"\n"+
			`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`+"\n"), 0o644))
	f, err := os.Open(in)
	require.NoError(t, err)
	defer f.Close()
	prev := os.Stdin
	os.Stdin = f
	defer func() { os.Stdin = prev }()

	res := runCLI(t, "mcp", "--agents-dir", dir, "--transport", "stdio")
	require.NoError(t, res.Err, res.Stderr)
	lines := strings.Split(strings.TrimSpace(res.Stdout), "\n")
	require.Len(t, lines, 2, "stdout carries only JSON-RPC responses: %q", res.Stdout)
	for _, l := range lines {
		assert.True(t, json.Valid([]byte(l)), "non-JSON on the stdio stream: %q", l)
	}
	assert.Contains(t, lines[1], "get_forecast")
}

func TestCLIMCP_Errors(t *testing.T) {
	dir := mcpAgentsDir(t, nil)
	empty := writeTree(t, map[string]string{"alpha.yaml": validAgentYAML, "broken.yaml": "kind: Agent\nmetadata: [\n"})
	two := mcpAgentsDir(t, map[string]string{"second.yaml": strings.Replace(mustRead(t, filepath.Join("..", "..", "examples", "weather-mcp.yaml")), "name: weather-mcp", "name: second-mcp", 1)})

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"unknown transport", []string{"mcp", "--agents-dir", dir, "--transport", "carrier-pigeon"}, `unknown transport "carrier-pigeon"`},
		{"manage on a public bind", []string{"mcp", "--agents-dir", dir, "--manage", "--bind", "0.0.0.0"}, "refusing to bind them"},
		{"no server documents", []string{"mcp", "--agents-dir", empty}, "pass --manage"},
		{"named server missing", []string{"mcp", "--agents-dir", dir, "--server", "ghost"}, `mcp server "ghost" not found`},
		{"ambiguous", []string{"mcp", "--agents-dir", two}, "pick one with --server"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := runCLI(t, tc.args...)
			assert.Equal(t, 2, res.Code)
			assert.ErrorContains(t, res.Err, tc.want)
		})
	}

	// With --manage and no MCPServer document, a synthetic admin server is
	// served.
	mcpManage = true
	def, err := selectMCPServer(&config.Documents{}, empty)
	mcpManage = false
	require.NoError(t, err)
	assert.Equal(t, adminServerName, def.Metadata.Name)

	// Load errors are reported on stderr, never on stdout (the stdio stream).
	res := runCLI(t, "mcp", "--agents-dir", empty)
	assert.Contains(t, res.Stderr, "load error:")
	assert.Empty(t, res.Stdout)

	// --server picks one of several.
	mcpServerName = "second-mcp"
	docs, _ := config.LoadAllDocuments(two)
	def, err = selectMCPServer(docs, two)
	mcpServerName = ""
	require.NoError(t, err)
	assert.Equal(t, "second-mcp", def.Metadata.Name)
}

func TestBuildManageRegistry_SkipsInvalidAgents(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"alpha.yaml": validAgentYAML,
		"bad.yaml":   strings.Replace(strings.Replace(validAgentYAML, "name: alpha", "name: Bad Name", 1), "model-alpha", "model-bad", 1),
	})
	docs, errs := config.LoadAllDocuments(dir)
	require.Empty(t, errs)
	stderr := captureStderr(t, func() {
		r := buildManageRegistry(docs)
		agents := r.ListForTenant("")
		require.Len(t, agents, 1)
		assert.Equal(t, "alpha", agents[0].Metadata.Name)
	})
	assert.Contains(t, stderr, "skipping invalid agent")
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

// ---- a2a -------------------------------------------------------------------

func TestCLIA2A_SelectionErrors(t *testing.T) {
	example := mustRead(t, filepath.Join("..", "..", "examples", "a2a-server.yaml"))
	one := writeTree(t, map[string]string{"a.yaml": example})
	two := writeTree(t, map[string]string{"a.yaml": example, "b.yaml": strings.Replace(example, "name: weather-a2a", "name: other-a2a", 1)})
	none := writeTree(t, map[string]string{"alpha.yaml": validAgentYAML})

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"none", []string{"a2a", "--agents-dir", none}, "no kind:A2AServer definitions"},
		{"named missing", []string{"a2a", "--agents-dir", one, "--server", "ghost"}, `a2a server "ghost" not found`},
		{"ambiguous", []string{"a2a", "--agents-dir", two}, "pick one with --server"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := runCLI(t, tc.args...)
			assert.Equal(t, 2, res.Code)
			assert.ErrorContains(t, res.Err, tc.want)
		})
	}

	docs, _ := config.LoadAllDocuments(two)
	a2aServerName = "other-a2a"
	def, err := selectA2AServer(docs, two)
	a2aServerName = ""
	require.NoError(t, err)
	assert.Equal(t, "other-a2a", def.Metadata.Name)
}

func TestNewA2AMux_ServesCardAndRPC(t *testing.T) {
	docs, errs := config.LoadAllDocuments(writeTree(t, map[string]string{
		"a.yaml": mustRead(t, filepath.Join("..", "..", "examples", "a2a-server.yaml")),
	}))
	require.Empty(t, errs)
	def, err := selectA2AServer(docs, "x")
	require.NoError(t, err)
	srv := httptest.NewServer(newA2AMux(a2a.NewServer(def)))
	defer srv.Close()

	for _, path := range []string{"/.well-known/agent-card.json", "/.well-known/agent.json"} {
		code, body := httpDo(t, http.MethodGet, srv.URL+path, "", "")
		assert.Equal(t, http.StatusOK, code, path)
		assert.Contains(t, body, "Weather Agent", path)
	}
	code, body := httpDo(t, http.MethodGet, srv.URL+"/healthz", "", "")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "ok\n", body)

	code, body = httpDo(t, http.MethodPost, srv.URL+"/", "application/json",
		`{"jsonrpc":"2.0","id":1,"method":"message/send","params":{"message":{"role":"user","messageId":"m1","parts":[{"kind":"text","text":"what is the weather?"}]}}}`)
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, "sunny")
}
