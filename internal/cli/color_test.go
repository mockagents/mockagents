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
