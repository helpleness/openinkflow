package utils

import (
	"encoding/json"
	"fmt"
	"strings"

	"InkFlow/config"
	llmutil "InkFlow/utils/llm"
)

// UnmarshalModelJSON 在解析前修复常见的模型 JSON 格式问题。
func UnmarshalModelJSON(resp string, out any) error {
	clean := RepairJSON(ExtractJSON(resp))
	if clean == "" {
		return fmt.Errorf("模型返回了空 JSON")
	}
	if err := json.Unmarshal([]byte(clean), out); err != nil {
		return fmt.Errorf("%w，清洗后内容: %s", err, PrefixRunes(clean, 320))
	}
	return nil
}

// ExtractJSON 从模型输出中提取第一个完整的 JSON 对象或数组，并兼容常见的
// Markdown 代码围栏。字符串里的括号不会被误认为 JSON 结构边界。
func ExtractJSON(input string) string {
	s := strings.TrimSpace(input)
	for strings.HasPrefix(s, "```") {
		rest := strings.TrimLeft(s[3:], " \t\r\n")
		if newline := strings.IndexByte(rest, '\n'); newline >= 0 && strings.EqualFold(strings.TrimSpace(rest[:newline]), "json") {
			rest = rest[newline+1:]
		}
		if closing := strings.Index(rest, "```"); closing >= 0 {
			s = strings.TrimSpace(rest[:closing])
		} else {
			s = strings.TrimSpace(rest)
		}
	}
	if s == "" {
		return ""
	}
	if s[0] != '{' && s[0] != '[' {
		object, array := strings.IndexByte(s, '{'), strings.IndexByte(s, '[')
		start := object
		if start < 0 || array >= 0 && array < start {
			start = array
		}
		if start >= 0 {
			s = strings.TrimSpace(s[start:])
		}
	}
	s = strings.TrimSpace(strings.TrimSuffix(s, "```"))
	stack := make([]byte, 0, 32)
	inString, escaped := false, false
	for index := 0; index < len(s); index++ {
		char := s[index]
		if inString {
			if escaped {
				escaped = false
			} else if char == '\\' {
				escaped = true
			} else if char == '"' {
				inString = false
			}
			continue
		}
		switch char {
		case '"':
			inString = true
		case '{', '[':
			stack = append(stack, char)
		case '}', ']':
			if len(stack) == 0 {
				continue
			}
			top := stack[len(stack)-1]
			if top == '{' && char == '}' || top == '[' && char == ']' {
				stack = stack[:len(stack)-1]
				if len(stack) == 0 {
					return strings.TrimSpace(s[:index+1])
				}
			}
		}
	}
	return s
}

// RepairJSON 只平衡 JSON 的结构分隔符，不会虚构缺失的值、逗号或字符串内容。
// 它适合作为可选 LLM 修复前的本地、无副作用预处理。
func RepairJSON(input string) string {
	s := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(input), "```"))
	if s == "" {
		return ""
	}
	stack := make([]rune, 0, 64)
	var output strings.Builder
	inString, escaped := false, false
	closeFor := func(open rune) rune {
		if open == '{' {
			return '}'
		}
		return ']'
	}
	matches := func(open, close rune) bool { return open == '{' && close == '}' || open == '[' && close == ']' }
	for _, char := range s {
		if inString {
			output.WriteRune(char)
			if escaped {
				escaped = false
			} else if char == '\\' {
				escaped = true
			} else if char == '"' {
				inString = false
			}
			continue
		}
		switch char {
		case '"':
			inString = true
			output.WriteRune(char)
		case '{', '[':
			stack = append(stack, char)
			output.WriteRune(char)
		case '}', ']':
			for len(stack) > 0 && !matches(stack[len(stack)-1], char) {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				output.WriteRune(closeFor(top))
			}
			if len(stack) > 0 && matches(stack[len(stack)-1], char) {
				stack = stack[:len(stack)-1]
			}
			output.WriteRune(char)
		default:
			output.WriteRune(char)
		}
	}
	if !inString {
		for index := len(stack) - 1; index >= 0; index-- {
			output.WriteRune(closeFor(stack[index]))
		}
	}
	return strings.TrimSpace(output.String())
}

// RepairAndUnmarshalModelJSON 使用配置的 LLM 作为解析失败后的兜底修复器。
func RepairAndUnmarshalModelJSON(resp, schema string, out any) error {
	return RepairAndUnmarshalModelJSONWithLLM(resp, schema, out, nil)
}

func RepairAndUnmarshalModelJSONWithLLM(resp, schema string, out any, cfg *config.LLM) error {
	firstErr := UnmarshalModelJSON(resp, out)
	if firstErr == nil {
		return nil
	}
	repaired, repairErr := llmutil.GenerateWithOptions(
		"你是 JSON 修复器。只修复格式，不要改写、删减或补充业务内容。必须只返回一个合法 JSON 值，不要输出 Markdown 或解释。",
		fmt.Sprintf("目标 JSON 结构：\n%s\n\n待修复输出：\n%s", schema, resp),
		llmutil.GenerateOptions{Temperature: 0.1, MaxTokens: 8192, LLM: cfg},
		false,
	)
	if repairErr != nil {
		return fmt.Errorf("首次解析失败: %v；JSON 修复调用失败: %w", firstErr, repairErr)
	}
	if err := UnmarshalModelJSON(repaired, out); err != nil {
		return fmt.Errorf("首次解析失败: %v；修复后仍解析失败: %w", firstErr, err)
	}
	return nil
}

// StringFromMapPath reads a dotted path from a decoded JSON object. It is
// intended for provider responses whose field names are configurable, such as
// "email" or "user.email".
func StringFromMapPath(value map[string]any, path string) (string, bool) {
	if value == nil || path == "" {
		return "", false
	}
	parts := strings.Split(path, ".")
	var current any = value
	for _, part := range parts {
		currentMap, ok := current.(map[string]any)
		if !ok {
			return "", false
		}
		current, ok = currentMap[part]
		if !ok {
			return "", false
		}
	}
	return ValueToString(current)
}

// ValueToString converts decoded JSON scalar values to their textual form.
// Non-scalar values are kept as JSON so callers never silently stringify a
// struct-like value into Go's default fmt rendering.
func ValueToString(value any) (string, bool) {
	switch v := value.(type) {
	case nil:
		return "", false
	case string:
		return v, true
	case json.Number:
		return v.String(), true
	case float64:
		return fmt.Sprintf("%v", v), true
	case bool:
		return fmt.Sprintf("%v", v), true
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return "", false
		}
		return string(data), true
	}
}

// Summarize serializes a value as JSON and bounds the result by Unicode rune
// count. If serialization fails, it falls back to the value's display form so
// diagnostics are still available.
func Summarize(value any, maxRunes int) string {
	if value == nil {
		return ""
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return TruncateRunes(fmt.Sprint(value), maxRunes)
	}
	return TruncateRunes(string(encoded), maxRunes)
}

func CleanModelText(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	return strings.TrimSpace(strings.TrimSuffix(text, "```"))
}
