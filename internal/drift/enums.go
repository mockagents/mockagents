package drift

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// EnumSet maps report JSON paths to the complete supported string values at
// that path. Values are normalized to unique lexical order.
type EnumSet map[string][]string

func IgnoreEnumPaths(enums EnumSet, paths []string) (EnumSet, error) {
	ignored := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if !validDriftPath(path) {
			return nil, fmt.Errorf("invalid ignored JSON path %q", path)
		}
		ignored = append(ignored, path)
	}
	filtered := make(EnumSet, len(enums))
	for path, values := range enums {
		if !isIgnoredPath(path, ignored) {
			filtered[path] = values
		}
	}
	return filtered, nil
}

func ExtractEnums(data []byte) (EnumSet, error) {
	var raw map[string][]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid enum JSON object: %w", err)
	}
	if raw == nil {
		return nil, fmt.Errorf("enum artifact must be a JSON object")
	}
	out := make(EnumSet, len(raw))
	for path, values := range raw {
		path = strings.TrimSpace(path)
		if !validDriftPath(path) {
			return nil, fmt.Errorf("invalid enum JSON path %q", path)
		}
		unique := make(map[string]struct{}, len(values))
		for _, value := range values {
			unique[value] = struct{}{}
		}
		normalized := make([]string, 0, len(unique))
		for value := range unique {
			normalized = append(normalized, value)
		}
		sort.Strings(normalized)
		out[path] = normalized
	}
	return out, nil
}

// CompareEnums triangulates complete SDK, provider, and mock enum inventories.
func CompareEnums(operation string, sdk, provider, mock EnumSet) []Finding {
	paths := make(map[string]struct{}, len(sdk)+len(provider)+len(mock))
	for path := range sdk {
		paths[path] = struct{}{}
	}
	for path := range provider {
		paths[path] = struct{}{}
	}
	for path := range mock {
		paths[path] = struct{}{}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	findings := make([]Finding, 0)
	for _, path := range ordered {
		s, p, m := sdk[path], provider[path], mock[path]
		base := Finding{Operation: operation, Path: path, SDKValues: s, ProviderValues: p, MockValues: m}
		if missing := union(difference(s, p), difference(s, m)); len(missing) != 0 {
			item := base
			item.Severity, item.Rule, item.Values = SeverityCritical, "sdk-required-enum-missing", missing
			findings = append(findings, item)
		}
		if added := difference(difference(p, s), m); len(added) != 0 {
			item := base
			item.Severity, item.Rule, item.Values = SeverityWarning, "provider-only-enum-value", added
			findings = append(findings, item)
		}
		if added := difference(difference(m, s), p); len(added) != 0 {
			item := base
			item.Severity, item.Rule, item.Values = SeverityInfo, "mock-only-enum-value", added
			findings = append(findings, item)
		}
	}
	return findings
}

func MergeFindings(report Report, findings []Finding) Report {
	report.Findings = append(report.Findings, findings...)
	sort.SliceStable(report.Findings, func(i, j int) bool {
		if report.Findings[i].Path != report.Findings[j].Path {
			return report.Findings[i].Path < report.Findings[j].Path
		}
		return report.Findings[i].Rule < report.Findings[j].Rule
	})
	return report
}

func FindingDetail(finding Finding) string {
	detail := fmt.Sprintf("%s provider drift at %s (%s)", finding.Severity, finding.Path, finding.Rule)
	if values := FindingValueSummary(finding); values != "" {
		detail += " " + values
	}
	return detail
}

func FindingValueSummary(finding Finding) string {
	if len(finding.Values) != 0 {
		return fmt.Sprintf("values=%q", finding.Values)
	}
	if len(finding.SDKValues) != 0 || len(finding.ProviderValues) != 0 || len(finding.MockValues) != 0 {
		return fmt.Sprintf("sdk=%q provider=%q mock=%q", finding.SDKValues, finding.ProviderValues, finding.MockValues)
	}
	return ""
}

func difference(left, right []string) []string {
	rightSet := make(map[string]struct{}, len(right))
	for _, value := range right {
		rightSet[value] = struct{}{}
	}
	out := make([]string, 0)
	for _, value := range left {
		if _, ok := rightSet[value]; !ok {
			out = append(out, value)
		}
	}
	return out
}

func union(left, right []string) []string {
	values := make(map[string]struct{}, len(left)+len(right))
	for _, value := range append(left, right...) {
		values[value] = struct{}{}
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
