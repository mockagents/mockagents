package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/tenancy"
)

// U3-6: the catalog's two new columns. "Does this survive a restart" is not a
// question a count of agents can answer, and three outcomes are genuinely
// different: backed by a file, never had one, or had one that is now gone.
func TestSummarizePersistence(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "present.yaml")
	if err := os.WriteFile(present, []byte("kind: Agent\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("no source path is runtime-only", func(t *testing.T) {
		kind, file := summarizePersistence("")
		if kind != agentRuntimeOnly || file != "" {
			t.Errorf("got (%q, %q), want (%q, \"\")", kind, file, agentRuntimeOnly)
		}
	})

	t.Run("a present file is persisted, named by base name only", func(t *testing.T) {
		kind, file := summarizePersistence(present)
		if kind != agentPersistedToFile {
			t.Errorf("kind = %q, want %q", kind, agentPersistedToFile)
		}
		// The absolute path is server-side detail; a client has no use for it.
		if file != "present.yaml" {
			t.Errorf("file = %q, want the base name", file)
		}
	})

	// The case worth having: a tracked path whose file was deleted out of band
	// still serves from memory. Reporting it as persisted would promise a
	// restart it will not survive.
	t.Run("a tracked file that is gone is neither persisted nor runtime-only", func(t *testing.T) {
		kind, file := summarizePersistence(filepath.Join(dir, "vanished.yaml"))
		if kind != agentFileMissing {
			t.Errorf("kind = %q, want %q", kind, agentFileMissing)
		}
		if file != "vanished.yaml" {
			t.Errorf("file = %q, want the base name", file)
		}
	})
}

// Sources are tracked under an agent's owner. A tenant caller viewing a global
// agent used to look in its own bucket, so the file-backed global agent showed
// as runtime-only and GET omitted its source revision.
func TestGlobalAgentSourceVisibleToTenantCaller(t *testing.T) {
	dir := t.TempDir()
	_, h := newAgentWriteServer(t, dir)
	src := filepath.Join(dir, "shared.yaml")
	if err := os.WriteFile(src, []byte(agentYAML("shared", "shared-model")), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	config.ApplyDefaults(loaded.Definition)
	h.Engine.Registry.RegisterWithSource(loaded.Definition, src)

	asTenant := func(req *http.Request) *http.Request {
		p := &tenancy.Principal{TenantID: "ten_a", KeyID: "k", Role: tenancy.RoleViewer}
		return req.WithContext(tenancy.WithPrincipal(req.Context(), p))
	}

	rec := httptest.NewRecorder()
	h.ListAgents(rec, asTenant(httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)))
	var summaries []AgentSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &summaries); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if len(summaries) != 1 || summaries[0].Persistence != agentPersistedToFile || summaries[0].File != "shared.yaml" {
		t.Fatalf("tenant view of a file-backed global agent = %+v", summaries)
	}

	req := asTenant(httptest.NewRequest(http.MethodGet, "/api/v1/agents/shared", nil))
	req.SetPathValue("name", "shared")
	rec = httptest.NewRecorder()
	h.GetAgent(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d", rec.Code)
	}
	if rec.Header().Get(headerRevisionSource) == "" {
		t.Fatal("GET of a file-backed global agent omits the source revision for a tenant caller")
	}
}
