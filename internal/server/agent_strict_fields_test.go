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

// Unknown fields are rejected on every agent write, conditional or not.

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

// An unconditional write is strict too (2026-10-06 quality review, C-04):
// before, the field was dropped and the write reported success.
func TestStrictFields_UnconditionalWriteRejectsUnknownField(t *testing.T) {
	e := newRevEnv(t)

	rec := e.put(t, "rev-agent", unknownFieldAgent, nil)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "someFutureFieldTheGuiDoesNotKnow")

	_, err := os.Stat(filepath.Join(e.dir, "rev-agent.yaml"))
	require.True(t, os.IsNotExist(err), "a refused write must not persist anything")
}

// A second YAML document in the body is refused rather than silently dropped.
func TestStrictFields_MultiDocumentBodyRejected(t *testing.T) {
	body := revAgent("clean", "hi") + "---\nkind: Agent\nmetadata:\n  name: second\n"
	rec := newRevEnv(t).put(t, "rev-agent", body, nil)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "second YAML document")
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
		require.Contains(t, body, "unknown field")
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
	require.NotContains(t, rec.Body.String(), "unknown field")
}

func TestStrictFields_ErrorCarriesLineAndSuggestion(t *testing.T) {
	errs := config.UnknownAgentFields([]byte(unknownFieldAgent))
	require.Len(t, errs, 1)
	require.Equal(t, "spec.someFutureFieldTheGuiDoesNotKnow", errs[0].Field)
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
			errs := config.UnknownAgentFields(data)
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
