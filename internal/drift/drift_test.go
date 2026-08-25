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

func TestIgnorePathsRemovesExactPathsAndDescendants(t *testing.T) {
	shape := mustShape(t, `{"created":123,"data":[{"id":"volatile","value":1}]}`)
	filtered, err := IgnorePaths(shape, []string{"$.created", " $.data[].id "})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := filtered["$.created"]; ok {
		t.Fatal("created path was not ignored")
	}
	if _, ok := filtered["$.data[].id"]; ok {
		t.Fatal("array item id path was not ignored")
	}
	if _, ok := filtered["$.data[].value"]; !ok {
		t.Fatal("unrelated path was removed")
	}
	if _, err := IgnorePaths(shape, []string{"created"}); err == nil {
		t.Fatal("invalid path must fail")
	}
}

func TestExtractHeadersCanonicalizesNamesAndShapes(t *testing.T) {
	headers, err := ExtractHeaders([]byte(`{"Content-Type":"application/json","X-RateLimit-Remaining":12}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := headers["$headers.content-type"].Types; len(got) != 1 || got[0] != "string" {
		t.Fatalf("content-type=%+v", got)
	}
	if got := headers["$headers.x-ratelimit-remaining"].Types; len(got) != 1 || got[0] != "number" {
		t.Fatalf("rate-limit=%+v", got)
	}
	filtered, err := IgnorePaths(headers, []string{"$headers.x-ratelimit-remaining"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := filtered["$headers.x-ratelimit-remaining"]; ok {
		t.Fatal("volatile header path was not ignored")
	}
	if _, err := ExtractHeaders([]byte(`{"X-ID":"a","x-id":"b"}`)); err == nil {
		t.Fatal("case-insensitive duplicate must fail")
	}
	if _, err := ExtractHeaders([]byte(`[]`)); err == nil {
		t.Fatal("non-object headers must fail")
	}
}

func TestCompareIncludesHeaderDrift(t *testing.T) {
	sdk := MergeShapes(mustShape(t, `{"id":"x"}`), mustHeaders(t, `{"Content-Type":"application/json"}`))
	provider := MergeShapes(mustShape(t, `{"id":"x"}`), mustHeaders(t, `{"content-type":"application/json"}`))
	mock := MergeShapes(mustShape(t, `{"id":"x"}`), mustHeaders(t, `{"CONTENT-TYPE":["application/json"]}`))
	report := Compare("test", sdk, provider, mock)
	if !report.HasCritical() || len(report.Findings) != 2 || report.Findings[0].Path != "$headers.content-type" || report.Findings[1].Path != "$headers.content-type[]" {
		t.Fatalf("header findings=%+v", report.Findings)
	}
}

func mustHeaders(t *testing.T, value string) map[string]Shape {
	t.Helper()
	shape, err := ExtractHeaders([]byte(value))
	if err != nil {
		t.Fatal(err)
	}
	return shape
}
