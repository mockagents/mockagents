package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEstimateTokens_Empty(t *testing.T) {
	assert.Equal(t, 0, EstimateTokens(""))
}

func TestEstimateTokens_SingleWord(t *testing.T) {
	tokens := EstimateTokens("hello")
	assert.Equal(t, 1, tokens)
}

func TestEstimateTokens_MultipleWords(t *testing.T) {
	tokens := EstimateTokens("hello world how are you")
	assert.Equal(t, 6, tokens) // 5 * 1.3 = 6.5 -> 6
}

func TestEstimateTokens_WithNewlines(t *testing.T) {
	tokens := EstimateTokens("hello\nworld\ntest")
	assert.Equal(t, 3, tokens) // 3 * 1.3 = 3.9 -> 3
}

func TestEstimateTokens_OnlyWhitespace(t *testing.T) {
	assert.Equal(t, 0, EstimateTokens("   "))
}

func TestEstimateTokens_LongText(t *testing.T) {
	text := "The quick brown fox jumps over the lazy dog and runs through the forest"
	tokens := EstimateTokens(text)
	assert.Greater(t, tokens, 10)
}
