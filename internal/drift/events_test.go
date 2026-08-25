package drift

import "testing"

func TestCompareEventsPreservesOrderAndDuplicates(t *testing.T) {
	sdk, err := ExtractEvents([]byte(`["response.created","response.delta","response.delta","response.done"]`))
	if err != nil {
		t.Fatal(err)
	}
	provider, _ := ExtractEvents([]byte(`["response.created","response.delta","response.done"]`))
	mock, _ := ExtractEvents([]byte(`["response.created","response.done","response.delta"]`))
	findings := CompareEvents("openai.responses.stream", sdk, provider, mock)
	if len(findings) != 3 {
		t.Fatalf("findings=%+v", findings)
	}
	if findings[0].Path != "$events" || findings[0].Rule != "sdk-provider-event-order-mismatch" {
		t.Fatalf("first=%+v", findings[0])
	}
	if len(findings[0].SDKValues) != 4 || findings[0].SDKValues[1] != findings[0].SDKValues[2] {
		t.Fatalf("duplicates lost: %v", findings[0].SDKValues)
	}
	if got := CompareEvents("test", sdk, sdk, sdk); len(got) != 0 {
		t.Fatalf("compatible findings=%+v", got)
	}
}

func TestExtractEventsRejectsInvalidArtifact(t *testing.T) {
	if _, err := ExtractEvents([]byte(`null`)); err == nil {
		t.Fatal("null event artifact must fail")
	}
	if _, err := ExtractEvents([]byte(`["created",""]`)); err == nil {
		t.Fatal("empty event name must fail")
	}
}
