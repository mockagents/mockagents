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

// inlineCode matches a single-backtick code span on one line.
var inlineCode = regexp.MustCompile("`[^`\n]*`")

// withoutCode blanks fenced code blocks and inline code spans, where Markdown
// renders brackets and parentheses literally: a Go generic such as
// New[K, V](cfg Config[K]) is code, not a link.
func withoutCode(body string) string {
	lines := strings.Split(body, "\n")
	fence := ""
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, fence[:1]) == "" {
				fence = ""
			}
			lines[i] = ""
			continue
		}
		if marker := fenceMarker(trimmed); marker != "" {
			fence = marker
			lines[i] = ""
			continue
		}
		lines[i] = inlineCode.ReplaceAllString(line, "")
	}
	return strings.Join(lines, "\n")
}

// fenceMarker returns the run of backticks or tildes (three or more) that
// opens a fenced code block, or "".
func fenceMarker(trimmed string) string {
	for _, c := range []string{"`", "~"} {
		n := len(trimmed) - len(strings.TrimLeft(trimmed, c))
		if n >= 3 {
			return strings.Repeat(c, n)
		}
	}
	return ""
}

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
		for _, match := range markdownLink.FindAllStringSubmatch(withoutCode(string(body)), -1) {
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
