package config

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ErrorFormat controls the output format of validation errors.
type ErrorFormat int

const (
	ErrorFormatText ErrorFormat = iota
	ErrorFormatJSON
)

// FormatErrors renders validation errors for terminal or CI consumption.
func FormatErrors(errs []*ValidationError, format ErrorFormat) string {
	if len(errs) == 0 {
		return ""
	}

	switch format {
	case ErrorFormatJSON:
		return formatJSON(errs)
	default:
		return formatText(errs)
	}
}

func formatText(errs []*ValidationError) string {
	var b strings.Builder
	for i, e := range errs {
		if i > 0 {
			b.WriteByte('\n')
		}
		loc := e.File
		if e.Line > 0 {
			loc = fmt.Sprintf("%s:%d:%d", e.File, e.Line, e.Column)
		}
		fmt.Fprintf(&b, "%s: %s: %s", loc, e.Field, e.Message)
		if e.Suggestion != "" {
			fmt.Fprintf(&b, "\n  Suggestion: %s", e.Suggestion)
		}
	}
	return b.String()
}

func formatJSON(errs []*ValidationError) string {
	data, err := json.MarshalIndent(errs, "", "  ")
	if err != nil {
		return fmt.Sprintf(`[{"error": "failed to marshal errors: %s"}]`, err)
	}
	return string(data)
}

// FormatSummary returns a summary line for the validation result.
func FormatSummary(totalFiles int, totalErrors int) string {
	if totalErrors == 0 {
		return fmt.Sprintf("Validated %d file(s): all valid.", totalFiles)
	}
	return fmt.Sprintf("Validated %d file(s): %d error(s) found.", totalFiles, totalErrors)
}
