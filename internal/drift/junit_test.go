package drift

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestJUnitReportsCriticalDriftAsFailure(t *testing.T) {
	report := Report{Version: "mockagents-drift/v1", Operation: "openai.chat", Adapter: "internal/adapter/openai.go", Findings: []Finding{
		{Operation: "openai.chat", Path: "$.id", Severity: SeverityCritical, Rule: "sdk-mock-type-mismatch"},
		{Operation: "openai.chat", Path: "$.new", Severity: SeverityWarning, Rule: "provider-only-addition"},
	}}
	out, err := JUnit(report)
	if err != nil {
		t.Fatal(err)
	}
	var suite junitSuite
	if err := xml.Unmarshal(out, &suite); err != nil {
		t.Fatalf("invalid JUnit XML: %v\n%s", err, out)
	}
	if suite.Tests != 2 || suite.Failures != 1 || suite.Errors != 0 {
		t.Fatalf("counts=%d/%d/%d", suite.Tests, suite.Failures, suite.Errors)
	}
	if suite.Cases[0].Failure == nil || !strings.Contains(suite.Cases[0].Failure.Body, report.Adapter) {
		t.Fatalf("critical case=%+v", suite.Cases[0])
	}
	if suite.Cases[1].Failure != nil || !strings.Contains(suite.Cases[1].SystemOut, "warning") {
		t.Fatalf("warning case=%+v", suite.Cases[1])
	}
}

func TestJUnitCompatibleReportPasses(t *testing.T) {
	out, err := JUnit(Report{Operation: "cohere.rerank", Findings: []Finding{}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `tests="1" failures="0"`) || !strings.Contains(string(out), `name="compatible"`) {
		t.Fatalf("output=%s", out)
	}
}
