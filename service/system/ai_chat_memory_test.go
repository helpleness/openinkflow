package system

import (
	"strings"
	"testing"

	corellm "InkFlow/internal/ai/llm"
	model "InkFlow/model/system"

	"gorm.io/gorm"
)

func TestAIChatMemoryQueryKeepsCurrentQuestionAndLocalContext(t *testing.T) {
	recent := []model.SysAIChatMessage{
		{Role: string(corellm.RoleUser), Content: "请比较两个改造方案的成本。"},
		{Role: string(corellm.RoleAssistant), Content: "我已列出成本口径和风险。"},
		{Role: string(corellm.RoleUser), Content: "继续，并采用刚才的方案。"},
	}
	query := aiChatMemoryQuery(recent[2].Content, recent)
	for _, want := range []string{"继续，并采用刚才的方案。", "请比较两个改造方案的成本。", "我已列出成本口径和风险。"} {
		if !strings.Contains(query, want) {
			t.Fatalf("query %q does not retain %q", query, want)
		}
	}
}

func TestAIChatMemoryMessageKeepsQuestionAndAnswerTogether(t *testing.T) {
	memories := []model.SysAIChatTurnMemory{{
		Model:    gorm.Model{ID: 1},
		Question: "预算口径是什么？",
		Answer:   "以已批复项目预算为准。",
	}}
	message := aiChatMemoryMessage(memories)
	if !strings.Contains(message, "用户：预算口径是什么？") || !strings.Contains(message, "助手：以已批复项目预算为准。") {
		t.Fatalf("retrieved memory must include a complete question-and-answer pair: %q", message)
	}
}
