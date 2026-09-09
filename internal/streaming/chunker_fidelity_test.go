package streaming

import (
	"strings"
	"testing"
)

// TestChunker_PreservesWhitespaceExactly is the audit H-07 guard: the stream
// must reproduce the response byte-for-byte when its deltas are
// concatenated, whatever the chunk size — newlines, indentation, tabs,
// runs of spaces and unicode spaces included. The old implementation
// flattened a markdown table or code block into one line.
func TestChunker_PreservesWhitespaceExactly(t *testing.T) {
	inputs := []string{
		"line one\nline two\n\nline four",
		"```go\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n```",
		"| a | b |\n|---|---|\n| 1 | 2 |",
		"double  space   and\ttab",
		"  leading and trailing  ",
		"nbsp separated words and em-space",
		"single",
		"\n",
		"a\r\nb\r\nc",
	}
	for _, in := range inputs {
		for size := 1; size <= 5; size++ {
			chunks := NewChunker(size).Chunk(in)
			if got := strings.Join(chunks, ""); got != in {
				t.Errorf("size %d: %q reassembled to %q", size, in, got)
			}
			for i, c := range chunks {
				if c == "" {
					t.Errorf("size %d: %q produced an empty chunk at %d: %q", size, in, i, chunks)
				}
			}
		}
	}
}

// TestChunker_WordCountPerChunk: every non-final chunk holds exactly
// ChunkSize words, so pacing physics see the same token cadence as before.
func TestChunker_WordCountPerChunk(t *testing.T) {
	in := "one two\nthree  four\tfive six seven"
	chunks := NewChunker(3).Chunk(in)
	if len(chunks) != 3 {
		t.Fatalf("chunks = %q, want 3", chunks)
	}
	for i, c := range chunks[:2] {
		if n := len(strings.Fields(c)); n != 3 {
			t.Errorf("chunk %d %q has %d words, want 3", i, c, n)
		}
	}
	want := []string{"one two\nthree  ", "four\tfive six ", "seven"}
	for i := range want {
		if chunks[i] != want[i] {
			t.Errorf("chunk %d = %q, want %q", i, chunks[i], want[i])
		}
	}
}
