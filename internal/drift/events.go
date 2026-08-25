package drift

import (
	"encoding/json"
	"fmt"
	"strings"
)

func ExtractEvents(data []byte) ([]string, error) {
	var events []string
	if err := json.Unmarshal(data, &events); err != nil {
		return nil, fmt.Errorf("invalid event sequence JSON array: %w", err)
	}
	if events == nil {
		return nil, fmt.Errorf("event artifact must be a JSON array")
	}
	for index, event := range events {
		if strings.TrimSpace(event) == "" {
			return nil, fmt.Errorf("event at index %d cannot be empty", index)
		}
	}
	return events, nil
}

// CompareEvents reports ordering, omission, and duplicate-event drift. Event
// arrays preserve duplicates because they are part of the stream contract.
func CompareEvents(operation string, sdk, provider, mock []string) []Finding {
	base := Finding{Operation: operation, Path: "$events", SDKValues: sdk, ProviderValues: provider, MockValues: mock}
	findings := make([]Finding, 0, 3)
	if !sameSequence(sdk, provider) {
		item := base
		item.Severity, item.Rule = SeverityCritical, "sdk-provider-event-order-mismatch"
		findings = append(findings, item)
	}
	if !sameSequence(sdk, mock) {
		item := base
		item.Severity, item.Rule = SeverityCritical, "sdk-mock-event-order-mismatch"
		findings = append(findings, item)
	}
	if !sameSequence(provider, mock) {
		item := base
		item.Severity, item.Rule = SeverityCritical, "provider-mock-event-order-mismatch"
		findings = append(findings, item)
	}
	return findings
}

func sameSequence(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
