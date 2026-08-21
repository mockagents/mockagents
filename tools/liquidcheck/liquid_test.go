package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckSource_CleanPageHasNoProblems(t *testing.T) {
	src := "# Title\n\nOrdinary prose with `code spans` and a { single brace }.\n"
	if got := checkSource("clean.md", src); len(got) != 0 {
		t.Fatalf("want no problems, got %v", got)
	}
}

func TestCheckSource_UnterminatedVariable(t *testing.T) {
	// The exact shape that kept the Pages build red: a literal {{ in prose,
	// inside backticks, which Liquid sees long before markdown does.
	src := "line one\nline two\nbuffers across template renders; the static (no-`{{`) path is\nline four\n"

	got := checkSource("benchmarks/README.md", src)
	if len(got) != 1 {
		t.Fatalf("want 1 problem, got %d: %v", len(got), got)
	}
	if got[0].Line != 3 {
		t.Errorf("want line 3, got %d", got[0].Line)
	}
	if got[0].File != "benchmarks/README.md" {
		t.Errorf("want the file name reported, got %q", got[0].File)
	}
	if got[0].Text == "" {
		t.Error("want the offending line quoted in the report")
	}
}

func TestCheckSource_UnterminatedTag(t *testing.T) {
	got := checkSource("page.md", "text\n{% include something\nmore text\n")
	if len(got) != 1 {
		t.Fatalf("want 1 problem, got %d: %v", len(got), got)
	}
	if got[0].Line != 2 {
		t.Errorf("want line 2, got %d", got[0].Line)
	}
}

func TestCheckSource_RawBlockProtectsLiteralBraces(t *testing.T) {
	// This is the fix that was applied to docs/benchmarks/README.md — the guard
	// must accept it, or it would flag the very thing it is asking people to do.
	src := "the static (no-{% raw %}`{{`{% endraw %}) path is unchanged\n"
	if got := checkSource("fixed.md", src); len(got) != 0 {
		t.Fatalf("raw-wrapped braces should be accepted, got %v", got)
	}
}

func TestCheckSource_RawBlockSpanningLines(t *testing.T) {
	src := "before\n{% raw %}\nresponse: \"{{ .Name }}\"\nand a stray {{ too\n{% endraw %}\nafter\n"
	if got := checkSource("template-docs.md", src); len(got) != 0 {
		t.Fatalf("everything inside raw is literal, got %v", got)
	}
}

func TestCheckSource_UnterminatedRawBlock(t *testing.T) {
	got := checkSource("page.md", "before\n{% raw %}\nnever closed\n")
	if len(got) != 1 {
		t.Fatalf("want 1 problem, got %d: %v", len(got), got)
	}
	if got[0].Line != 2 {
		t.Errorf("want line 2, got %d", got[0].Line)
	}
}

func TestCheckSource_BalancedLiquidIsAllowed(t *testing.T) {
	// Balanced Liquid builds fine. Whether a page MEANT to use it is not this
	// tool's call — flagging it would make the guard noisy and get it ignored.
	src := "{{ site.title }} and {% if x %}yes{% endif %}\n"
	if got := checkSource("uses-liquid.md", src); len(got) != 0 {
		t.Fatalf("balanced Liquid should pass, got %v", got)
	}
}

func TestCheckSource_VariableClosedOnALaterLine(t *testing.T) {
	// Liquid scans the whole document, so this is legal and must not be flagged.
	if got := checkSource("multiline.md", "{{\n  site.title\n}}\n"); len(got) != 0 {
		t.Fatalf("a multi-line variable is valid Liquid, got %v", got)
	}
}

func TestCheckSource_ReportsFirstProblemPerFile(t *testing.T) {
	// Jekyll aborts on the first error, so one report per file matches what a
	// fixer will actually see when they rebuild.
	got := checkSource("page.md", "{{ broken\nand `{{` again\n")
	if len(got) != 1 {
		t.Fatalf("want 1 problem, got %d: %v", len(got), got)
	}
}

func TestCheckTree_FindsAndSkipsTheRightFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("ok.md", "clean page\n")
	write("nested/bad.md", "a literal `{{` in prose\n")
	write("page.html", "<p>{{ unterminated</p>\n")
	write("notes.txt", "a literal {{ here is not rendered by Jekyll\n")
	write(".git/config.md", "{{ not ours\n")

	problems, err := checkTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 2 {
		t.Fatalf("want 2 problems (nested/bad.md, page.html), got %d: %v", len(problems), problems)
	}
	for _, p := range problems {
		if filepath.Ext(p.File) == ".txt" {
			t.Errorf(".txt is not rendered by Jekyll and must be ignored: %v", p)
		}
	}
}

// The repository's own docs/ must pass — this is the guard watching the tree it
// was written for, so a regression in either fails here before CI.
func TestCheckTree_RepositoryDocsAreClean(t *testing.T) {
	problems, err := checkTree(filepath.Join("..", "..", "docs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("docs/ has unterminated Liquid openers: %v", problems)
	}
}
