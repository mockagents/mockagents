package drift

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

const exceptionFileVersion = "mockagents-drift-exceptions/v1"

type Exception struct {
	Operation string `json:"operation"`
	Path      string `json:"path"`
	Rule      string `json:"rule"`
	Owner     string `json:"owner"`
	Expires   string `json:"expires"`
}

type exceptionFile struct {
	Version    string      `json:"version"`
	Exceptions []Exception `json:"exceptions"`
}

// ApplyExceptions validates owner-approved exceptions, rejects stale
// approvals, and suppresses only exact operation/path/rule matches.
func ApplyExceptions(report Report, data []byte, now time.Time) (Report, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document exceptionFile
	if err := decoder.Decode(&document); err != nil {
		return report, fmt.Errorf("invalid drift exceptions: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return report, err
	}
	if document.Version != exceptionFileVersion {
		return report, fmt.Errorf("unsupported drift exception version %q (want %s)", document.Version, exceptionFileVersion)
	}
	today := now.UTC().Truncate(24 * time.Hour)
	for i, exception := range document.Exceptions {
		if strings.TrimSpace(exception.Operation) == "" || strings.TrimSpace(exception.Path) == "" || strings.TrimSpace(exception.Rule) == "" || strings.TrimSpace(exception.Owner) == "" {
			return report, fmt.Errorf("drift exception %d requires operation, path, rule, and owner", i)
		}
		expires, err := time.Parse("2006-01-02", exception.Expires)
		if err != nil {
			return report, fmt.Errorf("drift exception %d has invalid expires date %q (want YYYY-MM-DD)", i, exception.Expires)
		}
		if expires.Before(today) {
			return report, fmt.Errorf("drift exception %d owned by %q expired on %s", i, exception.Owner, exception.Expires)
		}
	}

	kept := make([]Finding, 0, len(report.Findings))
	for _, finding := range report.Findings {
		matched := false
		for _, exception := range document.Exceptions {
			if exception.Operation == finding.Operation && exception.Path == finding.Path && exception.Rule == finding.Rule {
				report.Exceptions = append(report.Exceptions, exception)
				matched = true
				break
			}
		}
		if !matched {
			kept = append(kept, finding)
		}
	}
	report.Findings = kept
	return report, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("invalid drift exceptions: multiple JSON values")
		}
		return fmt.Errorf("invalid drift exceptions: %w", err)
	}
	return nil
}
