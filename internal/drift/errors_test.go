package drift

import "testing"

func TestCompareErrorsChecksMetadataAndBodyShape(t *testing.T) {
	sdk := mustErrors(t, `{"rate_limit":{"status":429,"code":"rate_limit","body":{"error":{"message":"wait"}}}}`)
	provider := mustErrors(t, `{"rate_limit":{"status":429,"code":"rate_limit","body":{"error":{"message":"wait","retry_after":1}}}}`)
	mock := mustErrors(t, `{"rate_limit":{"status":400,"code":"bad_request","body":{"error":{"message":12}}}}`)
	findings := CompareErrors("cohere.rerank.error", sdk, provider, mock)
	rules := make(map[string]bool)
	for _, finding := range findings {
		rules[finding.Rule] = true
	}
	for _, rule := range []string{"sdk-mock-error-status-mismatch", "sdk-mock-error-code-mismatch", "sdk-mock-type-mismatch", "provider-only-addition"} {
		if !rules[rule] {
			t.Fatalf("missing %s in %+v", rule, findings)
		}
	}
}

func TestCompareErrorsClassifiesMissingCases(t *testing.T) {
	one := mustErrors(t, `{"invalid":{"status":400,"code":"invalid","body":{"error":true}}}`)
	empty := mustErrors(t, `{}`)
	findings := CompareErrors("test", one, empty, empty)
	if len(findings) != 1 || findings[0].Rule != "sdk-required-error-case-missing" || findings[0].Severity != SeverityCritical {
		t.Fatalf("findings=%+v", findings)
	}
}

func TestExtractErrorsRejectsInvalidArtifact(t *testing.T) {
	if _, err := ExtractErrors([]byte(`null`)); err == nil {
		t.Fatal("null error artifact must fail")
	}
	if _, err := ExtractErrors([]byte(`{"bad.case":{"status":400,"code":"bad","body":{}}}`)); err == nil {
		t.Fatal("invalid case name must fail")
	}
}

func mustErrors(t *testing.T, value string) ErrorSet {
	t.Helper()
	cases, err := ExtractErrors([]byte(value))
	if err != nil {
		t.Fatal(err)
	}
	return cases
}
