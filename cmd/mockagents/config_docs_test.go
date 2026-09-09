package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// configReferencePath is the single place every environment variable is
// documented (audit H-13: configuration was env-only with no reference).
const configReferencePath = "site/docs/reference/configuration.md"

// envVarsNotDocumented are names that exist only to drive tests or tooling, so
// they are deliberately absent from the operator-facing reference.
var envVarsNotDocumented = map[string]bool{
	"MOCKAGENTS_TEST_PG_DSN": true, // opts the store conformance suite into Postgres
	"MOCKAGENTS_BIN":         true, // points a test at a prebuilt binary
}

var envVarRe = regexp.MustCompile(`MOCKAGENTS_[A-Z0-9_]+`)

// TestConfigurationReferenceCoversEveryEnvVar keeps the reference honest: a new
// MOCKAGENTS_* knob that ships undocumented fails here, which is the drift that
// left operators setting variables the code never read.
func TestConfigurationReferenceCoversEveryEnvVar(t *testing.T) {
	root := repoRoot(t)

	doc, err := os.ReadFile(filepath.Join(root, configReferencePath))
	if err != nil {
		t.Fatalf("reading the configuration reference: %v", err)
	}
	documented := string(doc)

	found := map[string]string{} // name -> first file that mentions it
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "site", "dist", ".gotmp":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, name := range envVarRe.FindAllString(string(data), -1) {
			if _, ok := found[name]; !ok {
				rel, _ := filepath.Rel(root, path)
				found[name] = filepath.ToSlash(rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
	if len(found) == 0 {
		t.Fatal("found no MOCKAGENTS_* variables; the scan is broken")
	}

	var missing []string
	for name, file := range found {
		if envVarsNotDocumented[name] {
			continue
		}
		if !strings.Contains(documented, name) {
			missing = append(missing, name+" (used in "+file+")")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("these environment variables are not in %s:\n  %s",
			configReferencePath, strings.Join(missing, "\n  "))
	}
}

// repoRoot walks up from the package directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not locate the module root from the test working directory")
	return ""
}
