package api_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/app"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

type env struct {
	t    *testing.T
	app  *app.App
	srv  *httptest.Server
	spec map[string]any
	c    *jsonschema.Compiler
}

func newEnv(t *testing.T, opts app.Options) *env {
	t.Helper()
	opts.Logger = quiet
	a, err := app.New(opts)
	require.NoError(t, err)
	srv := httptest.NewServer(a.Handler)
	t.Cleanup(func() {
		srv.Close()
		a.Close()
	})
	raw, err := os.ReadFile("../../openapi.yaml")
	require.NoError(t, err)
	var spec map[string]any
	require.NoError(t, yaml.Unmarshal(raw, &spec))
	c := jsonschema.NewCompiler()
	require.NoError(t, c.AddResource("openapi.yaml", spec))
	return &env{t: t, app: a, srv: srv, spec: spec, c: c}
}

// do sends a request and returns status + decoded JSON body.
func (e *env) do(method, path string, body any, headers ...string) (int, any) {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(e.t, err)
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, rd)
	require.NoError(e.t, err)
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(e.t, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(e.t, err)
	var out any
	if len(raw) > 0 && strings.Contains(resp.Header.Get("Content-Type"), "json") {
		require.NoError(e.t, json.Unmarshal(raw, &out), string(raw))
	}
	return resp.StatusCode, out
}

// valid asserts that v matches components/schemas/<name>.
func (e *env) valid(name string, v any) {
	e.t.Helper()
	sch, err := e.c.Compile("openapi.yaml#/components/schemas/" + name)
	require.NoError(e.t, err, name)
	if err := sch.Validate(v); err != nil {
		raw, _ := json.MarshalIndent(v, "", "  ")
		e.t.Fatalf("response does not match schema %s: %v\n%s", name, err, truncate(string(raw), 4000))
	}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }

// TestOpenAPIMatchesRoutes checks the route table and openapi.yaml against
// each other in both directions, plus document hygiene ($refs resolve,
// operationIds unique).
func TestOpenAPIMatchesRoutes(t *testing.T) {
	e := newEnv(t, app.Options{})
	paths := obj(e.spec["paths"])
	documented := map[string]bool{}
	opIDs := map[string]bool{}
	for p, item := range paths {
		for method, op := range obj(item) {
			if method == "parameters" {
				continue
			}
			documented[strings.ToUpper(method)+" "+p] = true
			id, _ := obj(op)["operationId"].(string)
			require.NotEmpty(t, id, "%s %s has no operationId", method, p)
			require.False(t, opIDs[id], "duplicate operationId %s", id)
			opIDs[id] = true
			require.NotEmpty(t, obj(obj(op)["responses"]), "%s %s has no responses", method, p)
		}
	}
	routes := map[string]bool{}
	for _, r := range e.app.Server.RoutePatterns() {
		routes[r] = true
		require.True(t, documented[r], "route %q is not documented in openapi.yaml", r)
	}
	var undocumented []string
	for d := range documented {
		if !routes[d] {
			undocumented = append(undocumented, d)
		}
	}
	sort.Strings(undocumented)
	require.Empty(t, undocumented, "openapi.yaml documents operations the server does not serve")

	// Every $ref resolves, and every component schema compiles.
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if ref, ok := x["$ref"].(string); ok {
				require.True(t, strings.HasPrefix(ref, "#/"), "external ref %s", ref)
				node := any(e.spec)
				for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
					node = obj(node)[part]
					require.NotNil(t, node, "unresolved $ref %s", ref)
				}
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(e.spec)
	for name := range obj(obj(e.spec["components"])["schemas"]) {
		_, err := e.c.Compile("openapi.yaml#/components/schemas/" + name)
		require.NoError(t, err, name)
	}

	// The validator must actually bite: drift between Go structs and the
	// spec (unknown field, missing required field, bad enum) is caught.
	errSchema, err := e.c.Compile("openapi.yaml#/components/schemas/Error")
	require.NoError(t, err)
	require.NoError(t, errSchema.Validate(map[string]any{"error": map[string]any{"code": "x", "message": "y"}}))
	require.Error(t, errSchema.Validate(map[string]any{"error": map[string]any{"code": "x"}}), "missing required field")
	require.Error(t, errSchema.Validate(map[string]any{"error": map[string]any{"code": "x", "message": "y"}, "extra": 1}), "unknown field")
	status, err := e.c.Compile("openapi.yaml#/components/schemas/RunStatus")
	require.NoError(t, err)
	require.Error(t, status.Validate("stuck"), "only in_progress, completed and failed are valid run states")
}

// TestResponsesMatchSchemas exercises the API and validates every JSON
// response against the schema the spec declares for it.
func TestResponsesMatchSchemas(t *testing.T) {
	e := newEnv(t, app.Options{})

	st, body := e.do("GET", "/healthz", nil)
	require.Equal(t, 200, st)
	e.valid("Health", body)
	st, body = e.do("GET", "/readyz", nil)
	require.Equal(t, 200, st)
	e.valid("Ready", body)
	_, body = e.do("GET", "/api/info", nil)
	e.valid("Info", body)
	_, body = e.do("GET", "/api/workflows", nil)
	e.valid("WorkflowList", body)
	require.Len(t, obj(body)["workflows"], 4, "four public workflows")
	_, body = e.do("GET", "/api/workflows/support-triage", nil)
	e.valid("WorkflowInfo", body)
	_, body = e.do("GET", "/api/agents", nil)
	e.valid("AgentList", body)
	_, body = e.do("GET", "/api/agents/researcher", nil)
	e.valid("AgentView", body)
	_, body = e.do("GET", "/api/tools", nil)
	e.valid("ToolList", body)
	st, body = e.do("POST", "/api/tools/calculator/execute", map[string]any{"arguments": map[string]any{"expression": "(1+2)*3"}})
	require.Equal(t, 200, st)
	e.valid("ExecuteToolResponse", body)
	require.Equal(t, "9", obj(obj(body)["result"])["result"])
	_, body = e.do("GET", "/api/config", nil)
	e.valid("ConfigEnvelope", body)
	_, body = e.do("GET", "/api/mock/status", nil)
	e.valid("MockStatus", body)

	// A full manual run: create (waits until the refund approval), approve
	// the tool, approve the gate, then read run/trace/reviews/list.
	st, body = e.do("POST", "/api/workflows/support-triage/runs?wait=30", map[string]any{
		"input":   map[string]any{"ticket": "I was charged twice for order ORD-1001. Please refund the duplicate charge."},
		"options": map[string]any{"review_mode": "manual"},
	})
	require.Equal(t, 201, st)
	e.valid("Run", body)
	run := obj(body)
	id := run["id"].(string)
	require.Equal(t, "awaiting_approval", run["phase"])

	_, body = e.do("GET", "/api/reviews?status=pending&blocking=true&run_id="+id, nil)
	e.valid("ReviewList", body)
	items := obj(body)["reviews"].([]any)
	require.Len(t, items, 1)
	revID := obj(items[0])["id"].(string)
	_, body = e.do("GET", "/api/reviews/"+revID, nil)
	e.valid("ReviewItem", body)

	st, body = e.do("POST", "/api/reviews/"+revID+"/decision", map[string]any{"action": "revise", "comment": "x"})
	require.Equal(t, 400, st, "revise is not offered for tool approvals")
	e.valid("Error", body)
	require.Equal(t, "action_not_allowed", obj(obj(body)["error"])["code"])

	st, body = e.do("POST", "/api/reviews/"+revID+"/decision", map[string]any{"action": "approve", "reviewer": "test"})
	require.Equal(t, 200, st)
	e.valid("ReviewItem", body)
	st, body = e.do("POST", "/api/reviews/"+revID+"/decision", map[string]any{"action": "approve"})
	require.Equal(t, 409, st, "a review takes exactly one decision")
	e.valid("Error", body)

	waitFor(t, func() bool {
		_, b := e.do("GET", "/api/runs/"+id, nil)
		return obj(b)["phase"] == "awaiting_review"
	})
	_, body = e.do("GET", "/api/reviews?status=pending&kind=output_gate&run_id="+id, nil)
	gate := obj(obj(body)["reviews"].([]any)[0])
	st, _ = e.do("POST", "/api/reviews/"+gate["id"].(string)+"/decision", map[string]any{"action": "approve"})
	require.Equal(t, 200, st)
	waitFor(t, func() bool {
		_, b := e.do("GET", "/api/runs/"+id, nil)
		return obj(b)["status"] == "completed"
	})
	_, body = e.do("GET", "/api/runs/"+id, nil)
	e.valid("Run", body)
	_, body = e.do("GET", "/api/runs/"+id+"/trace", nil)
	e.valid("Trace", body)
	require.NotEmpty(t, obj(body)["spans"])
	_, body = e.do("GET", "/api/runs?limit=10", nil)
	e.valid("RunList", body)

	// Direct invoke + a failing invoke.
	st, body = e.do("POST", "/api/agents/planner/invoke", map[string]any{"input": "Task: plan a research brief\nTopic: retry strategies"})
	require.Equal(t, 200, st)
	e.valid("InvokeResponse", body)
	e.valid("AgentResult", obj(body)["result"])
	st, body = e.do("POST", "/api/agents/drill-overloaded/invoke", map[string]any{"input": "x"})
	require.Equal(t, 200, st)
	e.valid("InvokeResponse", body)
	require.Equal(t, "failed", obj(body)["status"])

	// Errors.
	st, body = e.do("GET", "/api/runs/nope", nil)
	require.Equal(t, 404, st)
	e.valid("Error", body)
	st, body = e.do("POST", "/api/workflows/research-brief/runs", map[string]any{"input": map[string]any{}})
	require.Equal(t, 400, st)
	e.valid("Error", body)
	st, body = e.do("POST", "/api/workflows/research-brief/runs", map[string]any{"input": map[string]any{"topic": "x"}, "bogus": 1})
	require.Equal(t, 400, st, "unknown request fields are rejected")
	e.valid("Error", body)
	st, body = e.do("POST", "/api/tools/issue_refund/execute", map[string]any{"arguments": map[string]any{}})
	require.Equal(t, 403, st)
	e.valid("Error", body)
}

func TestAgentAndConfigPatching(t *testing.T) {
	e := newEnv(t, app.Options{})
	// Merge patch: pin tier and set a retry policy.
	st, body := e.do("PATCH", "/api/agents/summarizer", map[string]any{"tier": "llm", "retry": map[string]any{"max_retries": 5, "initial_backoff_ms": 50, "max_backoff_ms": 400, "multiplier": 2, "jitter": 0}})
	require.Equal(t, 200, st)
	e.valid("AgentView", body)
	route := obj(obj(body)["effective_route"])
	require.Equal(t, "llm", route["tier"])
	require.EqualValues(t, 5, obj(obj(body)["effective_retry"])["max_retries"])

	// null removes the override, so the default policy applies again.
	st, body = e.do("PATCH", "/api/agents/summarizer", map[string]any{"retry": nil})
	require.Equal(t, 200, st)
	require.EqualValues(t, 3, obj(obj(body)["effective_retry"])["max_retries"])

	// Invalid changes are rejected with field details, and nothing changes.
	st, body = e.do("PATCH", "/api/agents/summarizer", map[string]any{"retry": map[string]any{"max_retries": 6}})
	require.Equal(t, 400, st)
	e.valid("Error", body)
	require.Equal(t, "invalid_config", obj(obj(body)["error"])["code"])
	st, _ = e.do("PATCH", "/api/agents/summarizer", map[string]any{"name": "renamed"})
	require.Equal(t, 400, st)
	st, _ = e.do("PATCH", "/api/agents/summarizer", map[string]any{"tools": []string{"no_such_tool"}})
	require.Equal(t, 400, st)
	st, _ = e.do("PATCH", "/api/agents/classifier", map[string]any{"tools": []string{"search_kb"}})
	require.Equal(t, 400, st, "the gemini client is text-only")

	st, body = e.do("PATCH", "/api/config", map[string]any{"router": map[string]any{"escalation_threshold": 0.95}})
	require.Equal(t, 200, st)
	e.valid("ConfigEnvelope", body)
	st, _ = e.do("PATCH", "/api/config", map[string]any{"agents": []any{}})
	require.Equal(t, 400, st)
	st, _ = e.do("PATCH", "/api/config", map[string]any{"defaults": map[string]any{"review": map[string]any{"timeout_ms": 99999999}}})
	require.Equal(t, 400, st, "review timeout must be shorter than the run timeout")

	// With the threshold at 0.95, even a clear billing ticket (0.94) escalates.
	st, body = e.do("POST", "/api/workflows/support-triage/runs?wait=30", map[string]any{
		"input": map[string]any{"ticket": "Please refund the duplicate charge."}, "options": map[string]any{"review_mode": "auto"}})
	require.Equal(t, 201, st)
	require.Equal(t, true, obj(obj(body)["output"])["escalated"])

	st, body = e.do("POST", "/api/config/reset", nil)
	require.Equal(t, 200, st)
	require.EqualValues(t, 0.6, obj(obj(obj(body)["config"])["router"])["escalation_threshold"])
}

func TestTokenProtectsMutations(t *testing.T) {
	e := newEnv(t, app.Options{Token: "s3cret"})
	st, _ := e.do("GET", "/api/agents", nil)
	require.Equal(t, 200, st, "reads stay open")
	st, body := e.do("POST", "/api/config/reset", nil)
	require.Equal(t, 401, st)
	e.valid("Error", body)
	st, _ = e.do("POST", "/api/config/reset", nil, "Authorization", "Bearer s3cret")
	require.Equal(t, 200, st)
}

func TestSSE(t *testing.T) {
	e := newEnv(t, app.Options{})
	// Streaming invoke: deltas, then done.
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/agents/assistant/invoke", strings.NewReader(`{"input":"what can you do?","stream":true}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, "text/event-stream; charset=utf-8", resp.Header.Get("Content-Type"))
	events := readEvents(t, resp.Body)
	resp.Body.Close()
	require.Equal(t, "run", events[0])
	require.Equal(t, "done", events[len(events)-1])
	require.GreaterOrEqual(t, count(events, "delta"), 3)

	// Broken stream: reset + retry events, then done.
	req, _ = http.NewRequest("POST", e.srv.URL+"/api/agents/drill-streamcut/invoke?stream=true", strings.NewReader(`{"input":"status"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	events = readEvents(t, resp.Body)
	resp.Body.Close()
	require.Contains(t, events, "reset")
	require.Contains(t, events, "retry")
	require.Equal(t, "done", events[len(events)-1])

	// Run events: snapshots until done.
	st, body := e.do("POST", "/api/workflows/decision-review/runs", map[string]any{"input": map[string]any{"question": "Should we rewrite the legacy module?"}, "options": map[string]any{"review_mode": "auto"}})
	require.Equal(t, 201, st)
	resp, err = http.Get(e.srv.URL + "/api/runs/" + obj(body)["id"].(string) + "/events")
	require.NoError(t, err)
	events = readEvents(t, resp.Body)
	resp.Body.Close()
	require.Equal(t, "done", events[len(events)-1])
	require.GreaterOrEqual(t, count(events, "run"), 1)
}

func TestUIServed(t *testing.T) {
	e := newEnv(t, app.Options{})
	for _, p := range []string{"/", "/ui/app.js", "/ui/style.css", "/openapi.yaml", "/openapi.json", "/metrics"} {
		resp, err := http.Get(e.srv.URL + p)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, 200, resp.StatusCode, p)
	}
}

func readEvents(t *testing.T, r io.Reader) []string {
	t.Helper()
	var events []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	deadline := time.Now().Add(60 * time.Second)
	for sc.Scan() && time.Now().Before(deadline) {
		if ev, ok := strings.CutPrefix(sc.Text(), "event: "); ok {
			events = append(events, ev)
		}
	}
	require.NotEmpty(t, events)
	return events
}

func count(xs []string, s string) int {
	n := 0
	for _, x := range xs {
		if x == s {
			n++
		}
	}
	return n
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 30s")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
