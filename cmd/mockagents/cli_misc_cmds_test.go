package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- logs ------------------------------------------------------------------

// seedLogsDB writes interaction rows the way `start` does and returns the path.
func seedLogsDB(t *testing.T, rows ...storage.InteractionLog) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "logs.db")
	store, err := storage.NewSQLiteStore(path)
	require.NoError(t, err)
	for i := range rows {
		require.NoError(t, store.Log(context.Background(), &rows[i]))
	}
	require.NoError(t, store.Close())
	return path
}

func TestCLILogs(t *testing.T) {
	now := time.Now().UTC()
	db := seedLogsDB(t,
		storage.InteractionLog{Timestamp: now.Add(-3 * time.Hour).Format(time.RFC3339), AgentName: "old-bot", SessionID: "s-old",
			Protocol: "openai", RequestMethod: "POST", RequestPath: "/v1/chat/completions", ResponseStatus: 200, LatencyMs: 7, ScenarioName: "default"},
		storage.InteractionLog{Timestamp: now.Format(time.RFC3339), AgentName: "new-bot", SessionID: "s-new",
			Protocol: "anthropic", RequestMethod: "POST", RequestPath: "/v1/messages", ResponseStatus: 503, LatencyMs: 42, ScenarioName: "outage"},
	)

	t.Run("table", func(t *testing.T) {
		res := runCLI(t, "logs", "--db", db)
		require.NoError(t, res.Err)
		lines := strings.Split(strings.TrimSpace(res.Stdout), "\n")
		require.Len(t, lines, 4, res.Stdout) // header, rule, two rows
		assert.Contains(t, lines[0], "AGENT")
		assert.Contains(t, res.Stdout, "new-bot")
		assert.Contains(t, res.Stdout, "42ms")
		assert.Contains(t, res.Stdout, "outage")
		assert.Contains(t, res.Stdout, "old-bot")
	})
	t.Run("json with filters", func(t *testing.T) {
		res := runCLI(t, "logs", "--db", db, "--output", "json", "--agent", "new-bot")
		require.NoError(t, res.Err)
		var got []storage.InteractionLog
		require.NoError(t, json.Unmarshal([]byte(res.Stdout), &got), res.Stdout)
		require.Len(t, got, 1)
		assert.Equal(t, "new-bot", got[0].AgentName)
		assert.Equal(t, 503, got[0].ResponseStatus)

		res = runCLI(t, "logs", "--db", db, "--output", "json", "--session", "s-old")
		require.NoError(t, res.Err)
		require.NoError(t, json.Unmarshal([]byte(res.Stdout), &got))
		require.Len(t, got, 1)
		assert.Equal(t, "old-bot", got[0].AgentName)
	})
	t.Run("since and limit", func(t *testing.T) {
		res := runCLI(t, "logs", "--db", db, "--output", "json", "--since", "1h")
		require.NoError(t, res.Err)
		var got []storage.InteractionLog
		require.NoError(t, json.Unmarshal([]byte(res.Stdout), &got))
		require.Len(t, got, 1, "the 3h-old row is outside --since 1h")
		assert.Equal(t, "new-bot", got[0].AgentName)

		res = runCLI(t, "logs", "--db", db, "--output", "json", "--limit", "1")
		require.NoError(t, res.Err)
		require.NoError(t, json.Unmarshal([]byte(res.Stdout), &got))
		assert.Len(t, got, 1)
	})
	t.Run("no match: table says so, json is an empty array", func(t *testing.T) {
		res := runCLI(t, "logs", "--db", db, "--agent", "nobody")
		require.NoError(t, res.Err)
		assert.Equal(t, "No interaction logs found.", strings.TrimSpace(res.Stdout))

		res = runCLI(t, "logs", "--db", db, "--agent", "nobody", "--output", "json")
		require.NoError(t, res.Err)
		assert.Equal(t, "[]", strings.TrimSpace(res.Stdout), "never null")
	})
	t.Run("bad --since", func(t *testing.T) {
		res := runCLI(t, "logs", "--db", db, "--since", "yesterday")
		assert.Equal(t, 2, res.Code)
		assert.ErrorContains(t, res.Err, `invalid --since value "yesterday"`)
	})
	t.Run("missing database is an error and is not created", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "absent.db")
		res := runCLI(t, "logs", "--db", missing)
		assert.Equal(t, 2, res.Code)
		assert.ErrorContains(t, res.Err, "no interaction log database")
		_, err := os.Stat(missing)
		assert.True(t, os.IsNotExist(err))
	})
	t.Run("a file that is not a database", func(t *testing.T) {
		junk := filepath.Join(t.TempDir(), "junk.db")
		require.NoError(t, os.WriteFile(junk, []byte(strings.Repeat("not sqlite ", 200)), 0o644))
		res := runCLI(t, "logs", "--db", junk)
		assert.Equal(t, 2, res.Code)
		assert.Error(t, res.Err)
	})
}

func TestCLILogs_UnknownOutputIsAUsageError(t *testing.T) {
	db := seedLogsDB(t)
	res := runCLI(t, "logs", "--db", db, "--output", "jsonl")
	assert.Equal(t, 2, res.Code)
	assert.EqualError(t, res.Err, `unknown --output "jsonl" (want table or json)`)
	assert.Empty(t, res.Stdout)
}

// ---- init ------------------------------------------------------------------

func TestCLIInit_ScaffoldsARunnableProject(t *testing.T) {
	proj := filepath.Join(t.TempDir(), "proj")
	res := runCLI(t, "init", proj)
	require.NoError(t, res.Err, res.Stderr)
	assert.Contains(t, res.Stdout, "Project scaffolded at")
	assert.Contains(t, res.Stdout, "(template: basic)")
	assert.Contains(t, res.Stdout, "cd "+proj)
	for _, f := range []string{".mockagents.yaml", "README.md"} {
		_, err := os.Stat(filepath.Join(proj, f))
		assert.NoError(t, err, f)
	}

	// The printed next steps must work: validate the agents, then run each
	// suggested test suite.
	res = runCLI(t, "validate", filepath.Join(proj, "agents"))
	assert.Equal(t, 0, res.Code, res.Stderr)
	tests, err := filepath.Glob(filepath.Join(proj, "tests", "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, tests)
	sort.Strings(tests)
	res = runCLI(t, append([]string{"test", "--agents-dir", filepath.Join(proj, "agents")}, tests...)...)
	assert.Equal(t, 0, res.Code, "scaffolded suites must pass: %s\n%s", res.Stdout, res.Stderr)

	// A second init into the same directory refuses to clobber.
	res = runCLI(t, "init", proj)
	assert.Equal(t, 2, res.Code)
	assert.ErrorContains(t, res.Err, "already exists (use --force to overwrite)")
}

func TestCLIInit_ForceKeepsEditedFiles(t *testing.T) {
	proj := filepath.Join(t.TempDir(), "proj")
	require.NoError(t, runCLI(t, "init", proj).Err)
	agents, err := filepath.Glob(filepath.Join(proj, "agents", "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, agents)
	edited := agents[0]
	require.NoError(t, os.WriteFile(edited, []byte("# my edits\n"), 0o644))

	res := runCLI(t, "init", proj, "--force", "--template", "rag")
	require.NoError(t, res.Err, res.Stderr)
	assert.Contains(t, res.Stdout, "(template: rag)")
	assert.Contains(t, res.Stdout, "kept ")
	got, err := os.ReadFile(edited)
	require.NoError(t, err)
	assert.Equal(t, "# my edits\n", string(got), "--force must not delete an edited file")
}

func TestCLIInit_CurrentDirectoryAndTemplates(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	res := runCLI(t, "init")
	require.NoError(t, res.Err, res.Stderr)
	assert.NotContains(t, res.Stdout, "1. cd", "no cd step when scaffolding in place")
	_, err := os.Stat(filepath.Join(dir, ".mockagents.yaml"))
	assert.NoError(t, err)

	res = runCLI(t, "init", "--list-templates")
	require.NoError(t, res.Err)
	for _, name := range []string{"basic", "customer-support", "rag", "coding-agent", "planner"} {
		assert.Contains(t, res.Stdout, name)
	}
	assert.Contains(t, res.Stdout, "Usage: mockagents init my-project --template <name>")

	res = runCLI(t, "init", filepath.Join(dir, "x"), "-t", "nope")
	assert.Equal(t, 2, res.Code)
	assert.ErrorContains(t, res.Err, `unknown template "nope"`)

	res = runCLI(t, "init", "a", "b")
	assert.Equal(t, 2, res.Code, "at most one project name")
}

// ---- convert ---------------------------------------------------------------

const aimockFixtures = `{"fixtures":[
	{"match":{"userMessage":"hello"},"response":{"content":"Hi there"}},
	{"match":{"toolName":"get_weather"},"response":{"content":"unsafe catch-all"}}
]}`

func TestCLIConvertAIMock(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "Legacy Fixtures.json")
	require.NoError(t, os.WriteFile(in, []byte(aimockFixtures), 0o644))
	out := filepath.Join(dir, "agent.yaml")

	res := runCLI(t, "convert", "aimock", in, "-o", out, "--model", "gpt-4o")
	require.NoError(t, res.Err, res.Stderr)
	assert.Contains(t, res.Stderr, "skip:", "the unsupported matcher is reported")
	assert.Contains(t, res.Stderr, "converted 1 AIMock fixtures (1 skipped) -> "+out)
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(data), "name: legacy-fixtures", "name defaults to the sanitised file name")
	assert.Contains(t, string(data), "model: gpt-4o")
	res = runCLI(t, "validate", out)
	assert.Equal(t, 0, res.Code, "converted output must validate: %s", res.Stderr)

	// Without --force an existing output is never overwritten.
	require.NoError(t, os.WriteFile(out, []byte("keep me"), 0o644))
	res = runCLI(t, "convert", "aimock", in, "-o", out)
	assert.Equal(t, 2, res.Code)
	assert.ErrorContains(t, res.Err, "use --force to overwrite")
	got, _ := os.ReadFile(out)
	assert.Equal(t, "keep me", string(got))

	res = runCLI(t, "convert", "aimock", in, "-o", out, "--force", "--name", "renamed")
	require.NoError(t, res.Err)
	got, _ = os.ReadFile(out)
	assert.Contains(t, string(got), "name: renamed")

	res = runCLI(t, "convert", "aimock", in, "-o", "-")
	require.NoError(t, res.Err)
	assert.Contains(t, res.Stdout, "kind: Agent", "-o - writes the agent to stdout")

	res = runCLI(t, "convert", "aimock", filepath.Join(dir, "missing.json"), "-o", "-")
	assert.Equal(t, 2, res.Code)
	assert.ErrorContains(t, res.Err, "opening AIMock fixtures")

	res = runCLI(t, "convert", "aimock", in, "-o", "-", "--name", "NOT VALID")
	assert.Equal(t, 2, res.Code, "an invalid converted agent is an error")
	assert.Empty(t, res.Stdout)

	res = runCLI(t, "convert", "aimock", in, "-o", filepath.Join(dir, "no-such-dir", "a.yaml"), "--force")
	assert.ErrorContains(t, res.Err, "writing output")
}

// ---- add / rm through the CLI ---------------------------------------------

func TestCLIAddRm(t *testing.T) {
	type seen struct{ method, path, query, ct, auth string }
	var got seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = seen{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Content-Type"), r.Header.Get("Authorization")}
		if strings.Contains(r.URL.Path, "silent") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	jsonAgent := filepath.Join(t.TempDir(), "bot.json")
	require.NoError(t, os.WriteFile(jsonAgent, []byte(`{"apiVersion":"mockagents/v1","kind":"Agent","metadata":{"name":"json-bot"}}`), 0o644))

	res := runCLI(t, "add", "--server", srv.URL+"/", "--api-key", "k1", "--replace", jsonAgent)
	require.NoError(t, res.Err)
	assert.Equal(t, seen{http.MethodPut, "/api/v1/agents/json-bot", "", "application/json", "Bearer k1"}, got)
	assert.Contains(t, res.Stdout, "Agent accepted (PUT 200)")
	assert.Contains(t, res.Stdout, `{"status":"ok"}`)

	nameless := filepath.Join(t.TempDir(), "nameless.yaml")
	require.NoError(t, os.WriteFile(nameless, []byte("kind: Agent\n"), 0o644))
	res = runCLI(t, "add", "--server", srv.URL, "--replace", nameless)
	assert.ErrorContains(t, res.Err, "--replace needs the file's metadata.name")

	res = runCLI(t, "add", "--server", srv.URL, filepath.Join(t.TempDir(), "absent.yaml"))
	assert.ErrorContains(t, res.Err, "reading ")

	res = runCLI(t, "rm", "--server", srv.URL, "--keep-file", "kept-bot")
	require.NoError(t, res.Err)
	assert.Equal(t, http.MethodDelete, got.method)
	assert.Equal(t, "/api/v1/agents/kept-bot", got.path)
	assert.Equal(t, "keep_file=true", got.query)
	assert.Contains(t, res.Stdout, `Agent "kept-bot" deleted`)

	res = runCLI(t, "rm", "--server", srv.URL, "-y", "silent")
	assert.Equal(t, 2, res.Code)
	assert.EqualError(t, res.Err, "server rejected the request (HTTP 403): Forbidden",
		"an empty error body falls back to the status text")

	// Off a terminal, rm without --yes refuses and sends nothing.
	got = seen{}
	stdin, err := os.Open(jsonAgent)
	require.NoError(t, err)
	defer stdin.Close()
	prevStdin := os.Stdin
	os.Stdin = stdin
	defer func() { os.Stdin = prevStdin }()
	res = runCLI(t, "rm", "--server", srv.URL, "precious")
	assert.ErrorContains(t, res.Err, "refusing to delete agent \"precious\"")
	assert.Equal(t, seen{}, got, "no request is sent")

	srv.Close()
	res = runCLI(t, "rm", "--server", srv.URL, "-y", "gone")
	assert.ErrorContains(t, res.Err, "could not reach the server")
}
