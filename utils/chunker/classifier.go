package chunker

import "strings"

// DetectSectionType 根据文本内容探测区块类型

func DetectSectionType(path []string, content string) string {
	text := strings.ToLower(strings.Join(path, " ") + " " + content)
	//todo 把这些映射规则写到配置文件或数据库里，而不是硬编码在 Go 代码里。
	switch {
	case containsAny(text, "规则", "法则", "限制", "sop", "标准", "等级", "rule", "rules"):
		return "rule"
	case containsAny(text, "角色", "人物", "主角", "档案", "character"):
		return "character"
	case containsAny(text, "地点", "城市", "茶铺", "洞天", "location"):
		return "location"
	case containsAny(text, "物品", "道具", "术", "植物", "item", "spell"):
		return "entity"
	default:
		return "lore"
	}
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}
