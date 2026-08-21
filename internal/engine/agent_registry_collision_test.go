package engine

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureWarnings redirects the default slog logger into a buffer for the
// duration of a test. The registry warns through the package-level logger (it
// has no injected one), so this is the only seam.
func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

func collisionAgent(name, model string) *types.AgentDefinition {
	return &types.AgentDefinition{
		APIVersion: "mockagents/v1",
		Kind:       "Agent",
		Metadata:   types.Metadata{Name: name},
		Spec: types.AgentSpec{
			Protocol: "openai-chat-completions",
			Model:    model,
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{{
					Name:     "default",
					Response: types.ScenarioResponse{Content: "hi"},
				}},
			},
		},
	}
}

// TestRegister_TwoFilesOneNameWarns is the guard for the silent half of the
// collision pair. The model-collision warning has existed since round-9; a NAME
// collision said nothing, so an agent could vanish at load time with no signal.
//
// Recursive directory scanning made this reachable in normal use: organizing
// agents into subdirectories is the feature's own motivation, and two folders
// naming an agent `support` is the obvious way to hit it.
func TestRegister_TwoFilesOneNameWarns(t *testing.T) {
	buf := captureWarnings(t)
	r := NewAgentRegistry()

	r.RegisterWithSource(collisionAgent("support", "gpt-team-a"), "/agents/team-a/support.yaml")
	r.RegisterWithSource(collisionAgent("support", "gpt-team-b"), "/agents/team-b/support.yaml")

	out := buf.String()
	require.Contains(t, out, "agent name claimed by multiple files")
	assert.Contains(t, out, "agent=support")
	// Both files are named, so the reader does not have to go hunting.
	assert.Contains(t, out, "team-b/support.yaml", "the winner should be named")
	assert.Contains(t, out, "team-a/support.yaml", "the shadowed file should be named")

	// And the warning is true: one agent survives, under the later file's model.
	require.Equal(t, 1, r.Count())
	assert.Equal(t, "gpt-team-b", r.Get("support").Spec.Model)
}

// TestRegister_ShadowedAgentLosesItsModelToo pins the consequence that makes
// this worth a warning rather than a shrug. The replaced agent is unreachable
// by name AND its model is dropped from the index, so a request for it does not
// 404 — it falls through to whatever the resolver picks next.
func TestRegister_ShadowedAgentLosesItsModelToo(t *testing.T) {
	captureWarnings(t)
	r := NewAgentRegistry()

	r.RegisterWithSource(collisionAgent("support", "gpt-team-a"), "/agents/team-a/support.yaml")
	r.RegisterWithSource(collisionAgent("support", "gpt-team-b"), "/agents/team-b/support.yaml")

	assert.Nil(t, r.GetByModel("gpt-team-a"), "the shadowed agent's model should resolve to nothing")
	assert.NotNil(t, r.GetByModel("gpt-team-b"))
}

// TestRegister_SameFileReloadIsSilent: a hot reload (watcher.go), a
// POST /reload, and a PUT that overwrites an agent in place all re-register the
// SAME path on purpose. Warning there would fire on every file save, which is
// how a warning gets tuned out.
func TestRegister_SameFileReloadIsSilent(t *testing.T) {
	buf := captureWarnings(t)
	r := NewAgentRegistry()

	const path = "/agents/support.yaml"
	r.RegisterWithSource(collisionAgent("support", "gpt-4o"), path)
	r.RegisterWithSource(collisionAgent("support", "gpt-4o-mini"), path) // edited and reloaded

	assert.Empty(t, buf.String(), "re-registering the same file must not warn")
	assert.Equal(t, "gpt-4o-mini", r.Get("support").Spec.Model, "the reload should still take effect")
}

// TestRegister_InMemoryRegistrationIsSilent: an agent with no backing file (the
// write API without an agents directory, or a test) has no path to compare, so
// there is nothing to claim a collision about.
func TestRegister_InMemoryRegistrationIsSilent(t *testing.T) {
	buf := captureWarnings(t)
	r := NewAgentRegistry()

	r.Register(collisionAgent("support", "gpt-4o"))
	r.Register(collisionAgent("support", "gpt-4o-mini"))
	r.RegisterWithSource(collisionAgent("support", "gpt-4o"), "/agents/support.yaml")

	assert.Empty(t, buf.String(), "registrations without a known source must not warn")
}

// TestRegister_DifferentTenantsSameNameIsSilent: two tenants may each own an
// agent called `support` — that is the tenancy model working, not a collision.
func TestRegister_DifferentTenantsSameNameIsSilent(t *testing.T) {
	buf := captureWarnings(t)
	r := NewAgentRegistry()

	a := collisionAgent("support", "gpt-team-a")
	a.Metadata.TenantID = "ten_a"
	b := collisionAgent("support", "gpt-team-b")
	b.Metadata.TenantID = "ten_b"

	r.RegisterWithSource(a, "/agents/a/support.yaml")
	r.RegisterWithSource(b, "/agents/b/support.yaml")

	assert.Empty(t, buf.String(), "per-tenant agents of the same name are not a collision")
	assert.Equal(t, 2, r.Count())
}

// TestRegister_SourceIsStillRecorded guards the reordering this change made:
// the collision check reads the PREVIOUS source, so recording the new one moved
// to the end of registerLocked. If it ever moves back above the check, the
// warning goes silent — and Source(), which the write API uses to overwrite the
// right file, must keep working either way.
func TestRegister_SourceIsStillRecorded(t *testing.T) {
	captureWarnings(t)
	r := NewAgentRegistry()

	r.RegisterWithSource(collisionAgent("support", "gpt-4o"), "/agents/nested/support.yaml")
	assert.Equal(t, "/agents/nested/support.yaml", r.Source("support", ""))

	r.RegisterWithSource(collisionAgent("support", "gpt-4o"), "/agents/moved/support.yaml")
	assert.Equal(t, "/agents/moved/support.yaml", r.Source("support", ""),
		"the latest registration's source should win")
}
