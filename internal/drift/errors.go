package drift

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type ErrorCase struct {
	Status int             `json:"status"`
	Code   string          `json:"code"`
	Body   json.RawMessage `json:"body"`
}

type ErrorSet map[string]ErrorCase

func ExtractErrors(data []byte) (ErrorSet, error) {
	var cases ErrorSet
	if err := json.Unmarshal(data, &cases); err != nil {
		return nil, fmt.Errorf("invalid error contract JSON object: %w", err)
	}
	if cases == nil {
		return nil, fmt.Errorf("error artifact must be a JSON object")
	}
	for name, item := range cases {
		if !validCaseName(name) {
			return nil, fmt.Errorf("invalid error case name %q", name)
		}
		if item.Status == 0 {
			return nil, fmt.Errorf("error case %q must define a nonzero status", name)
		}
		if strings.TrimSpace(item.Code) == "" {
			return nil, fmt.Errorf("error case %q must define a code", name)
		}
		if len(item.Body) == 0 || string(item.Body) == "null" {
			return nil, fmt.Errorf("error case %q must define a body", name)
		}
		if _, err := Extract(item.Body); err != nil {
			return nil, fmt.Errorf("error case %q body: %w", name, err)
		}
	}
	return cases, nil
}

func CompareErrors(operation string, sdk, provider, mock ErrorSet) []Finding {
	names := make(map[string]struct{}, len(sdk)+len(provider)+len(mock))
	for name := range sdk {
		names[name] = struct{}{}
	}
	for name := range provider {
		names[name] = struct{}{}
	}
	for name := range mock {
		names[name] = struct{}{}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	findings := make([]Finding, 0)
	for _, name := range ordered {
		s, sok := sdk[name]
		p, pok := provider[name]
		m, mok := mock[name]
		path := "$errors." + name
		base := Finding{Operation: operation, Path: path, SDKValues: errorMetadata(s, sok), ProviderValues: errorMetadata(p, pok), MockValues: errorMetadata(m, mok)}
		switch {
		case sok && (!pok || !mok):
			item := base
			item.Severity, item.Rule = SeverityCritical, "sdk-required-error-case-missing"
			findings = append(findings, item)
		case pok && !sok && !mok:
			item := base
			item.Severity, item.Rule = SeverityWarning, "provider-only-error-case"
			findings = append(findings, item)
		case mok && !sok && !pok:
			item := base
			item.Severity, item.Rule = SeverityInfo, "mock-only-error-case"
			findings = append(findings, item)
		}
		if !(sok && pok && mok) {
			continue
		}
		findings = append(findings, compareErrorMetadata(base, s, p, m)...)
		sdkBody, _ := Extract(s.Body)
		providerBody, _ := Extract(p.Body)
		mockBody, _ := Extract(m.Body)
		bodyReport := Compare(operation, sdkBody, providerBody, mockBody)
		for _, finding := range bodyReport.Findings {
			finding.Path = path + ".body" + strings.TrimPrefix(finding.Path, "$")
			findings = append(findings, finding)
		}
	}
	return findings
}

func compareErrorMetadata(base Finding, sdk, provider, mock ErrorCase) []Finding {
	findings := make([]Finding, 0, 6)
	checks := []struct {
		mismatch bool
		rule     string
	}{
		{sdk.Status != provider.Status, "sdk-provider-error-status-mismatch"},
		{sdk.Status != mock.Status, "sdk-mock-error-status-mismatch"},
		{provider.Status != mock.Status, "provider-mock-error-status-mismatch"},
		{sdk.Code != provider.Code, "sdk-provider-error-code-mismatch"},
		{sdk.Code != mock.Code, "sdk-mock-error-code-mismatch"},
		{provider.Code != mock.Code, "provider-mock-error-code-mismatch"},
	}
	for _, check := range checks {
		if check.mismatch {
			item := base
			item.Severity, item.Rule = SeverityCritical, check.rule
			findings = append(findings, item)
		}
	}
	return findings
}

func errorMetadata(item ErrorCase, ok bool) []string {
	if !ok {
		return nil
	}
	return []string{"status=" + strconv.Itoa(item.Status), "code=" + item.Code}
}

func validCaseName(name string) bool {
	if name == "" {
		return false
	}
	for _, char := range name {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' && char != '_' {
			return false
		}
	}
	return true
}
