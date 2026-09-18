package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type exclusion struct {
	Pattern  string `json:"pattern"`
	Source   string `json:"source"`
	Reason   string `json:"reason"`
	Contract string `json:"contract"`
}

// checkCoverage compares independently parsed source mounts with reviewed
// operations and exact, justified exclusions. No wildcard exclusion is allowed.
func checkCoverage(routes []route) error {
	data, err := os.ReadFile("docs/api-spec.yaml")
	if err != nil {
		return err
	}
	var spec struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err = yaml.Unmarshal(data, &spec); err != nil {
		return err
	}
	data, err = os.ReadFile("docs/api-exclusions.json")
	if err != nil {
		return err
	}
	var exclusions []exclusion
	if err = json.Unmarshal(data, &exclusions); err != nil {
		return err
	}
	excluded := map[string]bool{}
	for _, e := range exclusions {
		key := e.Source + " " + e.Pattern
		if excluded[key] || len(e.Reason) < 20 || e.Contract == "" {
			return fmt.Errorf("invalid or duplicate exclusion: %s", key)
		}
		if _, err := os.Stat(e.Contract); err != nil {
			return fmt.Errorf("exclusion contract: %w", err)
		}
		excluded[key] = true
	}
	mounted := map[string]bool{}
	operations := map[string]bool{}
	ids := map[string]bool{}
	for path, methods := range spec.Paths {
		for method, value := range methods {
			if !strings.Contains(" get post put patch delete options head ", " "+method+" ") {
				continue
			}
			op, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("invalid operation %s %s", method, path)
			}
			id, _ := op["operationId"].(string)
			if id == "" || ids[id] {
				return fmt.Errorf("missing/duplicate operationId at %s %s", method, path)
			}
			ids[id] = true
			if responses, ok := op["responses"].(map[string]any); !ok || len(responses) == 0 {
				return fmt.Errorf("missing responses: %s", id)
			}
			operations[strings.ToUpper(method)+" "+path] = true
		}
	}
	for _, r := range routes {
		key := r.file + " " + r.pattern
		mounted[r.pattern] = true
		if operations[r.pattern] {
			if excluded[key] {
				return fmt.Errorf("documented operation still excluded: %s", key)
			}
			continue
		}
		if !excluded[key] {
			return fmt.Errorf("HTTP operation missing from OpenAPI or exclusions: %s", key)
		}
		delete(excluded, key)
	}
	if len(excluded) > 0 {
		return fmt.Errorf("stale route exclusions: %v", excluded)
	}
	for op := range operations {
		if !mounted[op] {
			return fmt.Errorf("OpenAPI operation has no source mount: %s", op)
		}
	}
	fmt.Printf("api coverage: %d OpenAPI operations; %d explicit source-bound exclusions\n", len(operations), len(exclusions))
	return nil
}
