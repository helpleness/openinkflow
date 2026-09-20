package llm

import "strings"

func supportsTopK(baseURL string) bool {
	baseURL = strings.ToLower(baseURL)
	return !strings.Contains(baseURL, "generativelanguage.googleapis.com")
}
