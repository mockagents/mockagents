package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/engine"
)

// UX-03 slice A: the additive conditional-write contract for agents.
//
// The cases here are the ones epic §8.2 names explicitly: concurrent API
// writes, reload, deletion, external source-file edits, a failed disk write,
// and the legacy unconditional writer that conditional writes do NOT protect
// against.

// revAgent builds a valid agent document whose content varies, so successive
// revisions are genuinely different bytes. Shape matches agentYAML in
// agent_write_handlers_test.go — the schema the validator actually accepts.
const revisionAgentYAML = `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: rev-agent
  description: %s
spec:
  protocol: openai-chat-completions
  model: gpt-4o
  behavior:
    scenarios:
      - name: default
        response:
          content: "%s"
`

func revAgent(description, reply string) string {
	return fmt.Sprintf(revisionAgentYAML, description, reply)
}

type revEnv struct {
	h   *Handlers
	dir string
}

func newRevEnv(t *testing.T) *revEnv {
	t.Helper()
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	h := &Handlers{
		Engine:    engine.NewEngine(engine.NewAgentRegistry(), nil, logger),
		AgentsDir: dir,
		Logger:    logger,
	}
	return &revEnv{h: h, dir: dir}
}

// put issues PUT /api/v1/agents/{name} with optional conditional headers.
func (e *revEnv) put(t *testing.T, name, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/agents/"+name, strings.NewReader(body))
	req.SetPathValue("name", name)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.h.PutAgent(rec, req)
	return rec
}

func (e *revEnv) get(t *testing.T, name string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/"+name, nil)
	req.SetPathValue("name", name)
	rec := httptest.NewRecorder()
	e.h.GetAgent(rec, req)
	return rec
}

func (e *revEnv) del(t *testing.T, name string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/agents/"+name, nil)
	req.SetPathValue("name", name)
	rec := httptest.NewRecorder()
	e.h.DeleteAgent(rec, req)
	return rec
}

// etagOf reads the current ETag the way a client would: from a GET.
func (e *revEnv) etagOf(t *testing.T, name string) string {
	t.Helper()
	rec := e.get(t, name)
	require.Equal(t, http.StatusOK, rec.Code)
	tag := rec.Header().Get("ETag")
	require.NotEmpty(t, tag, "GET must publish an ETag")
	return tag
}

// seed creates the agent unconditionally and returns its ETag.
func (e *revEnv) seed(t *testing.T) string {
	t.Helper()
	rec := e.put(t, "rev-agent", revAgent("original", "hello"), nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	return e.etagOf(t, "rev-agent")
}

// ---------------------------------------------------------------------------
// The additive guarantee
// ---------------------------------------------------------------------------

// The whole point of "additive": a client that sends no precondition must
// behave exactly as it did before this change. Every existing SDK and the CLI
// depend on this.
func TestAgentRevision_UnconditionalPutStillWorks(t *testing.T) {
	e := newRevEnv(t)

	created := e.put(t, "rev-agent", revAgent("original", "hello"), nil)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())

	updated := e.put(t, "rev-agent", revAgent("second", "hi"), nil)
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
	require.Contains(t, updated.Body.String(), `"status":"updated"`)
}

func TestAgentRevision_GetPublishesRevisionHeaders(t *testing.T) {
	e := newRevEnv(t)
	e.seed(t)

	rec := e.get(t, "rev-agent")
	require.NotEmpty(t, rec.Header().Get("ETag"))
	require.NotEmpty(t, rec.Header().Get(headerRevisionEffective))
	// The agent was persisted, so it has a readable source file too.
	require.NotEmpty(t, rec.Header().Get(headerRevisionSource))
}

func TestAgentRevision_ChangesAfterEveryWrite(t *testing.T) {
	e := newRevEnv(t)
	first := e.seed(t)

	require.Equal(t, http.StatusOK,
		e.put(t, "rev-agent", revAgent("changed", "hello"), map[string]string{"If-Match": first}).Code)

	require.NotEqual(t, first, e.etagOf(t, "rev-agent"),
		"the revision must move after a write, or a stale editor could re-apply forever")
}

// ---------------------------------------------------------------------------
// YAML representation (slice B: what the editor loads)
// ---------------------------------------------------------------------------

func (e *revEnv) getAccept(t *testing.T, name, accept string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/"+name, nil)
	req.SetPathValue("name", name)
	req.Header.Set("Accept", accept)
	rec := httptest.NewRecorder()
	e.h.GetAgent(rec, req)
	return rec
}

func TestAgentGet_ServesYAMLWhenAsked(t *testing.T) {
	e := newRevEnv(t)
	e.seed(t)

	rec := e.getAccept(t, "rev-agent", "application/yaml")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "yaml")

	body := rec.Body.String()
	require.Contains(t, body, "kind: Agent")
	require.Contains(t, body, "name: rev-agent")
	require.NotContains(t, body, `"kind"`, "must not be JSON")

	// The revision headers come along, so an editor gets the document and the
	// precondition it needs in one request.
	require.NotEmpty(t, rec.Header().Get("ETag"))
}

// Content negotiation must be additive: anything that is not an explicit YAML
// media type keeps the JSON default.
func TestAgentGet_DefaultsToJSON(t *testing.T) {
	e := newRevEnv(t)
	e.seed(t)

	for _, accept := range []string{"", "*/*", "application/json", "text/html,application/xhtml+xml"} {
		rec := e.getAccept(t, "rev-agent", accept)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, rec.Header().Get("Content-Type"), "json",
			"Accept %q must still get JSON", accept)
	}
}

// What the editor loads must be exactly what a conditional PUT will store, or
// the diff it shows is a lie.
func TestAgentGet_YAMLRoundTripsThroughConditionalPut(t *testing.T) {
	e := newRevEnv(t)
	e.seed(t)

	loaded := e.getAccept(t, "rev-agent", "application/yaml").Body.String()
	tag := e.etagOf(t, "rev-agent")

	// Re-applying the untouched document is accepted...
	rec := e.put(t, "rev-agent", loaded, map[string]string{"If-Match": tag})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// ...and produces a byte-identical document, so an unmodified save is a
	// no-op rather than a silent rewrite.
	require.Equal(t, loaded, e.getAccept(t, "rev-agent", "application/yaml").Body.String())
}

// ---------------------------------------------------------------------------
// Concurrent API writes — the two-tab case
// ---------------------------------------------------------------------------

func TestAgentRevision_StaleIfMatchIsRejected(t *testing.T) {
	e := newRevEnv(t)
	stale := e.seed(t)

	// Tab A applies first and wins.
	require.Equal(t, http.StatusOK,
		e.put(t, "rev-agent", revAgent("tab-a", "from-a"), map[string]string{"If-Match": stale}).Code)

	// Tab B still holds the revision it loaded, and must be refused.
	rec := e.put(t, "rev-agent", revAgent("tab-b", "from-b"), map[string]string{"If-Match": stale})
	require.Equal(t, http.StatusPreconditionFailed, rec.Code)
	require.Contains(t, rec.Body.String(), "changed since it was loaded")

	// Neither draft is destroyed: the server still holds A's version, and B's
	// body was never applied. B's own draft lives in its client.
	require.Contains(t, e.get(t, "rev-agent").Body.String(), "from-a")
	require.NotContains(t, e.get(t, "rev-agent").Body.String(), "from-b")

	// The refusal carries the CURRENT revision, so B can rebase in one step.
	require.NotEmpty(t, rec.Header().Get("ETag"))
	require.NotEqual(t, stale, rec.Header().Get("ETag"))

	// And rebasing onto it succeeds.
	require.Equal(t, http.StatusOK, e.put(t, "rev-agent", revAgent("tab-b", "from-b"),
		map[string]string{"If-Match": rec.Header().Get("ETag")}).Code)
}

func TestAgentRevision_IfMatchStarRequiresExistence(t *testing.T) {
	e := newRevEnv(t)

	// Nothing there yet: "*" cannot match.
	rec := e.put(t, "rev-agent", revAgent("x", "y"), map[string]string{"If-Match": "*"})
	require.Equal(t, http.StatusPreconditionFailed, rec.Code)

	e.seed(t)
	require.Equal(t, http.StatusOK,
		e.put(t, "rev-agent", revAgent("x", "y"), map[string]string{"If-Match": "*"}).Code)
}

// ---------------------------------------------------------------------------
// Create must not overwrite
// ---------------------------------------------------------------------------

func TestAgentRevision_IfNoneMatchStarRefusesToOverwrite(t *testing.T) {
	e := newRevEnv(t)

	// Create-only on a fresh name succeeds.
	rec := e.put(t, "rev-agent", revAgent("first", "one"), map[string]string{"If-None-Match": "*"})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	// The same request again must NOT clobber the existing definition.
	rec = e.put(t, "rev-agent", revAgent("second", "two"), map[string]string{"If-None-Match": "*"})
	require.Equal(t, http.StatusPreconditionFailed, rec.Code)
	require.Contains(t, rec.Body.String(), "already exists")

	require.Contains(t, e.get(t, "rev-agent").Body.String(), "one", "the original must survive")
}

func TestAgentRevision_IfNoneMatchRejectsNonStar(t *testing.T) {
	e := newRevEnv(t)
	tag := e.seed(t)

	// Only "*" is meaningful here; a specific tag would silently mean something
	// other than what a caller expects, so it is refused rather than guessed.
	rec := e.put(t, "rev-agent", revAgent("x", "y"), map[string]string{"If-None-Match": tag})
	require.Equal(t, http.StatusPreconditionFailed, rec.Code)
	require.Contains(t, rec.Body.String(), "create-only")
}

// ---------------------------------------------------------------------------
// Deletion and reload invalidate a stale edit
// ---------------------------------------------------------------------------

func TestAgentRevision_DeleteInvalidatesInFlightEdit(t *testing.T) {
	e := newRevEnv(t)
	tag := e.seed(t)

	require.Equal(t, http.StatusOK, e.del(t, "rev-agent").Code)

	rec := e.put(t, "rev-agent", revAgent("edited", "late"), map[string]string{"If-Match": tag})
	require.Equal(t, http.StatusPreconditionFailed, rec.Code)
	require.Contains(t, rec.Body.String(), "deleted")

	// The editor is told to decide explicitly; the agent is NOT silently
	// resurrected by an in-flight edit.
	require.Equal(t, http.StatusNotFound, e.get(t, "rev-agent").Code)
}

// An out-of-band edit to the YAML on disk must invalidate a conditional write,
// even though the running definition is untouched. Without this, applying an
// edit would silently discard whatever was written to the file.
func TestAgentRevision_ExternalSourceEditInvalidatesEdit(t *testing.T) {
	e := newRevEnv(t)
	tag := e.seed(t)

	src := e.h.Engine.Registry.Source("rev-agent", "")
	require.NotEmpty(t, src, "the seeded agent should have a source file")

	// Someone edits the file directly — the registry still serves the old one.
	require.NoError(t, os.WriteFile(src, []byte(revAgent("edited-on-disk", "from-disk")), 0o600))

	rec := e.put(t, "rev-agent", revAgent("from-editor", "from-editor"), map[string]string{"If-Match": tag})
	require.Equal(t, http.StatusPreconditionFailed, rec.Code)
	require.Contains(t, rec.Body.String(), "edited on disk")

	// The two revisions are reported separately, so a UI can say WHICH drifted.
	get := e.get(t, "rev-agent")
	require.NotEqual(t, get.Header().Get(headerRevisionEffective), get.Header().Get(headerRevisionSource),
		"effective and source revisions must differ after an out-of-band edit")
}

// Regression: a hand-authored YAML file never matches canonical marshaled
// output byte for byte — comments, key order and unset defaults all differ —
// so comparing raw file bytes against the effective revision reported "edited
// on disk" for every agent ever loaded from disk. Drift must compare CANONICAL
// forms, so an untouched file reports no drift.
func TestAgentRevision_HandAuthoredFileIsNotReportedAsDrifted(t *testing.T) {
	e := newRevEnv(t)

	// A file as a human would write it: comments, a blank line, no defaults.
	handAuthored := `# A hand-written agent.
apiVersion: mockagents/v1
kind: Agent

metadata:
  name: rev-agent          # trailing comment
spec:
  protocol: openai-chat-completions
  model: gpt-4o
  behavior:
    scenarios:
      - name: default
        response:
          content: "hi"
`
	src := filepath.Join(e.dir, "rev-agent.yaml")
	require.NoError(t, os.WriteFile(src, []byte(handAuthored), 0o600))

	res, err := config.LoadFile(src)
	require.NoError(t, err)
	config.ApplyDefaults(res.Definition)
	e.h.Engine.Registry.RegisterWithSource(res.Definition, src)

	rev, ok, err := e.h.agentRevisionFor("rev-agent", "")
	require.NoError(t, err)
	require.True(t, ok)

	require.NotEqual(t, rev.Effective, rev.Source,
		"precondition: raw bytes should differ from canonical output")
	require.False(t, rev.drift(),
		"an untouched hand-authored file must not be reported as edited on disk")
	require.False(t, rev.SourceUnparseable)

	// And the 412 for a genuinely stale edit must not blame the file.
	rec := e.put(t, "rev-agent", revAgent("x", "y"), map[string]string{"If-Match": `"deadbeef"`})
	require.Equal(t, http.StatusPreconditionFailed, rec.Code)
	require.NotContains(t, rec.Body.String(), "edited on disk")
	require.Contains(t, rec.Body.String(), "changed since it was loaded")
}

// A source file that no longer parses cannot be compared, so drift is UNKNOWN
// rather than absent — and the conflict message says so.
func TestAgentRevision_UnparseableSourceIsReportedAsUnknown(t *testing.T) {
	e := newRevEnv(t)
	tag := e.seed(t)

	src := e.h.Engine.Registry.Source("rev-agent", "")
	require.NoError(t, os.WriteFile(src, []byte("{{ not: [valid yaml"), 0o600))

	rev, ok, err := e.h.agentRevisionFor("rev-agent", "")
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, rev.SourceUnparseable)
	require.False(t, rev.drift(), "unknown must not be reported as drift")

	rec := e.put(t, "rev-agent", revAgent("x", "y"), map[string]string{"If-Match": tag})
	require.Equal(t, http.StatusPreconditionFailed, rec.Code)
	require.Contains(t, rec.Body.String(), "no longer parses")
}

func TestAgentRevision_MissingSourceFileIsReported(t *testing.T) {
	e := newRevEnv(t)
	tag := e.seed(t)

	src := e.h.Engine.Registry.Source("rev-agent", "")
	require.NoError(t, os.Remove(src))

	rec := e.put(t, "rev-agent", revAgent("x", "y"), map[string]string{"If-Match": tag})
	require.Equal(t, http.StatusPreconditionFailed, rec.Code)
	require.Contains(t, rec.Body.String(), "no longer has a readable source file")
}

// ---------------------------------------------------------------------------
// Durability honesty
// ---------------------------------------------------------------------------

// A failed disk write must not report a durable save (epic §8.2).
func TestAgentRevision_FailedDiskWriteIsNotReportedAsSaved(t *testing.T) {
	e := newRevEnv(t)

	// Point the agents directory at a path that cannot be written to, so the
	// atomic write fails.
	e.h.AgentsDir = filepath.Join(e.dir, "not-a-directory")
	require.NoError(t, os.WriteFile(e.h.AgentsDir, []byte("i am a file"), 0o600))

	rec := e.put(t, "rev-agent", revAgent("x", "y"), nil)
	require.GreaterOrEqual(t, rec.Code, 500, "a failed write must be an error, not a success")
	require.NotContains(t, rec.Body.String(), `"persisted":true`)
	require.NotContains(t, rec.Body.String(), `"status":"created"`)
}

// With no agents directory the save is real but RUNTIME-ONLY, and must say so:
// a restart loses it.
func TestAgentRevision_RuntimeOnlySaveIsLabelled(t *testing.T) {
	e := newRevEnv(t)
	e.h.AgentsDir = ""

	rec := e.put(t, "rev-agent", revAgent("x", "y"), nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"persisted":false`)

	// It is serving, and it still has a revision to edit against.
	require.Equal(t, http.StatusOK, e.get(t, "rev-agent").Code)
	require.NotEmpty(t, e.etagOf(t, "rev-agent"))
}

// ---------------------------------------------------------------------------
// The advertised limit
// ---------------------------------------------------------------------------

// Conditional writes order only the callers who USE them. A legacy client that
// sends no If-Match still overwrites, because keeping the route additive is
// precisely what preserves those clients. This test exists so the limit is
// pinned and documented rather than discovered later in production.
func TestAgentRevision_LegacyUnconditionalWriterCanStillClobber(t *testing.T) {
	e := newRevEnv(t)
	tag := e.seed(t)

	// A legacy writer (no precondition) overwrites without consulting anyone.
	require.Equal(t, http.StatusOK, e.put(t, "rev-agent", revAgent("legacy", "from-legacy"), nil).Code)
	require.Contains(t, e.get(t, "rev-agent").Body.String(), "from-legacy")

	// The conditional writer that held `tag` is at least TOLD, rather than
	// silently losing its work: it gets a 412, not a false success.
	rec := e.put(t, "rev-agent", revAgent("careful", "from-careful"),
		map[string]string{"If-Match": tag})
	require.Equal(t, http.StatusPreconditionFailed, rec.Code,
		"a conditional writer must still detect that a legacy writer moved the agent")
}
