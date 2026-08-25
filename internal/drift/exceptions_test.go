package drift

import (
	"strings"
	"testing"
	"time"
)

func TestApplyExceptionsExactMatchAndExpiry(t *testing.T) {
	report := Report{Version: "mockagents-drift/v1", Operation: "openai.chat", Findings: []Finding{
		{Operation: "openai.chat", Path: "$.id", Rule: "sdk-mock-type-mismatch", Severity: SeverityCritical},
		{Operation: "openai.chat", Path: "$.usage", Rule: "provider-only-addition", Severity: SeverityWarning},
	}}
	document := []byte(`{"version":"mockagents-drift-exceptions/v1","exceptions":[{"operation":"openai.chat","path":"$.id","rule":"sdk-mock-type-mismatch","owner":"sdk-team","expires":"2026-09-01"}]}`)
	got, err := ApplyExceptions(report, document, time.Date(2026, 8, 25, 18, 0, 0, 0, time.UTC))
	if err != nil || len(got.Findings) != 1 || got.Findings[0].Path != "$.usage" || len(got.Exceptions) != 1 || got.HasCritical() {
		t.Fatalf("report=%+v err=%v", got, err)
	}
	_, err = ApplyExceptions(report, document, time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC))
	if err == nil || !strings.Contains(err.Error(), "expired on 2026-09-01") {
		t.Fatalf("expired err=%v", err)
	}
}

func TestApplyExceptionsRequiresOwnerAndVersion(t *testing.T) {
	for _, document := range []string{
		`{"version":"v2","exceptions":[]}`,
		`{"version":"mockagents-drift-exceptions/v1","exceptions":[{"operation":"op","path":"$.id","rule":"rule","expires":"2026-09-01"}]}`,
	} {
		if _, err := ApplyExceptions(Report{}, []byte(document), time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)); err == nil {
			t.Fatalf("document unexpectedly valid: %s", document)
		}
	}
}
