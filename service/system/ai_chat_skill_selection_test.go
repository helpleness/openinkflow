package system

import (
	"reflect"
	"testing"

	model "InkFlow/model/system"
	"gorm.io/gorm"
)

func TestManuallySelectedAISkillsUsesRequestedOrderAndEnabledCandidates(t *testing.T) {
	candidates := []model.SysAISkill{{Model: gorm.Model{ID: 7}, Name: "公文校对", Enabled: true}, {Model: gorm.Model{ID: 11}, Name: "数据分析", Enabled: true}}
	selected, err := manuallySelectedAISkills(candidates, []uint{11, 7, 11})
	if err != nil {
		t.Fatalf("manuallySelectedAISkills() error = %v", err)
	}
	got := []uint{selected[0].ID, selected[1].ID}
	if want := []uint{11, 7}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selected IDs = %v, want %v", got, want)
	}
	if _, err := manuallySelectedAISkills(candidates, []uint{99}); err == nil {
		t.Fatal("manual selection accepted a Skill outside the enabled candidate set")
	}
}

func TestAIChatRoutingTermsIncludesChineseNgramsAndLatinWords(t *testing.T) {
	terms := aiChatRoutingTerms("请用 MCP 分析数字政府项目")
	for _, term := range []string{"数字", "数字政", "数字政府", "mcp"} {
		if _, found := terms[term]; !found {
			t.Fatalf("routing terms missing %q: %v", term, terms)
		}
	}
}
