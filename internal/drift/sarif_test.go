package drift

import (
	"encoding/json"
	"testing"
)

func TestSARIFMapsSeverityLocationAndRule(t *testing.T) {
	report := Report{
		Version: "mockagents-drift/v1", Operation: "cohere.rerank", Adapter: "internal/adapter/cohere_rerank.go",
		Findings: []Finding{{Operation: "cohere.rerank", Path: "$.results[].score", Severity: SeverityCritical, Rule: "sdk-mock-type-mismatch"}},
	}
	out, err := SARIF(report)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("invalid SARIF JSON: %v", err)
	}
	if doc["version"] != "2.1.0" {
		t.Fatalf("version=%v", doc["version"])
	}
	runs := doc["runs"].([]any)
	results := runs[0].(map[string]any)["results"].([]any)
	result := results[0].(map[string]any)
	if result["ruleId"] != "sdk-mock-type-mismatch" || result["level"] != "error" {
		t.Fatalf("result=%+v", result)
	}
	locations := result["locations"].([]any)
	physical := locations[0].(map[string]any)["physicalLocation"].(map[string]any)
	artifact := physical["artifactLocation"].(map[string]any)
	if artifact["uri"] != report.Adapter {
		t.Fatalf("artifact=%+v", artifact)
	}
}

func TestSARIFCompatibleReportHasEmptyResults(t *testing.T) {
	out, err := SARIF(Report{Version: "mockagents-drift/v1", Operation: "test", Findings: []Finding{}})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Runs []struct {
			Results []any `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(out, &doc); err != nil || len(doc.Runs) != 1 || len(doc.Runs[0].Results) != 0 {
		t.Fatalf("compatible SARIF=%s err=%v", out, err)
	}
}
