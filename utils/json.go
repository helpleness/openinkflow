package utils

import (
	"encoding/json"
	"fmt"
	"strings"

	"InkFlow/config"
	llmutil "InkFlow/utils/llm"
)

// CleanModelJSON 提取模型输出中的 JSON 内容，供各领域模块共用。
func CleanModelJSON(resp string) string {
	return llmutil.ExtractJSON(resp)
}

// UnmarshalModelJSON 在解析前修复常见的模型 JSON 格式问题。
func UnmarshalModelJSON(resp string, out any) error {
	clean := llmutil.RepairJSON(CleanModelJSON(resp))
	if clean == "" {
		return fmt.Errorf("模型返回了空 JSON")
	}
	if err := json.Unmarshal([]byte(clean), out); err != nil {
		return fmt.Errorf("%w，清洗后内容: %s", err, PrefixRunes(clean, 320))
	}
	return nil
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
