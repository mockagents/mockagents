package streaming

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

func TestChunker_BasicChunking(t *testing.T) {
	c := NewChunker(2)
	chunks := c.Chunk("Hello world how are you")

	assert.Equal(t, []string{"Hello world ", "how are ", "you"}, chunks)
}

func TestChunker_DefaultChunkSize(t *testing.T) {
	c := NewChunker(0)
	assert.Equal(t, DefaultChunkSize, c.ChunkSize)
}

func TestChunker_SingleChunk(t *testing.T) {
	c := NewChunker(10)
	chunks := c.Chunk("Hello world")
	assert.Equal(t, []string{"Hello world"}, chunks)
}

func TestChunker_ExactFit(t *testing.T) {
	c := NewChunker(3)
	chunks := c.Chunk("one two three")
	assert.Equal(t, []string{"one two three"}, chunks)
}

func TestChunker_EmptyContent(t *testing.T) {
	c := NewChunker(4)
	assert.Nil(t, c.Chunk(""))
}

func TestChunker_WhitespaceOnly(t *testing.T) {
	c := NewChunker(4)
	chunks := c.Chunk("   ")
	assert.Equal(t, []string{"   "}, chunks)
}

func TestChunker_SingleWord(t *testing.T) {
	c := NewChunker(4)
	chunks := c.Chunk("Hello")
	assert.Equal(t, []string{"Hello"}, chunks)
}

func TestChunker_ChunkSize1(t *testing.T) {
	c := NewChunker(1)
	chunks := c.Chunk("Hello world test")
	assert.Equal(t, []string{"Hello ", "world ", "test"}, chunks)
}

func TestChunker_TrailingSpaces(t *testing.T) {
	c := NewChunker(2)
	chunks := c.Chunk("a b c d e f")
	// Non-final chunks should have trailing space.
	for i, chunk := range chunks {
		if i < len(chunks)-1 {
			assert.True(t, strings.HasSuffix(chunk, " "),
				"chunk %d should have trailing space: %q", i, chunk)
		}
	}
}

func TestChunker_ReassemblesCorrectly(t *testing.T) {
	c := NewChunker(3)
	original := "The quick brown fox jumps over the lazy dog"
	chunks := c.Chunk(original)

	reassembled := strings.Join(chunks, "")
	assert.Equal(t, original, reassembled)
}

func TestChunker_LargeChunkSize(t *testing.T) {
	c := NewChunker(100)
	content := "short text"
	chunks := c.Chunk(content)
	assert.Len(t, chunks, 1)
	assert.Equal(t, content, chunks[0])
}

func TestChunkString(t *testing.T) {
	assert.Equal(t, []string{"abcde", "fghij", "k"}, chunkString("abcdefghijk", 5))
	assert.Equal(t, []string{"abc"}, chunkString("abc", 10))
	assert.Equal(t, []string{""}, chunkString("", 5))
}

// ChunkArguments never cuts inside a UTF-8 sequence, at any alignment, and
// the pieces reassemble byte-exactly (2026-10-06 review E-02).
func TestChunkArguments_RuneSafeAtEveryOffset(t *testing.T) {
	for pad := 0; pad < 25; pad++ {
		s := `{"city":"` + strings.Repeat("Z", pad) + `ü日本😀é"}`
		for _, size := range []int{1, 2, 3, 4, 5, 20} {
			chunks := ChunkArguments(s, size)
			assert.Equal(t, s, strings.Join(chunks, ""), "pad=%d size=%d", pad, size)
			for _, c := range chunks {
				assert.True(t, utf8.ValidString(c), "pad=%d size=%d chunk %q is not valid UTF-8", pad, size, c)
			}
		}
	}
	assert.Equal(t, []string{""}, ChunkArguments("", 20))
}
