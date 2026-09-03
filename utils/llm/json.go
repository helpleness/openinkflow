package llm

import "strings"

// ExtractJSON tries to extract the first JSON payload from a model response.
// It removes common Markdown code fences like ```json ... ``` and falls back to
// slicing from the first '{'/'[' when the model adds extra prose.
func ExtractJSON(input string) string {
	s := strings.TrimSpace(input)
	if s == "" {
		return s
	}

	// 1) Prefer fenced blocks at the start: ```json ... ``` or ``` ... ```
	// We loop to tolerate accidental double-wrapping.
	for strings.HasPrefix(s, "```") {
		rest := s[3:]
		rest = strings.TrimLeft(rest, " \t\r\n")

		// Optional language label line.
		if k := strings.IndexByte(rest, '\n'); k >= 0 {
			head := strings.TrimSpace(rest[:k])
			if strings.EqualFold(head, "json") {
				rest = rest[k+1:]
			}
		}

		// Strip closing fence if present.
		if j := strings.Index(rest, "```"); j >= 0 {
			s = strings.TrimSpace(rest[:j])
		} else {
			// No closing fence; best effort.
			s = strings.TrimSpace(rest)
		}
	}

	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}

	// 2) If still not starting as JSON, try slicing from the first bracket.
	if s[0] != '{' && s[0] != '[' {
		iObj := strings.IndexByte(s, '{')
		iArr := strings.IndexByte(s, '[')
		i := -1
		if iObj >= 0 && iArr >= 0 {
			if iObj < iArr {
				i = iObj
			} else {
				i = iArr
			}
		} else if iObj >= 0 {
			i = iObj
		} else if iArr >= 0 {
			i = iArr
		}
		if i >= 0 {
			s = strings.TrimSpace(s[i:])
		}
	}

	// 3) Remove a trailing fence if the model appended it without a leading one.
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)

	// 4) Keep only the first complete JSON payload. Models occasionally append
	// explanations after valid JSON, which makes json.Unmarshal reject the
	// otherwise usable response.
	stack := make([]byte, 0, 32)
	inStr := false
	esc := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			if ch == '\\' {
				esc = true
				continue
			}
			if ch == '"' {
				inStr = false
			}
			continue
		}

		switch ch {
		case '"':
			inStr = true
		case '{', '[':
			stack = append(stack, ch)
		case '}', ']':
			if len(stack) == 0 {
				continue
			}
			top := stack[len(stack)-1]
			if (top == '{' && ch == '}') || (top == '[' && ch == ']') {
				stack = stack[:len(stack)-1]
				if len(stack) == 0 {
					return strings.TrimSpace(s[:i+1])
				}
			}
		}
	}
	return s
}

// RepairJSON attempts to fix a few common LLM JSON issues:
// - missing '}' before ']' (or missing ']' before '}')
// - unbalanced braces/brackets
// It does NOT try to fix invalid strings/quotes or missing commas.
func RepairJSON(input string) string {
	s := strings.TrimSpace(input)
	if s == "" {
		return s
	}

	// Strip a trailing fence just in case (ExtractJSON normally handles this).
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)

	// Stack of open delimiters: '{' or '['
	stack := make([]rune, 0, 64)
	var out strings.Builder
	out.Grow(len(s))

	inStr := false
	esc := false

	closeFor := func(open rune) rune {
		if open == '{' {
			return '}'
		}
		return ']'
	}
	matches := func(open, close rune) bool {
		return (open == '{' && close == '}') || (open == '[' && close == ']')
	}

	for _, r := range s {
		if inStr {
			out.WriteRune(r)
			if esc {
				esc = false
				continue
			}
			if r == '\\' {
				esc = true
				continue
			}
			if r == '"' {
				inStr = false
			}
			continue
		}

		switch r {
		case '"':
			inStr = true
			out.WriteRune(r)
		case '{', '[':
			stack = append(stack, r)
			out.WriteRune(r)
		case '}', ']':
			// If the top doesn't match, close what we can before writing this closer.
			for len(stack) > 0 && !matches(stack[len(stack)-1], r) {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				out.WriteRune(closeFor(top))
			}
			if len(stack) > 0 && matches(stack[len(stack)-1], r) {
				stack = stack[:len(stack)-1]
			}
			out.WriteRune(r)
		default:
			out.WriteRune(r)
		}
	}

	// Close remaining opens (only if not inside a string).
	if !inStr {
		for i := len(stack) - 1; i >= 0; i-- {
			out.WriteRune(closeFor(stack[i]))
		}
	}

	return strings.TrimSpace(out.String())
}
