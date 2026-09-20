package chunker

import (
	"strings"
	"unicode"
)

func estimateLinesTokens(lines []lineSpan) int {
	return estimateTextTokens(joinLines(lines))
}

func estimateTextTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}

	tokens := 0
	inASCIIWord := false
	for _, r := range text {
		switch {
		case r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'):
			if !inASCIIWord {
				tokens++
				inASCIIWord = true
			}
		case unicode.IsSpace(r):
			inASCIIWord = false
		default:
			inASCIIWord = false
			if unicode.IsPunct(r) || unicode.IsSymbol(r) {
				continue
			}
			tokens++
		}
	}
	if tokens == 0 {
		return 1
	}
	return tokens
}

// EstimateTokens exposes the splitter's token estimate for incremental chunk updates.
func EstimateTokens(text string) int {
	return estimateTextTokens(text)
}
