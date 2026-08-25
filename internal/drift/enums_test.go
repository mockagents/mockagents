package drift

import "testing"

func TestExtractAndCompareEnums(t *testing.T) {
	sdk, err := ExtractEnums([]byte(`{"$.status":["done","queued","done"]}`))
	if err != nil {
		t.Fatal(err)
	}
	provider, _ := ExtractEnums([]byte(`{"$.status":["queued","running"]}`))
	mock, _ := ExtractEnums([]byte(`{"$.status":["queued","local"]}`))
	findings := CompareEnums("test", sdk, provider, mock)
	if len(findings) != 3 {
		t.Fatalf("findings=%+v", findings)
	}
	if findings[0].Severity != SeverityCritical || findings[0].Rule != "sdk-required-enum-missing" || findings[0].Values[0] != "done" {
		t.Fatalf("critical=%+v", findings[0])
	}
	if findings[1].Severity != SeverityWarning || findings[1].Values[0] != "running" {
		t.Fatalf("warning=%+v", findings[1])
	}
	if findings[2].Severity != SeverityInfo || findings[2].Values[0] != "local" {
		t.Fatalf("info=%+v", findings[2])
	}
	if got := sdk["$.status"]; len(got) != 2 || got[0] != "done" || got[1] != "queued" {
		t.Fatalf("normalized=%v", got)
	}
}

func TestExtractEnumsRejectsInvalidArtifact(t *testing.T) {
	if _, err := ExtractEnums([]byte(`[]`)); err == nil {
		t.Fatal("non-object enum artifact must fail")
	}
	if _, err := ExtractEnums([]byte(`{"status":["done"]}`)); err == nil {
		t.Fatal("invalid enum path must fail")
	}
}
