package streaming

import "strings"

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

// Chunk splits content into pieces of approximately ChunkSize words.
// Non-final chunks have a trailing space to preserve word spacing when
// reassembled by the client.
func (c *Chunker) Chunk(content string) []string {
	if content == "" {
		return nil
	}

	words := strings.Fields(content)
	if len(words) == 0 {
		return []string{content}
	}

	var chunks []string
	for i := 0; i < len(words); i += c.ChunkSize {
		end := i + c.ChunkSize
		if end > len(words) {
			end = len(words)
		}
		chunk := strings.Join(words[i:end], " ")
		if end < len(words) {
			chunk += " "
		}
		chunks = append(chunks, chunk)
	}
	return chunks
}
