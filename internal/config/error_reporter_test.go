package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatErrors_Text(t *testing.T) {
	errs := []*ValidationError{
		{
			File:       "agents/test.yaml",
			Line:       10,
			Column:     5,
			Field:      "spec.protocol",
			Message:    "invalid protocol",
			Suggestion: "Use openai-chat-completions.",
		},
	}
	out := FormatErrors(errs, ErrorFormatText)
	assert.Contains(t, out, "agents/test.yaml:10:5")
	assert.Contains(t, out, "spec.protocol")
	assert.Contains(t, out, "invalid protocol")
	assert.Contains(t, out, "Suggestion: Use openai-chat-completions.")
}

func TestFormatErrors_TextNoLineNumber(t *testing.T) {
	errs := []*ValidationError{
		{File: "test.yaml", Field: "kind", Message: "missing"},
	}
	out := FormatErrors(errs, ErrorFormatText)
	assert.Contains(t, out, "test.yaml: kind: missing")
	assert.NotContains(t, out, ":0:")
}

func TestFormatErrors_JSON(t *testing.T) {
	errs := []*ValidationError{
		{
			File:    "test.yaml",
			Line:    5,
			Column:  3,
			Field:   "apiVersion",
			Message: "required",
		},
	}
	out := FormatErrors(errs, ErrorFormatJSON)
	var parsed []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &parsed))
	assert.Len(t, parsed, 1)
	assert.Equal(t, "apiVersion", parsed[0]["field"])
}

func TestFormatErrors_Empty(t *testing.T) {
	assert.Equal(t, "", FormatErrors(nil, ErrorFormatText))
	assert.Equal(t, "", FormatErrors(nil, ErrorFormatJSON))
}

func TestFormatSummary_Success(t *testing.T) {
	s := FormatSummary(3, 0)
	assert.Contains(t, s, "3 file(s)")
	assert.Contains(t, s, "all valid")
}

func TestFormatSummary_Errors(t *testing.T) {
	s := FormatSummary(5, 2)
	assert.Contains(t, s, "5 file(s)")
	assert.Contains(t, s, "2 error(s)")
}
