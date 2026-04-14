package adapter

// EstimateTokens approximates token count from text using a word-based heuristic.
// Roughly: word_count * 1.3, which approximates BPE tokenization.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	wordCount := 0
	inWord := false
	for _, r := range text {
		if r == ' ' || r == '\n' || r == '\t' || r == '\r' {
			inWord = false
		} else if !inWord {
			inWord = true
			wordCount++
		}
	}
	tokens := int(float64(wordCount) * 1.3)
	if tokens == 0 && wordCount > 0 {
		tokens = 1
	}
	return tokens
}
