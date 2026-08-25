// Package drift compares SDK, live-provider, and MockAgents response shapes.
// It is deliberately offline: callers collect and scrub artifacts separately.
package drift

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// IgnorePaths removes exact JSON paths and their descendants from a shape.
// Paths use the same notation emitted in findings, such as $.created or
// $.data[].id. This keeps volatile values out of comparison inputs.
func IgnorePaths(shape map[string]Shape, paths []string) (map[string]Shape, error) {
	ignored, err := normalizeIgnoredPaths(paths)
	if err != nil {
		return nil, err
	}
	filtered := make(map[string]Shape, len(shape))
	for path, value := range shape {
		if !isIgnoredPath(path, ignored) {
			filtered[path] = value
		}
	}
	return filtered, nil
}

func FilterFindings(report Report, paths []string) (Report, error) {
	ignored, err := normalizeIgnoredPaths(paths)
	if err != nil {
		return report, err
	}
	filtered := make([]Finding, 0, len(report.Findings))
	for _, finding := range report.Findings {
		if !isIgnoredPath(finding.Path, ignored) {
			filtered = append(filtered, finding)
		}
	}
	report.Findings = filtered
	return report, nil
}

func normalizeIgnoredPaths(paths []string) ([]string, error) {
	ignored := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if !validDriftPath(path) {
			return nil, fmt.Errorf("invalid ignored JSON path %q (want $, $.field, $[], $headers.field, $events, or $errors.field)", path)
		}
		ignored = append(ignored, path)
	}
	return ignored, nil
}

func validDriftPath(path string) bool {
	return path == "$" || path == "$headers" || path == "$events" || path == "$errors" || strings.HasPrefix(path, "$.") || strings.HasPrefix(path, "$[]") || strings.HasPrefix(path, "$headers.") || strings.HasPrefix(path, "$errors.")
}

func isIgnoredPath(path string, ignored []string) bool {
	for _, prefix := range ignored {
		if path == prefix || strings.HasPrefix(path, prefix+".") || strings.HasPrefix(path, prefix+"[]") {
			return true
		}
	}
	return false
}

type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityWarning  Severity = "warning"
	SeverityInfo     Severity = "info"
)

type Shape struct {
	Types    []string `json:"types"`
	Nullable bool     `json:"nullable,omitempty"`
}

type Finding struct {
	Operation      string   `json:"operation"`
	Path           string   `json:"path"`
	Severity       Severity `json:"severity"`
	Rule           string   `json:"rule"`
	SDK            *Shape   `json:"sdk,omitempty"`
	Provider       *Shape   `json:"provider,omitempty"`
	Mock           *Shape   `json:"mock,omitempty"`
	Values         []string `json:"values,omitempty"`
	SDKValues      []string `json:"sdk_values,omitempty"`
	ProviderValues []string `json:"provider_values,omitempty"`
	MockValues     []string `json:"mock_values,omitempty"`
}

type Report struct {
	Version   string    `json:"version"`
	Operation string    `json:"operation"`
	Adapter   string    `json:"adapter,omitempty"`
	Findings  []Finding `json:"findings"`
}

func Extract(data []byte) (map[string]Shape, error) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	shapes := make(map[string]Shape)
	walk(value, "$", shapes)
	return shapes, nil
}

// ExtractHeaders canonicalizes an HTTP-header JSON object into drift paths.
// Header names are case-insensitive and are reported below $headers.
func ExtractHeaders(data []byte) (map[string]Shape, error) {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("invalid header JSON object: %w", err)
	}
	if values == nil {
		return nil, fmt.Errorf("header artifact must be a JSON object")
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	shapes := map[string]Shape{"$headers": {Types: []string{"object"}}}
	seen := make(map[string]string, len(keys))
	for _, key := range keys {
		canonical := strings.ToLower(strings.TrimSpace(key))
		if canonical == "" {
			return nil, fmt.Errorf("header name cannot be empty")
		}
		if previous, ok := seen[canonical]; ok {
			return nil, fmt.Errorf("duplicate case-insensitive header %q and %q", previous, key)
		}
		seen[canonical] = key
		var value any
		if err := json.Unmarshal(values[key], &value); err != nil {
			return nil, fmt.Errorf("header %q: %w", key, err)
		}
		walk(value, "$headers."+canonical, shapes)
	}
	return shapes, nil
}

// MergeShapes adds source paths to destination and returns destination.
func MergeShapes(destination, source map[string]Shape) map[string]Shape {
	for path, shape := range source {
		destination[path] = shape
	}
	return destination
}

func walk(value any, path string, out map[string]Shape) {
	typeName := jsonType(value)
	shape := out[path]
	if typeName == "null" {
		shape.Nullable = true
	} else if !contains(shape.Types, typeName) {
		shape.Types = append(shape.Types, typeName)
		sort.Strings(shape.Types)
	}
	out[path] = shape
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			walk(v[key], path+"."+key, out)
		}
	case []any:
		for _, item := range v {
			walk(item, path+"[]", out)
		}
	}
}

func Compare(operation string, sdk, provider, mock map[string]Shape) Report {
	paths := map[string]struct{}{}
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
	report := Report{Version: "mockagents-drift/v1", Operation: operation, Findings: []Finding{}}
	for _, path := range ordered {
		s, sok := sdk[path]
		p, pok := provider[path]
		m, mok := mock[path]
		finding := Finding{Operation: operation, Path: path, SDK: shapePtr(s, sok), Provider: shapePtr(p, pok), Mock: shapePtr(m, mok)}
		switch {
		case sok && (!pok || !mok):
			finding.Severity, finding.Rule = SeverityCritical, "sdk-required-shape-missing"
		case sok && pok && (!sameTypes(s, p) || s.Nullable != p.Nullable):
			finding.Severity, finding.Rule = SeverityCritical, "sdk-provider-type-mismatch"
		case sok && mok && (!sameTypes(s, m) || s.Nullable != m.Nullable):
			finding.Severity, finding.Rule = SeverityCritical, "sdk-mock-type-mismatch"
		case pok && mok && (!sameTypes(p, m) || p.Nullable != m.Nullable):
			finding.Severity, finding.Rule = SeverityCritical, "provider-mock-type-mismatch"
		case pok && !sok && !mok:
			finding.Severity, finding.Rule = SeverityWarning, "provider-only-addition"
		case mok && !sok && !pok:
			finding.Severity, finding.Rule = SeverityInfo, "mock-only-field"
		default:
			continue
		}
		report.Findings = append(report.Findings, finding)
	}
	return report
}

func (r Report) HasCritical() bool {
	for _, f := range r.Findings {
		if f.Severity == SeverityCritical {
			return true
		}
	}
	return false
}

func shapePtr(s Shape, ok bool) *Shape {
	if !ok {
		return nil
	}
	copy := s
	return &copy
}
func sameTypes(a, b Shape) bool {
	if len(a.Types) != len(b.Types) {
		return false
	}
	for i := range a.Types {
		if a.Types[i] != b.Types[i] {
			return false
		}
	}
	return true
}
func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
func jsonType(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "unknown"
	}
}
