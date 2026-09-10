// Command doccheck protects the small set of repository instructions that
// release automation and coding agents rely on. It intentionally checks local
// Markdown links only; network links are not reproducible release evidence.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var markdownLink = regexp.MustCompile(`\[[^]]+\]\(([^)]+)\)`)

func main() {
	files, err := trackedMarkdownFiles()
	if err != nil {
		fmt.Fprintf(os.Stderr, "doccheck: list tracked Markdown: %v\n", err)
		os.Exit(1)
	}
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
			target = strings.Trim(target, "<>")
			if _, err := os.Stat(filepath.Clean(filepath.Join(filepath.Dir(name), target))); err != nil {
				fmt.Fprintf(os.Stderr, "doccheck: %s: broken local link %q\n", name, match[1])
				failed = true
			}
		}
	}
	if failed {
		os.Exit(1)
	}
	fmt.Printf("doccheck: local path targets resolve in %d tracked Markdown files\n", len(files))
}

func trackedMarkdownFiles() ([]string, error) {
	output, err := exec.Command("git", "ls-files", "-z", "--full-name", "--", ":(top,glob)**/*.md").Output()
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSuffix(string(output), "\x00")
	if trimmed == "" {
		return nil, fmt.Errorf("repository contains no tracked Markdown files")
	}
	return strings.Split(trimmed, "\x00"), nil
}
