// Package documentquality contains deterministic, auditable checks for
// controlled Chinese official-document drafts. It deliberately avoids calling
// an LLM so a result can be reproduced during review and audit.
package documentquality

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

type Finding struct {
	Rule     string   `json:"rule"`
	Category string   `json:"category"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	Line     int      `json:"line,omitempty"`
	Excerpt  string   `json:"excerpt,omitempty"`
}

var (
	phonePattern      = regexp.MustCompile(`(?:\+?86[- ]?)?1[3-9]\d{9}`)
	idCardPattern     = regexp.MustCompile(`\b\d{17}[0-9Xx]\b`)
	emailPattern      = regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`)
	placeholderMarker = regexp.MustCompile(`(?i)(?:\{\{.+?\}\}|\bTODO\b|\bXXX\b|待补充|待确认)`)
)

// Validate returns conservative findings. It does not reject publication on
// its own; workflow owners decide whether warnings require remediation.
func Validate(content string, extraSensitiveTerms []string) []Finding {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	lines := strings.Split(content, "\n")
	findings := make([]Finding, 0, 12)
	if utf8.RuneCountInString(strings.TrimSpace(content)) < 50 {
		findings = append(findings, Finding{Rule: "minimum_body", Category: "structure", Severity: SeverityError, Message: "正文少于 50 个字符，不能作为正式公文版本。"})
	}

	firstContentLine := 0
	previousHeadingLevel := 0
	for index, rawLine := range lines {
		lineNumber := index + 1
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if firstContentLine == 0 {
			firstContentLine = lineNumber
			if !strings.HasPrefix(line, "# ") {
				findings = append(findings, Finding{Rule: "document_title", Category: "structure", Severity: SeverityWarning, Message: "建议以一级 Markdown 标题作为公文标题，便于导出时应用正式标题样式。", Line: lineNumber, Excerpt: excerpt(line)})
			}
		}
		if level := headingLevel(line); level > 0 {
			if previousHeadingLevel > 0 && level > previousHeadingLevel+1 {
				findings = append(findings, Finding{Rule: "heading_hierarchy", Category: "format", Severity: SeverityWarning, Message: "标题层级跳跃，建议按一级、二级、三级顺序组织。", Line: lineNumber, Excerpt: excerpt(line)})
			}
			previousHeadingLevel = level
		}
		if utf8.RuneCountInString(line) > 140 {
			findings = append(findings, Finding{Rule: "paragraph_length", Category: "format", Severity: SeverityInfo, Message: "段落较长，建议拆分以提升公文可读性。", Line: lineNumber, Excerpt: excerpt(line)})
		}
		findings = append(findings, sensitiveFindings(line, lineNumber, extraSensitiveTerms)...)
		if marker := placeholderMarker.FindString(line); marker != "" {
			findings = append(findings, Finding{Rule: "placeholder", Category: "structure", Severity: SeverityError, Message: "检测到未完成占位内容，请在定稿前处理。", Line: lineNumber, Excerpt: marker})
		}
	}
	if firstContentLine == 0 {
		findings = append(findings, Finding{Rule: "document_content", Category: "structure", Severity: SeverityError, Message: "版本正文为空。"})
	}
	return findings
}

// ValidateTemplateStructure checks the fixed second- and third-level section
// headings expressed by an organization's template. A variable title is not
// treated as a fixed requirement, so each task remains free to use its own
// formal document title.
func ValidateTemplateStructure(content, template string) []Finding {
	required := templateHeadings(template)
	if len(required) == 0 {
		return nil
	}
	present := make(map[string]struct{})
	for _, raw := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if level := headingLevel(line); level >= 2 {
			present[normalizeHeading(strings.TrimSpace(line[level+1:]))] = struct{}{}
		}
	}
	findings := make([]Finding, 0)
	for _, heading := range required {
		if _, found := present[heading.text]; found {
			continue
		}
		findings = append(findings, Finding{Rule: "template_section", Category: "structure", Severity: SeverityError, Message: "缺少模板要求的章节：" + heading.display, Excerpt: heading.display})
	}
	return findings
}

type requiredHeading struct{ text, display string }

func templateHeadings(template string) []requiredHeading {
	seen := make(map[string]struct{})
	result := make([]requiredHeading, 0)
	for _, raw := range strings.Split(strings.ReplaceAll(template, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		level := headingLevel(line)
		if level < 2 {
			continue
		}
		display := strings.TrimSpace(line[level+1:])
		text := normalizeHeading(display)
		if text == "" || strings.Contains(text, "{{") {
			continue
		}
		if _, exists := seen[text]; exists {
			continue
		}
		seen[text] = struct{}{}
		result = append(result, requiredHeading{text: text, display: display})
	}
	return result
}

func normalizeHeading(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), ""))
}

func sensitiveFindings(line string, lineNumber int, terms []string) []Finding {
	result := make([]Finding, 0, 4)
	for _, pattern := range []struct {
		rule, message string
		value         *regexp.Regexp
	}{
		{"phone_number", "检测到手机号，确认是否应脱敏后再发布。", phonePattern},
		{"identity_number", "检测到疑似身份证号码，定稿前必须核实授权与脱敏要求。", idCardPattern},
		{"email_address", "检测到邮箱地址，确认是否属于允许公开的联系方式。", emailPattern},
	} {
		if matched := pattern.value.FindString(line); matched != "" {
			result = append(result, Finding{Rule: pattern.rule, Category: "sensitive", Severity: SeverityWarning, Message: pattern.message, Line: lineNumber, Excerpt: matched})
		}
	}
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term != "" && strings.Contains(line, term) {
			result = append(result, Finding{Rule: "custom_sensitive_term", Category: "sensitive", Severity: SeverityWarning, Message: "检测到配置的敏感词，发布前请复核。", Line: lineNumber, Excerpt: term})
		}
	}
	return result
}

func headingLevel(line string) int {
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level > 0 && level < len(line) && line[level] == ' ' {
		return level
	}
	return 0
}

func excerpt(value string) string {
	const maximum = 56
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum]) + "…"
}
