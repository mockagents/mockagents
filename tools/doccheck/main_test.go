package main

import "testing"

func TestMarkdownLinkExtraction(t *testing.T) {
	got := markdownLink.FindStringSubmatch("read [the guide](docs/guide.md#gate)")
	if len(got) != 2 || got[1] != "docs/guide.md#gate" {
		t.Fatalf("unexpected match: %#v", got)
	}
}
