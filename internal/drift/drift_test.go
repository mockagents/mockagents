package drift

import "testing"

func mustShape(t *testing.T, value string) map[string]Shape {
	t.Helper()
	shape, err := Extract([]byte(value))
	if err != nil {
		t.Fatal(err)
	}
	return shape
}

func TestCompareSeededCriticalDrift(t *testing.T) {
	sdk := mustShape(t, `{"id":"x","results":[{"score":0.9,"index":0}]}`)
	provider := mustShape(t, `{"id":"x","results":[{"score":0.9,"index":0,"new_field":true}]}`)
	mock := mustShape(t, `{"id":"x","results":[{"score":"wrong","index":0}]}`)
	report := Compare("cohere.rerank", sdk, provider, mock)
	if !report.HasCritical() {
		t.Fatalf("seeded type drift must be critical: %+v", report.Findings)
	}
	if len(report.Findings) != 2 || report.Findings[0].Path != "$.results[].new_field" || report.Findings[0].Severity != SeverityWarning || report.Findings[1].Path != "$.results[].score" {
		t.Fatalf("findings not deterministic: %+v", report.Findings)
	}
}

func TestCompareCompatibleAndInvalidJSON(t *testing.T) {
	shape := mustShape(t, `{"ok":true,"value":null}`)
	if report := Compare("test", shape, shape, shape); len(report.Findings) != 0 || report.HasCritical() {
		t.Fatalf("compatible report: %+v", report)
	}
	if _, err := Extract([]byte(`{"broken":`)); err == nil {
		t.Fatal("expected parse error")
	}
}
