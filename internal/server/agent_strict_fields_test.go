package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mockagents/mockagents/internal/config"
)

// UX-03: unknown fields are rejected only for callers that opted into
// conditional writes.

const unknownFieldAgent = `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: rev-agent
spec:
  protocol: openai-chat-completions
  model: gpt-4o
  someFutureFieldTheGuiDoesNotKnow: keep-me-please
  behavior:
    scenarios:
      - name: default
        response:
          content: "hi"
`

// The legacy path must keep working exactly as before, unknown fields and all.
// Breaking it is precisely what the additive contract exists to avoid.
func TestStrictFields_UnconditionalWriteStaysLenient(t *testing.T) {
	e := newRevEnv(t)

	rec := e.put(t, "rev-agent", unknownFieldAgent, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	// Documenting the cost of that leniency: the field IS dropped. This is the
	// behaviour the conditional path exists to protect an editor from.
	data, err := os.ReadFile(filepath.Join(e.dir, "rev-agent.yaml"))
	require.NoError(t, err)
	require.NotContains(t, string(data), "keep-me-please",
		"the lenient path drops unknown fields — that is why strict mode exists")
}

func TestStrictFields_ConditionalWriteRejectsUnknownField(t *testing.T) {
	for _, header := range []map[string]string{
		{"If-None-Match": "*"},
		{"If-Match": "*"},
		{"If-Match": `"whatever"`},
	} {
		rec := newRevEnv(t).put(t, "rev-agent", unknownFieldAgent, header)
		require.Equal(t, http.StatusUnprocessableEntity, rec.Code,
			"precondition %v should enable strict field checking", header)

		body := rec.Body.String()
		require.Contains(t, body, "someFutureFieldTheGuiDoesNotKnow",
			"the response must name the offending field")
		require.Contains(t, body, "unsupported field")
		// Never leak Go type names at the API boundary.
		require.NotContains(t, body, "types.AgentSpec")
	}
}

// Rejection must change nothing: the point is to avoid a write that loses data.
func TestStrictFields_RejectionIsAtomic(t *testing.T) {
	e := newRevEnv(t)
	tag := e.seed(t)

	rec := e.put(t, "rev-agent", unknownFieldAgent, map[string]string{"If-Match": tag})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	// The stored agent is untouched, and its revision has not moved.
	require.Contains(t, e.get(t, "rev-agent").Body.String(), "hello")
	require.Equal(t, tag, e.etagOf(t, "rev-agent"), "a refused write must not move the revision")
}

// A conditional write of a clean document still succeeds — strict mode must not
// reject valid documents.
func TestStrictFields_ConditionalWriteAcceptsKnownFields(t *testing.T) {
	e := newRevEnv(t)

	rec := e.put(t, "rev-agent", revAgent("clean", "hi"), map[string]string{"If-None-Match": "*"})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
}

// A syntax error must still be reported as a bad document, not misreported as
// an unknown field.
func TestStrictFields_SyntaxErrorIsNotReportedAsUnknownField(t *testing.T) {
	e := newRevEnv(t)

	rec := e.put(t, "rev-agent", "{{ this is not: [valid yaml", map[string]string{"If-None-Match": "*"})
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "invalid agent document")
	require.NotContains(t, rec.Body.String(), "unsupported field")
}

func TestStrictFields_ErrorCarriesLineAndSuggestion(t *testing.T) {
	_, errs := decodeAgentStrict([]byte(unknownFieldAgent))
	require.Len(t, errs, 1)
	require.Equal(t, "someFutureFieldTheGuiDoesNotKnow", errs[0].Field)
	require.Positive(t, errs[0].Line, "the error should point at a line the editor can highlight")
	require.NotEmpty(t, errs[0].Suggestion)
}

// The most important guard on this feature: strict decoding must accept every
// agent document the project itself ships. If KnownFields rejects one of our
// own examples, the Go types and the documented schema have diverged and the
// strict path would refuse legitimate configuration.
func TestStrictFields_AcceptsEveryShippedExample(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "examples", "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, paths, "expected to find example agent documents")

	checked := 0
	for _, path := range paths {
		data, err := os.ReadFile(path)
		require.NoError(t, err)

		// Only Agent documents go through this decoder; the directory also
		// holds pipelines, suites and other kinds.
		if !isAgentDocument(data) {
			continue
		}
		checked++

		t.Run(filepath.Base(path), func(t *testing.T) {
			_, errs := decodeAgentStrict(data)
			require.Empty(t, errs, "strict decoding rejected a shipped example: %s", errorSummary(errs))
		})
	}
	require.Positive(t, checked, "no Agent examples were actually checked")
}

func isAgentDocument(data []byte) bool {
	s := string(data)
	if strings.Contains(s, "\nkind: Agent") || strings.HasPrefix(s, "kind: Agent") {
		return true
	}
	// A document with no explicit kind defaults to Agent.
	return !strings.Contains(s, "\nkind:") && !strings.HasPrefix(s, "kind:")
}

func errorSummary(errs []*config.ValidationError) string {
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		parts = append(parts, e.Error())
	}
	return strings.Join(parts, "; ")
}
