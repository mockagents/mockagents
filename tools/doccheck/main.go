// Command doccheck protects the small set of repository instructions that
// release automation and coding agents rely on. It intentionally checks local
// Markdown links only; network links are not reproducible release evidence.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var markdownLink = regexp.MustCompile(`\[[^]]+\]\(([^)]+)\)`)

func main() {
	files := []string{"AGENTS.md", "CONTRIBUTING.md", "docs/RELEASING.md"}
	failed := false
	for _, name := range files {
		body, err := os.ReadFile(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "doccheck: %s: %v\n", name, err)
			failed = true
			continue
		}
		for _, match := range markdownLink.FindAllStringSubmatch(string(body), -1) {
			target := strings.SplitN(match[1], "#", 2)[0]
			if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			if _, err := os.Stat(filepath.Clean(filepath.Join(filepath.Dir(name), target))); err != nil {
				fmt.Fprintf(os.Stderr, "doccheck: %s: broken local link %q\n", name, match[1])
				failed = true
			}
		}
	}
	if failed {
		os.Exit(1)
	}
	fmt.Println("doccheck: authoritative repository-document links resolve")
}
