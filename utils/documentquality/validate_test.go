package documentquality

import "testing"

func TestValidateFindsSensitiveDataAndIncompleteContent(t *testing.T) {
	findings := Validate("# 通知\n\n请联系张三，电话 13800138000，身份证号 110101199001011234。\n待补充", []string{"张三"})
	seen := map[string]bool{}
	for _, finding := range findings {
		seen[finding.Rule] = true
	}
	for _, rule := range []string{"phone_number", "identity_number", "placeholder", "custom_sensitive_term"} {
		if !seen[rule] {
			t.Fatalf("missing finding %q: %#v", rule, findings)
		}
	}
}

func TestValidateTemplateStructureRequiresFixedSections(t *testing.T) {
	findings := ValidateTemplateStructure("# 通知\n\n## 工作安排\n\n正文", "# {{标题}}\n\n## 工作安排\n\n## 落款")
	if len(findings) != 1 || findings[0].Rule != "template_section" || findings[0].Excerpt != "落款" {
		t.Fatalf("template findings = %#v", findings)
	}
}
