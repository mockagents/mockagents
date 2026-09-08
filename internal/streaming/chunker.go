package streaming

import "unicode"

const (
	DefaultChunkSize    = 4
	DefaultChunkDelayMs = 50
)

// Chunker splits response content into token-sized pieces.
// Tokens are approximated as whitespace-separated words.
type Chunker struct {
	ChunkSize int
}

// NewChunker creates a Chunker with the given chunk size.
// If chunkSize is <= 0, DefaultChunkSize is used.
func NewChunker(chunkSize int) *Chunker {
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	return &Chunker{ChunkSize: chunkSize}
}

// Chunk splits content into pieces of ChunkSize words each (the last may be
// shorter). The pieces are byte slices of the original string cut at word
// starts, so concatenating them reproduces content exactly — newlines,
// indentation, tabs and runs of spaces included. An earlier version split
// on strings.Fields and re-joined with single spaces, which streamed a
// markdown table or code block as one flattened line while the
// non-streaming path returned it verbatim (audit H-07). Whitespace that
// precedes a word travels with the chunk before it, so non-final chunks
// still end with their separator.
func (c *Chunker) Chunk(content string) []string {
	if content == "" {
		return nil
	}
	var chunks []string
	start, words := 0, 0
	prevSpace := true
	for i, r := range content {
		isSpace := unicode.IsSpace(r)
		if !isSpace && prevSpace {
			// A word starts here.
			if words == c.ChunkSize {
				chunks = append(chunks, content[start:i])
				start, words = i, 0
			}
			words++
		}
		prevSpace = isSpace
	}
	if words == 0 && len(chunks) == 0 {
		return []string{content} // whitespace only
	}
	return append(chunks, content[start:])
}
