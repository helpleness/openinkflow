package vectorstore

import (
	"strings"
	"unicode"
)

const maxLexicalQueryTerms = 24

// LexicalQueryTerms keeps the normalized full phrase and also turns a
// natural-language query into bounded search terms. It is language-neutral for
// whitespace-delimited text and additionally emits trigrams for longer CJK runs,
// so exact phrase and individual-term matching can work together.
func LexicalQueryTerms(query string) []string {
	terms := make([]string, 0, maxLexicalQueryTerms)
	seen := make(map[string]struct{}, maxLexicalQueryTerms)
	add := func(value string) bool {
		value = strings.TrimSpace(value)
		if len([]rune(value)) < 2 {
			return true
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			return true
		}
		seen[key] = struct{}{}
		terms = append(terms, value)
		return len(terms) < maxLexicalQueryTerms
	}
	// Keep phrase matching as the strongest signal. The remaining terms provide
	// recall when spaces, punctuation or wording differ from the indexed text.
	if !add(strings.Join(strings.Fields(query), " ")) {
		return terms
	}

	segments := strings.FieldsFunc(query, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
	})
	for _, segment := range segments {
		if !add(segment) {
			break
		}
		for _, run := range splitLexicalScriptRuns(segment) {
			if run != segment && !add(run) {
				break
			}
			runes := []rune(run)
			if !allHan(runes) || len(runes) <= 3 {
				continue
			}
			for start := 0; start+3 <= len(runes); start++ {
				if !add(string(runes[start : start+3])) {
					break
				}
			}
			if len(terms) >= maxLexicalQueryTerms {
				break
			}
		}
		if len(terms) >= maxLexicalQueryTerms {
			break
		}
	}
	return terms
}

func splitLexicalScriptRuns(value string) []string {
	runes := []rune(value)
	if len(runes) == 0 {
		return nil
	}
	runs := make([]string, 0, 3)
	start := 0
	previousClass := lexicalRuneClass(runes[0])
	for index := 1; index < len(runes); index++ {
		class := lexicalRuneClass(runes[index])
		if class == previousClass {
			continue
		}
		runs = append(runs, string(runes[start:index]))
		start = index
		previousClass = class
	}
	return append(runs, string(runes[start:]))
}

func lexicalRuneClass(r rune) int {
	switch {
	case r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
		return 1
	case unicode.Is(unicode.Han, r):
		return 2
	default:
		return 3
	}
}

func allHan(value []rune) bool {
	if len(value) == 0 {
		return false
	}
	for _, r := range value {
		if !unicode.Is(unicode.Han, r) {
			return false
		}
	}
	return true
}
