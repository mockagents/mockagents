package cli

import (
	"fmt"
	"os"
	"strings"
)

// ANSI color codes.
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBold   = "\033[1m"
)

// ColorEnabled returns whether colored output should be used.
// Respects the NO_COLOR environment variable (https://no-color.org/)
// and the --no-color flag.
func ColorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	for _, arg := range os.Args {
		if arg == "--no-color" {
			return false
		}
	}
	return true
}

// Red wraps text in red ANSI color if color is enabled.
func Red(s string) string {
	if !ColorEnabled() {
		return s
	}
	return colorRed + s + colorReset
}

// Green wraps text in green ANSI color if color is enabled.
func Green(s string) string {
	if !ColorEnabled() {
		return s
	}
	return colorGreen + s + colorReset
}

// Yellow wraps text in yellow ANSI color if color is enabled.
func Yellow(s string) string {
	if !ColorEnabled() {
		return s
	}
	return colorYellow + s + colorReset
}

// Bold wraps text in bold ANSI style if color is enabled.
func Bold(s string) string {
	if !ColorEnabled() {
		return s
	}
	return colorBold + s + colorReset
}

// StripColor removes ANSI escape codes from a string.
func StripColor(s string) string {
	result := s
	codes := []string{colorReset, colorRed, colorGreen, colorYellow, colorBold}
	for _, code := range codes {
		result = strings.ReplaceAll(result, code, "")
	}
	return result
}

// PrintError prints an error message in red to stderr.
func PrintError(msg string) {
	fmt.Fprintln(os.Stderr, Red("Error: "+msg))
}

// PrintWarning prints a warning message in yellow to stderr.
func PrintWarning(msg string) {
	fmt.Fprintln(os.Stderr, Yellow("Warning: "+msg))
}

// PrintSuccess prints a success message in green to stdout.
func PrintSuccess(msg string) {
	fmt.Println(Green("✓ " + msg))
}

// PrintInfo prints an informational message to stdout.
func PrintInfo(msg string) {
	fmt.Println(msg)
}
