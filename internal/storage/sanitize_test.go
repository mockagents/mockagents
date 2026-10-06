package storage

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeBody_MultipleKeys(t *testing.T) {
	out := SanitizeBody(`{"a":"sk-aaa111","b":"sk-bbb222"}`)
	assert.NotContains(t, out, "aaa111", "first key must be masked")
	assert.NotContains(t, out, "bbb222", "second key must also be masked")
	assert.Equal(t, 2, strings.Count(out, "sk-***"))
}

func TestSanitizeBody_MultiplePatterns(t *testing.T) {
	out := SanitizeBody(`{"auth":"Bearer tok999","key":"sk-abc"}`)
	assert.NotContains(t, out, "tok999")
	assert.NotContains(t, out, "abc")
	assert.Contains(t, out, "Bearer ***")
	assert.Contains(t, out, "sk-***")
}

func TestSanitizeBody_Idempotent(t *testing.T) {
	for _, in := range []string{
		`{"a":"sk-aaa","b":"sk-bbb"}`,
		`Authorization: Bearer sk-real-key`,
		`{"a":"sk-***","b":"sk-real"}`, // already-masked + a fresh key
		`{"model":"gpt-4o"}`,
	} {
		once := SanitizeBody(in)
		twice := SanitizeBody(once)
		assert.Equalf(t, once, twice, "SanitizeBody not idempotent for %q", in)
	}
}

func TestSanitizeBody_AnthropicKey(t *testing.T) {
	// sk-ant- keys are caught by the sk- prefix.
	out := SanitizeBody(`{"key":"sk-ant-api03-secretvalue"}`)
	assert.NotContains(t, out, "secretvalue")
	assert.Contains(t, out, "sk-***")
}

func TestSanitizeBody_AlreadyMaskedUnchanged(t *testing.T) {
	in := `{"key":"sk-***"}`
	assert.Equal(t, in, SanitizeBody(in))
}

func TestSanitizeBody_TrailingSecretAfterStars(t *testing.T) {
	// A real secret whose value happens to start with "***" must NOT be treated
	// as already-masked — the bytes after the stars must still be scrubbed.
	out := SanitizeBody(`{"key":"sk-***moresecret"}`)
	assert.NotContains(t, out, "moresecret")
	assert.Contains(t, out, "sk-***")
	// And the fix stays idempotent.
	assert.Equal(t, out, SanitizeBody(out))
}

// Prose with hyphenated words is left alone; real keys are still masked
// (review P-14).
func TestSanitizeBody_DoesNotRedactOrdinaryWords(t *testing.T) {
	prose := `a risk-based task-list for the turkey-dinner desk-lamp, a key-value store`
	assert.Equal(t, prose, SanitizeBody(prose))

	// Built by concatenation so secret scanners do not mistake the fixture
	// for a leaked credential.
	fakeKey := strings.Repeat("0f1e2d3c", 4)
	out := SanitizeBody(`{"a":"sk-proj-abc123","b":"key-` + fakeKey + `"}`)
	assert.NotContains(t, out, "abc123")
	assert.NotContains(t, out, fakeKey)
}

// Timestamps are stored fixed-width so the lexical Since/Until filters order
// rows by time (review P-19), and a blank timestamp becomes now (L-34).
func TestNormalizeTimestamp(t *testing.T) {
	a := NormalizeTimestamp("2026-10-06T10:00:00.5Z")
	b := NormalizeTimestamp("2026-10-06T10:00:00Z")
	c := NormalizeTimestamp("2026-10-06T10:00:00.123Z")
	assert.Equal(t, "2026-10-06T10:00:00.500000000Z", a)
	assert.True(t, b < c && c < a, "lexical order must equal time order: %s %s %s", b, c, a)
	assert.Equal(t, "2026-10-06T08:00:00.000000000Z", NormalizeTimestamp("2026-10-06T10:00:00+02:00"))
	assert.NotEmpty(t, NormalizeTimestamp(""))
	assert.Equal(t, "not a time", NormalizeTimestamp("not a time"))
}
