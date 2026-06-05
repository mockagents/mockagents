// Command driftcheck guards two cheap-to-drift invariants in CI (REF-06):
//
//  1. Every internal `$ref: '#/...'` in docs/api-spec.yaml resolves to a node
//     that actually exists, so the OpenAPI document can never reference a
//     component it has since deleted or renamed.
//  2. The license string agrees across the root LICENSE, both SDK manifests,
//     and the OpenAPI info block — all Apache-2.0. This guards REF-01 from
//     silently drifting back to MIT when someone regenerates a manifest.
//
// It is intentionally dependency-light (only yaml.v3, already required) and
// fast (<1s) so it can gate every PR. Exits 1 on any drift, printing each
// problem.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const wantLicense = "Apache-2.0"

func main() {
	root := flag.String("root", ".", "repository root to check")
	flag.Parse()

	var problems []string
	problems = append(problems, checkAPIRefs(filepath.Join(*root, "docs", "api-spec.yaml"))...)
	problems = append(problems, checkLicenses(*root)...)

	if len(problems) > 0 {
		fmt.Fprintln(os.Stderr, "drift check FAILED:")
		for _, p := range problems {
			fmt.Fprintf(os.Stderr, "  - %s\n", p)
		}
		os.Exit(1)
	}
	fmt.Printf("drift check OK: api-spec $refs resolve; licenses agree on %s\n", wantLicense)
}

// checkAPIRefs verifies every internal JSON-pointer $ref in the OpenAPI doc
// resolves to an existing node. External refs (no leading "#/") are out of
// scope and skipped.
func checkAPIRefs(specPath string) []string {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return []string{fmt.Sprintf("read %s: %v", specPath, err)}
	}
	var root any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return []string{fmt.Sprintf("parse %s: %v", specPath, err)}
	}

	var problems []string
	seen := map[string]bool{}
	for _, ref := range collectRefs(root) {
		if !strings.HasPrefix(ref, "#/") || seen[ref] {
			continue
		}
		seen[ref] = true
		if !resolveRef(root, ref) {
			problems = append(problems, fmt.Sprintf("api-spec.yaml: unresolved $ref %q", ref))
		}
	}
	return problems
}

// collectRefs walks a decoded YAML/JSON tree and returns every "$ref" string
// value it finds.
func collectRefs(node any) []string {
	var out []string
	switch n := node.(type) {
	case map[string]any:
		for k, v := range n {
			if k == "$ref" {
				if s, ok := v.(string); ok {
					out = append(out, s)
				}
				continue
			}
			out = append(out, collectRefs(v)...)
		}
	case []any:
		for _, v := range n {
			out = append(out, collectRefs(v)...)
		}
	}
	return out
}

// resolveRef follows an internal JSON pointer ("#/a/b/c") through the decoded
// document, applying the ~1→/ and ~0→~ escapes. Returns false if any segment
// is missing or descends through a non-map.
func resolveRef(root any, ref string) bool {
	ptr := strings.TrimPrefix(ref, "#/")
	if ptr == "" {
		return true
	}
	cur := root
	for _, rawSeg := range strings.Split(ptr, "/") {
		seg := strings.ReplaceAll(strings.ReplaceAll(rawSeg, "~1", "/"), "~0", "~")
		m, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		next, ok := m[seg]
		if !ok {
			return false
		}
		cur = next
	}
	return true
}

var pyLicenseRe = regexp.MustCompile(`(?m)^license\s*=\s*["']Apache-2\.0["']`)

// checkLicenses asserts the four canonical license sources all declare
// Apache-2.0.
func checkLicenses(root string) []string {
	var problems []string

	// 1. Root LICENSE — canonical Apache 2.0 text markers.
	licensePath := filepath.Join(root, "LICENSE")
	if data, err := os.ReadFile(licensePath); err != nil {
		problems = append(problems, fmt.Sprintf("read LICENSE: %v", err))
	} else if text := string(data); !strings.Contains(text, "Apache License") || !strings.Contains(text, "Version 2.0") {
		problems = append(problems, "LICENSE: file is not the Apache 2.0 license text")
	}

	// 2. Python SDK — license = "Apache-2.0".
	pyPath := filepath.Join(root, "sdk", "python", "pyproject.toml")
	if data, err := os.ReadFile(pyPath); err != nil {
		problems = append(problems, fmt.Sprintf("read %s: %v", pyPath, err))
	} else if !pyLicenseRe.Match(data) {
		problems = append(problems, fmt.Sprintf("sdk/python/pyproject.toml: license is not %q", wantLicense))
	}

	// 3. TypeScript SDK — "license": "Apache-2.0".
	tsPath := filepath.Join(root, "sdk", "typescript", "package.json")
	if got, err := jsonLicense(tsPath); err != nil {
		problems = append(problems, err.Error())
	} else if got != wantLicense {
		problems = append(problems, fmt.Sprintf("sdk/typescript/package.json: license = %q, want %q", got, wantLicense))
	}

	// 4. OpenAPI doc — info.license.name.
	specPath := filepath.Join(root, "docs", "api-spec.yaml")
	if got, err := specLicense(specPath); err != nil {
		problems = append(problems, err.Error())
	} else if got != wantLicense {
		problems = append(problems, fmt.Sprintf("docs/api-spec.yaml: info.license.name = %q, want %q", got, wantLicense))
	}

	return problems
}

func jsonLicense(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %v", path, err)
	}
	var pkg struct {
		License string `json:"license"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return "", fmt.Errorf("parse %s: %v", path, err)
	}
	return pkg.License, nil
}

func specLicense(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %v", path, err)
	}
	var doc struct {
		Info struct {
			License struct {
				Name string `yaml:"name"`
			} `yaml:"license"`
		} `yaml:"info"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("parse %s: %v", path, err)
	}
	return doc.Info.License.Name, nil
}
