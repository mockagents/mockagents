package drift

import (
	"encoding/json"
	"sort"
)

// SARIF renders a drift report as SARIF 2.1.0 for GitHub code scanning and
// other CI consumers. JSON paths are represented as logical locations; when an
// adapter is known it is also attached as the artifact URI to fix.
func SARIF(report Report) ([]byte, error) {
	type rule struct {
		ID               string         `json:"id"`
		ShortDescription map[string]any `json:"shortDescription"`
	}
	type location struct {
		LogicalLocations []map[string]any `json:"logicalLocations"`
		PhysicalLocation map[string]any   `json:"physicalLocation,omitempty"`
	}
	type result struct {
		RuleID    string         `json:"ruleId"`
		Level     string         `json:"level"`
		Message   map[string]any `json:"message"`
		Locations []location     `json:"locations"`
	}

	ruleIDs := make(map[string]struct{})
	results := make([]result, 0, len(report.Findings))
	for _, finding := range report.Findings {
		ruleIDs[finding.Rule] = struct{}{}
		loc := location{LogicalLocations: []map[string]any{{
			"name": finding.Path, "fullyQualifiedName": finding.Operation + ":" + finding.Path,
		}}}
		if report.Adapter != "" {
			loc.PhysicalLocation = map[string]any{"artifactLocation": map[string]any{"uri": report.Adapter}}
		}
		results = append(results, result{
			RuleID:    finding.Rule,
			Level:     sarifLevel(finding.Severity),
			Message:   map[string]any{"text": FindingDetail(finding)},
			Locations: []location{loc},
		})
	}
	orderedRules := make([]string, 0, len(ruleIDs))
	for id := range ruleIDs {
		orderedRules = append(orderedRules, id)
	}
	sort.Strings(orderedRules)
	rules := make([]rule, 0, len(orderedRules))
	for _, id := range orderedRules {
		rules = append(rules, rule{ID: id, ShortDescription: map[string]any{"text": id}})
	}

	doc := map[string]any{
		"$schema": "https://json.schemastore.org/sarif-2.1.0.json",
		"version": "2.1.0",
		"runs": []any{map[string]any{
			"tool": map[string]any{"driver": map[string]any{
				"name": "MockAgents provider drift", "version": report.Version, "rules": rules,
			}},
			"results": results,
		}},
	}
	out, err := json.MarshalIndent(doc, "", " ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func sarifLevel(severity Severity) string {
	switch severity {
	case SeverityCritical:
		return "error"
	case SeverityWarning:
		return "warning"
	default:
		return "note"
	}
}
