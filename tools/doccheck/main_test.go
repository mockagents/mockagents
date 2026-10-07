package main

import "testing"

func TestMarkdownLinkExtraction(t *testing.T) {
	got := markdownLink.FindStringSubmatch("read [the guide](docs/guide.md#gate)")
	if len(got) != 2 || got[1] != "docs/guide.md#gate" {
		t.Fatalf("unexpected match: %#v", got)
	}
}

// Brackets followed by parentheses inside code are not links: a Go generic
// signature in a fenced block or an inline span must not be checked.
func TestLinksInCodeAreIgnored(t *testing.T) {
	body := "see [the guide](docs/guide.md)\r\n" +
		"```go\r\n" +
		"func New[K comparable, V any](cfg Config[K]) *Store[K, V]\r\n" +
		"```\r\n" +
		"~~~~\n[not](a/link.md)\n~~~~\n" +
		"inline `m[k](x)` code, then [real](docs/real.md)\n" +
		"````\n```\n[still](inside.md)\n````\n"
	var got []string
	for _, m := range markdownLink.FindAllStringSubmatch(withoutCode(body), -1) {
		got = append(got, m[1])
	}
	if len(got) != 2 || got[0] != "docs/guide.md" || got[1] != "docs/real.md" {
		t.Fatalf("links = %q, want [docs/guide.md docs/real.md]", got)
	}
}

func TestTrackedMarkdownIncludesComponentGuides(t *testing.T) {
	files, err := trackedMarkdownFiles()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"AGENTS.md":               false,
		"sdk/python/README.md":    false,
		"site/docs/guides/a2a.md": false,
	}
	for _, file := range files {
		if _, ok := want[file]; ok {
			want[file] = true
		}
	}
	for file, found := range want {
		if !found {
			t.Errorf("tracked Markdown list does not contain %s", file)
		}
	}
}
