package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStripColor(t *testing.T) {
	colored := Red("error") + " " + Green("success") + " " + Yellow("warning")
	stripped := StripColor(colored)
	assert.Equal(t, "error success warning", stripped)
}

func TestStripColor_NoColor(t *testing.T) {
	assert.Equal(t, "plain text", StripColor("plain text"))
}

func TestRed(t *testing.T) {
	result := Red("error")
	// Should contain the text regardless of color mode.
	assert.Contains(t, StripColor(result), "error")
}

func TestGreen(t *testing.T) {
	result := Green("success")
	assert.Contains(t, StripColor(result), "success")
}

func TestYellow(t *testing.T) {
	result := Yellow("warning")
	assert.Contains(t, StripColor(result), "warning")
}

func TestBold(t *testing.T) {
	result := Bold("important")
	assert.Contains(t, StripColor(result), "important")
}

// Color is off when stdout is not a terminal, with NO_COLOR, and after
// DisableColor (the --no-color flag in any spelling) — review C-25.
func TestColorEnabled_Rules(t *testing.T) {
	orig := stdoutIsTerminal
	t.Cleanup(func() { stdoutIsTerminal = orig; colorDisabled.Store(false) })
	t.Setenv("NO_COLOR", "")

	stdoutIsTerminal = func() bool { return false }
	assert.False(t, ColorEnabled(), "redirected output gets no ANSI codes")
	assert.Equal(t, "x", Red("x"))

	stdoutIsTerminal = func() bool { return true }
	assert.True(t, ColorEnabled())

	t.Setenv("NO_COLOR", "1")
	assert.False(t, ColorEnabled())
	t.Setenv("NO_COLOR", "")

	DisableColor()
	assert.False(t, ColorEnabled())
}
