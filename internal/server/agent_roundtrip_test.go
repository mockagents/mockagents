package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/types"
)

// UX-03 slice C: round-trip verification.
//
// Epic §8.2 requires proving that reading a definition, writing it back, and
// reading it again does not quietly lose anything — across "scenarios,
// defaults, nested fields, tool schemas and Unicode". §10 adds that backend,
// CLI and GUI fixtures must agree on canonical decisions.
//
// The strongest available evidence is the project's own corpus: every shipped
// example is loaded exactly as startup loads it, then pushed through the real
// GET → conditional PUT → GET path. Two properties are asserted:
//
//  1. Canonical form is a FIXED POINT. A save that changes nothing must
//     produce a byte-identical document, or an editor would show phantom
//     diffs and every save would churn the revision.
//
//  2. Nothing is dropped. Every scalar leaf in the ORIGINAL hand-authored file
//     must still be present afterwards. This is the assertion that would catch
//     a field the Go types do not model being silently discarded — the exact
//     failure §8.2 is about.

// leaves flattens a parsed YAML document into dotted-path → scalar pairs, so
// two documents can be compared by meaning rather than by formatting.
//
// Sequences are indexed. Maps and sequences themselves are not emitted: only
// the scalars at the leaves, which is what "no data was lost" means here.
func leaves(node any, prefix string, out map[string]string) {
	switch v := node.(type) {
	case map[string]any:
		for key, child := range v {
			leaves(child, join(prefix, key), out)
		}
	case []any:
		for i, child := range v {
			leaves(child, join(prefix, fmt.Sprint(i)), out)
		}
	case nil:
		out[prefix] = "null"
	default:
		out[prefix] = fmt.Sprint(v)
	}
}

func join(prefix, seg string) string {
	if prefix == "" {
		return seg
	}
	return prefix + "." + seg
}

func leavesOf(t *testing.T, doc []byte) map[string]string {
	t.Helper()
	var parsed any
	require.NoError(t, yaml.Unmarshal(doc, &parsed), "parsing document")
	out := map[string]string{}
	leaves(normalize(parsed), "", out)
	return out
}

// normalize converts yaml.v3's map[string]interface{} shapes into a form the
// walker can traverse uniformly.
func normalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, val := range t {
			m[k] = normalize(val)
		}
		return m
	case map[any]any:
		m := make(map[string]any, len(t))
		for k, val := range t {
			m[fmt.Sprint(k)] = normalize(val)
		}
		return m
	case []any:
		s := make([]any, len(t))
		for i, val := range t {
			s[i] = normalize(val)
		}
		return s
	default:
		return v
	}
}

// exampleAgents returns every shipped example that is an Agent document.
func exampleAgents(t *testing.T) []*config.LoadResult {
	t.Helper()
	results, errs := config.LoadDir(filepath.Join("..", "..", "examples"))
	for _, err := range errs {
		t.Logf("loader reported: %v", err)
	}
	require.NotEmpty(t, results, "expected shipped example agents")
	return results
}

// seedFromFile registers a definition the way startup does — parsed from disk,
// defaults applied, source path recorded — into a fresh environment.
func seedFromFile(t *testing.T, res *config.LoadResult) *revEnv {
	t.Helper()
	e := newRevEnv(t)

	// Copy the file so a write cannot touch the repository's examples.
	data, err := os.ReadFile(res.FilePath)
	require.NoError(t, err)
	dst := filepath.Join(e.dir, filepath.Base(res.FilePath))
	require.NoError(t, os.WriteFile(dst, data, 0o600))

	def := res.Definition
	config.ApplyDefaults(def)
	e.h.Engine.Registry.RegisterWithSource(def, dst)
	return e
}

// TestRoundTrip_EveryShippedExample is the corpus-wide proof.
func TestRoundTrip_EveryShippedExample(t *testing.T) {
	for _, res := range exampleAgents(t) {
		name := res.Definition.Metadata.Name
		t.Run(filepath.Base(res.FilePath), func(t *testing.T) {
			original, err := os.ReadFile(res.FilePath)
			require.NoError(t, err)

			e := seedFromFile(t, res)

			// 1. Read the canonical document and its revision.
			first := e.getAccept(t, name, "application/yaml")
			require.Equal(t, http.StatusOK, first.Code, first.Body.String())
			doc1 := first.Body.String()
			etag := strings.Trim(first.Header().Get("ETag"), `"`)
			require.NotEmpty(t, etag)

			// 2. Write it back unchanged, conditionally — which also runs the
			//    strict unknown-field check. A shipped example must survive it.
			put := e.put(t, name, doc1, map[string]string{"If-Match": `"` + etag + `"`})
			require.Equal(t, http.StatusOK, put.Code,
				"re-applying an unmodified definition must be accepted: %s", put.Body.String())

			// 3. Read it again.
			doc2 := e.getAccept(t, name, "application/yaml").Body.String()

			// Property 1: canonical form is a fixed point.
			require.Equal(t, doc1, doc2,
				"a no-op save changed the document; an editor would show phantom diffs")

			// Property 2: nothing from the hand-authored file was dropped.
			before := leavesOf(t, original)
			after := leavesOf(t, []byte(doc2))
			assertNoLeafLost(t, before, after)
		})
	}
}

// knownRoundTripDefects are values this corpus is CURRENTLY known not to
// round-trip, each with the reason. They are carved out here — loudly, one line
// each — rather than weakening the assertion, so the list is the project's
// standing inventory of round-trip data loss and shrinks to nothing as the
// causes are fixed.
//
// Remove an entry the moment its cause is fixed: the entry then makes the test
// fail (see TestRoundTrip_KnownDefectsStillReproduce), which is the point.
var knownRoundTripDefects = map[string]string{
	// config.ApplyDefaults treats ChunkDelayMs == 0 as "unset" and overwrites it
	// with 50, so an author cannot express "stream with no delay" — even though
	// the JSON schema documents `minimum: 0` and every streaming path already
	// honours an explicit 0 (`if ChunkDelayMs >= 0`). examples/gemini-agent.yaml
	// asks for 0 and the server runs it at 50.
	//
	// Fixing it means distinguishing unset from zero (a *int in types), which
	// changes streaming timing behaviour and touches ~16 files — a change of its
	// own, not part of round-trip verification.
	"spec.behavior.streaming.chunk_delay_ms": "ApplyDefaults clobbers an explicit 0 with 50",
}

func assertNoLeafLost(t *testing.T, before, after map[string]string) {
	t.Helper()
	var lost []string
	for path, want := range before {
		got, ok := after[path]
		if !ok {
			lost = append(lost, fmt.Sprintf("%s (was %q) — MISSING", path, want))
			continue
		}
		if got != want {
			if reason, known := knownRoundTripDefects[path]; known {
				t.Logf("KNOWN DEFECT %s: %q -> %q (%s)", path, want, got, reason)
				continue
			}
			lost = append(lost, fmt.Sprintf("%s: %q -> %q", path, want, got))
		}
	}
	if len(lost) > 0 {
		sort.Strings(lost)
		t.Errorf("round trip lost or changed %d value(s):\n  %s", len(lost), strings.Join(lost, "\n  "))
	}
}

// TestRoundTrip_KnownDefectsStillReproduce pins each carve-out to the exact
// behaviour that justifies it. If someone fixes the defect without removing the
// entry above, this fails and points at the stale carve-out — so the allowlist
// cannot quietly outlive its reason.
func TestRoundTrip_KnownDefectsStillReproduce(t *testing.T) {
	require.Len(t, knownRoundTripDefects, 1,
		"update this test when the known-defect list changes")

	def := &types.AgentDefinition{}
	def.Spec.Behavior.Streaming = &types.StreamingConfig{ChunkDelayMs: 0}
	config.ApplyDefaults(def)

	require.Equal(t, 50, def.Spec.Behavior.Streaming.ChunkDelayMs,
		"an explicit chunk_delay_ms: 0 is still being overwritten. If this now "+
			"reports 0, the defect is FIXED — delete the matching entry from "+
			"knownRoundTripDefects and this test's expectation.")
}

// Repeated no-op saves must converge. If applying defaults were not idempotent,
// every save would move the revision and two editors could never agree.
//
// The FIRST save of a hand-authored file legitimately moves the revision: the
// file on disk is rewritten from its authored form into canonical form, so the
// source really did change. Every save after that must be inert.
func TestRoundTrip_RepeatedSavesConverge(t *testing.T) {
	res := exampleAgents(t)[0]
	name := res.Definition.Metadata.Name
	e := seedFromFile(t, res)

	authoredTag := strings.Trim(e.get(t, name).Header().Get("ETag"), `"`)
	doc := e.getAccept(t, name, "application/yaml").Body.String()

	// Save 1: canonicalizes the file on disk.
	require.Equal(t, http.StatusOK,
		e.put(t, name, doc, map[string]string{"If-Match": `"` + authoredTag + `"`}).Code)

	canonicalTag := strings.Trim(e.get(t, name).Header().Get("ETag"), `"`)
	require.NotEqual(t, authoredTag, canonicalTag,
		"the first save rewrites the authored file canonically, so the revision must move")
	require.Equal(t, doc, e.getAccept(t, name, "application/yaml").Body.String(),
		"canonicalizing the file must not change the document the server serves")

	// Saves 2..4: nothing changes, including the revision.
	prev, prevTag := doc, canonicalTag
	for i := 2; i <= 4; i++ {
		put := e.put(t, name, prev, map[string]string{"If-Match": `"` + prevTag + `"`})
		require.Equal(t, http.StatusOK, put.Code, put.Body.String())

		next := e.getAccept(t, name, "application/yaml").Body.String()
		nextTag := strings.Trim(e.get(t, name).Header().Get("ETag"), `"`)

		require.Equal(t, prev, next, "save %d changed the document", i)
		require.Equal(t, prevTag, nextTag,
			"save %d moved the revision without changing anything — a stale-edit false alarm", i)
		prev, prevTag = next, nextTag
	}
}

// §8.2 names Unicode explicitly. Round-tripping through Go's YAML marshaller
// must not escape, re-encode or truncate it.
func TestRoundTrip_Unicode(t *testing.T) {
	cases := map[string]string{
		"emoji":            "Handles 🎉 and 🚀 fine",
		"cjk":              "日本語のテキストと中文",
		"accents":          "Café — naïve résumé, Ünïcödé",
		"rtl":              "مرحبا بالعالم",
		"combining":        "é vs é",
		"zero_width_join":  "family: 👨‍👩‍👧",
		"quotes_and_colon": `He said: "it's fine" — really`,
	}

	for label, text := range cases {
		t.Run(label, func(t *testing.T) {
			e := newRevEnv(t)
			doc := fmt.Sprintf(`apiVersion: mockagents/v1
kind: Agent
metadata:
  name: rev-agent
  description: %q
spec:
  protocol: openai-chat-completions
  model: gpt-4o
  behavior:
    scenarios:
      - name: default
        response:
          content: %q
`, text, text)

			create := e.put(t, "rev-agent", doc, nil)
			require.Equal(t, http.StatusCreated, create.Code, create.Body.String())

			// Present in the served document...
			served := e.getAccept(t, "rev-agent", "application/yaml").Body.String()
			var parsed map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(served), &parsed))
			got := leavesOf(t, []byte(served))
			require.Equal(t, text, got["metadata.description"])
			require.Equal(t, text, got["spec.behavior.scenarios.0.response.content"])

			// ...and stable across a further conditional save.
			etag := strings.Trim(e.get(t, "rev-agent").Header().Get("ETag"), `"`)
			put := e.put(t, "rev-agent", served, map[string]string{"If-Match": `"` + etag + `"`})
			require.Equal(t, http.StatusOK, put.Code, put.Body.String())
			require.Equal(t, served, e.getAccept(t, "rev-agent", "application/yaml").Body.String())

			// And it reached disk intact, not just memory.
			onDisk, err := os.ReadFile(filepath.Join(e.dir, "rev-agent.yaml"))
			require.NoError(t, err)
			require.Equal(t, text, leavesOf(t, onDisk)["metadata.description"])
		})
	}
}

// Tool definitions carry a nested JSON schema — arbitrary depth, mixed types,
// and required-arrays. §8.2 calls these out because they are the most likely
// thing for a typed round trip to flatten.
func TestRoundTrip_ToolSchema(t *testing.T) {
	e := newRevEnv(t)
	doc := `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: rev-agent
spec:
  protocol: openai-chat-completions
  model: gpt-4o
  tools:
    - name: lookup_order
      description: Look up an order by id
      parameters:
        type: object
        properties:
          order_id:
            type: string
            description: The order identifier
          include_items:
            type: boolean
            default: false
          limit:
            type: integer
            minimum: 1
            maximum: 50
          filters:
            type: array
            items:
              type: string
            uniqueItems: true
        required:
          - order_id
        additionalProperties: false
  behavior:
    scenarios:
      - name: default
        response:
          content: ok
`
	create := e.put(t, "rev-agent", doc, nil)
	require.Equal(t, http.StatusCreated, create.Code, create.Body.String())

	served := e.getAccept(t, "rev-agent", "application/yaml").Body.String()
	before := leavesOf(t, []byte(doc))
	after := leavesOf(t, []byte(served))
	assertNoLeafLost(t, before, after)

	// Spot-check the values most likely to be coerced or dropped.
	require.Equal(t, "false", after["spec.tools.0.parameters.properties.include_items.default"])
	require.Equal(t, "50", after["spec.tools.0.parameters.properties.limit.maximum"])
	require.Equal(t, "true", after["spec.tools.0.parameters.properties.filters.uniqueItems"])
	require.Equal(t, "order_id", after["spec.tools.0.parameters.required.0"])
	require.Equal(t, "false", after["spec.tools.0.parameters.additionalProperties"])

	// Stable on a further conditional save.
	etag := strings.Trim(e.get(t, "rev-agent").Header().Get("ETag"), `"`)
	put := e.put(t, "rev-agent", served, map[string]string{"If-Match": `"` + etag + `"`})
	require.Equal(t, http.StatusOK, put.Code, put.Body.String())
	require.Equal(t, served, e.getAccept(t, "rev-agent", "application/yaml").Body.String())
}

// The GUI's guided form edits one scalar and sends the whole document back.
// This is that path end to end: unrelated configuration must survive a write
// the form produced, on the SERVER, not merely in the browser's text buffer.
func TestRoundTrip_FormStyleSingleFieldEdit(t *testing.T) {
	e := newRevEnv(t)
	doc := `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: rev-agent
  description: before
spec:
  protocol: openai-chat-completions
  model: gpt-4o
  tools:
    - name: lookup_order
      parameters:
        type: object
  behavior:
    chaos:
      enabled: true
      latency:
        distribution: uniform
        min_ms: 100
        max_ms: 300
      errors:
        rate: 0.1
        status_codes: [503, 504]
    scenarios:
      - name: default
        response:
          content: hello
`
	require.Equal(t, http.StatusCreated, e.put(t, "rev-agent", doc, nil).Code)

	served := e.getAccept(t, "rev-agent", "application/yaml").Body.String()
	etag := strings.Trim(e.get(t, "rev-agent").Header().Get("ETag"), `"`)

	// Exactly what lib/yamlPath.writeScalar does: replace one line.
	edited := strings.Replace(served, "description: before", "description: after", 1)
	require.NotEqual(t, served, edited, "the test edit must actually change something")

	put := e.put(t, "rev-agent", edited, map[string]string{"If-Match": `"` + etag + `"`})
	require.Equal(t, http.StatusOK, put.Code, put.Body.String())

	final := leavesOf(t, []byte(e.getAccept(t, "rev-agent", "application/yaml").Body.String()))
	require.Equal(t, "after", final["metadata.description"], "the edit must land")

	// Everything the form does not render is still there, with its values.
	require.Equal(t, "true", final["spec.behavior.chaos.enabled"])
	require.Equal(t, "uniform", final["spec.behavior.chaos.latency.distribution"])
	require.Equal(t, "100", final["spec.behavior.chaos.latency.min_ms"])
	require.Equal(t, "300", final["spec.behavior.chaos.latency.max_ms"])
	require.Equal(t, "0.1", final["spec.behavior.chaos.errors.rate"])
	require.Equal(t, "503", final["spec.behavior.chaos.errors.status_codes.0"])
	require.Equal(t, "504", final["spec.behavior.chaos.errors.status_codes.1"])
	require.Equal(t, "lookup_order", final["spec.tools.0.name"])
}
