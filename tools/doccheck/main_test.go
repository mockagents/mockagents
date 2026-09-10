package main

import "testing"

func TestMarkdownLinkExtraction(t *testing.T) {
	got := markdownLink.FindStringSubmatch("read [the guide](docs/guide.md#gate)")
	if len(got) != 2 || got[1] != "docs/guide.md#gate" {
		t.Fatalf("unexpected match: %#v", got)
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
