// Command liquidcheck guards the GitHub Pages build against the one construct
// that silently breaks it: an unterminated Liquid opener in a docs/ page.
//
// GitHub Pages builds docs/ with Jekyll, and the enabled
// jekyll-optional-front-matter plugin runs every markdown file through Liquid
// whether or not it has front matter. Liquid runs BEFORE markdown, so a backtick
// code span protects nothing — prose describing "the static (no-`{{`) path" reads
// to Liquid as a variable that never closes, and the whole build aborts:
//
//	Liquid syntax error (line 191): Variable '{{' was not properly
//	terminated with regexp: /\}\}/ in benchmarks/README.md
//
// That exact typo kept `pages build and deployment` red from 2026-07-27 until it
// was found by hand. Nothing caught it, because a failed Pages deploy raises no
// PR check — the deploy is not part of CI, so the site can be broken for weeks
// while every PR shows green. This guard puts that failure mode where a reviewer
// will see it.
//
// It flags an UNTERMINATED opener only — `{{` with no `}}`, or `{%` with no `%}`.
// Balanced Liquid is left alone: it builds fine, and deciding whether a page
// *meant* to use Liquid is not this tool's business. Content inside
// {% raw %}…{% endraw %} is skipped, which is exactly how the offending line was
// fixed.
//
// Dependency-free and fast (<1s) so it can gate every PR. Exits 1 on any
// problem, printing file, line, and the offending text.
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// renderedExts are the extensions Jekyll runs through Liquid.
var renderedExts = map[string]bool{
	".md":       true,
	".markdown": true,
	".html":     true,
}

// problem is one unterminated opener.
type problem struct {
	File string
	Line int
	Text string
	Why  string
}

func (p problem) String() string {
	return fmt.Sprintf("%s:%d: %s\n      %s", p.File, p.Line, p.Why, p.Text)
}

func main() {
	dir := flag.String("dir", "docs", "directory of Jekyll-rendered pages to check")
	flag.Parse()

	problems, err := checkTree(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "liquidcheck:", err)
		os.Exit(2)
	}

	if len(problems) > 0 {
		fmt.Fprintf(os.Stderr, "liquid check FAILED: %d unterminated Liquid opener(s) in %s/\n\n", len(problems), *dir)
		for _, p := range problems {
			fmt.Fprintf(os.Stderr, "  - %s\n", p)
		}
		fmt.Fprintf(os.Stderr, `
Jekyll runs every page under %s/ through Liquid before markdown, so backticks do
not protect these — the GitHub Pages build aborts on the first one and the
published site goes stale while CI stays green.

To write a literal {{ or {%%, wrap it:

    the static (no-{%% raw %%}`+"`{{`"+`{%% endraw %%}) path

`, *dir)
		os.Exit(1)
	}

	fmt.Printf("liquidcheck: no unterminated Liquid openers in %s/\n", *dir)
}

// checkTree walks dir and checks every Jekyll-rendered page under it.
func checkTree(dir string) ([]problem, error) {
	var problems []problem
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Jekyll ignores underscore-prefixed directories it does not own,
			// and there is no reason to read .git or a vendored tree.
			if path != dir && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !renderedExts[strings.ToLower(filepath.Ext(d.Name()))] {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		problems = append(problems, checkSource(filepath.ToSlash(path), string(data))...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return problems, nil
}

// checkSource reports unterminated Liquid openers in one page.
//
// It scans the whole text rather than line by line, because Liquid does: a `{{`
// may legitimately be closed by a `}}` several lines later, and treating each
// line independently would invent failures that Jekyll does not have.
func checkSource(name, src string) []problem {
	var problems []problem
	i := 0
	for i < len(src) {
		open := nextOpener(src[i:])
		if open < 0 {
			break
		}
		at := i + open

		if strings.HasPrefix(src[at:], "{%") {
			end := strings.Index(src[at:], "%}")
			if end < 0 {
				problems = append(problems, problem{
					File: name, Line: lineOf(src, at),
					Why:  "unterminated `{%` — no closing `%}`",
					Text: excerpt(src, at),
				})
				break
			}
			tag := strings.TrimSpace(src[at+2 : at+end])
			after := at + end + 2
			if tag == "raw" {
				// Everything up to {% endraw %} is literal; skip it wholesale.
				rest := src[after:]
				closeAt := indexEndRaw(rest)
				if closeAt < 0 {
					problems = append(problems, problem{
						File: name, Line: lineOf(src, at),
						Why:  "unterminated `{% raw %}` — no closing `{% endraw %}`",
						Text: excerpt(src, at),
					})
					break
				}
				i = after + closeAt
				continue
			}
			i = after
			continue
		}

		// "{{"
		end := strings.Index(src[at:], "}}")
		if end < 0 {
			problems = append(problems, problem{
				File: name, Line: lineOf(src, at),
				Why:  "unterminated `{{` — no closing `}}`",
				Text: excerpt(src, at),
			})
			break
		}
		i = at + end + 2
	}
	return problems
}

// nextOpener returns the offset of the earliest `{{` or `{%` in s, or -1.
func nextOpener(s string) int {
	v := strings.Index(s, "{{")
	t := strings.Index(s, "{%")
	switch {
	case v < 0:
		return t
	case t < 0:
		return v
	case v < t:
		return v
	default:
		return t
	}
}

// indexEndRaw finds the {% endraw %} tag that closes a raw block, tolerating
// the whitespace variations Liquid allows.
func indexEndRaw(s string) int {
	from := 0
	for {
		at := strings.Index(s[from:], "{%")
		if at < 0 {
			return -1
		}
		at += from
		end := strings.Index(s[at:], "%}")
		if end < 0 {
			return -1
		}
		if strings.TrimSpace(s[at+2:at+end]) == "endraw" {
			return at + end + 2
		}
		from = at + end + 2
	}
}

// lineOf returns the 1-based line number of a byte offset.
func lineOf(src string, offset int) int {
	return strings.Count(src[:offset], "\n") + 1
}

// excerpt returns the trimmed source line containing offset, for the report.
func excerpt(src string, offset int) string {
	start := strings.LastIndexByte(src[:offset], '\n') + 1
	end := strings.IndexByte(src[offset:], '\n')
	if end < 0 {
		end = len(src)
	} else {
		end += offset
	}
	return strings.TrimSpace(strings.ReplaceAll(src[start:end], "\r", ""))
}
