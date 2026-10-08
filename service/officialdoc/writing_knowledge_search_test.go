package officialdoc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"InkFlow/global"
	model "InkFlow/model/officialdoc"
	response "InkFlow/model/officialdoc/response"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestFreezeKnowledgeSearchResultKeepsStableCitations(t *testing.T) {
	db := useWritingKnowledgeSearchTestDB(t)
	service := &WritingRunService{}

	first, err := service.freezeKnowledgeSearchResult(context.Background(), 42, "林晓雨", response.KnowledgeSearchResult{
		Items: []response.KnowledgeEvidence{
			{DocumentID: 1, DocumentName: "员工名册", ChunkID: 11, Title: "研发一组", Content: "林晓雨的直属上级是周文博。", Score: 0.9},
			{DocumentID: 2, DocumentName: "管理关系", ChunkID: 12, Title: "周文博", Content: "周文博的直属上级是沈岚。", Score: 0.8},
		},
	})
	if err != nil {
		t.Fatalf("freeze first result: %v", err)
	}
	if len(first.Items) != 2 || first.Items[0].Citation != "[E1]" || first.Items[1].Citation != "[E2]" {
		t.Fatalf("unexpected first citations: %#v", first.Items)
	}

	second, err := service.freezeKnowledgeSearchResult(context.Background(), 42, "沈岚", response.KnowledgeSearchResult{
		Items: []response.KnowledgeEvidence{
			{DocumentID: 99, DocumentName: "变化后的来源", ChunkID: 11, Title: "变化后的标题", Content: "不应覆盖已冻结内容", Score: 1},
			{DocumentID: 3, DocumentName: "高层管理", ChunkID: 13, Title: "沈岚", Content: "沈岚的直属上级是顾承泽。", Score: 0.7},
		},
	})
	if err != nil {
		t.Fatalf("freeze follow-up result: %v", err)
	}
	if len(second.Items) != 2 {
		t.Fatalf("expected two follow-up items, got %#v", second.Items)
	}
	if second.Items[0].Citation != "[E1]" || !second.Items[0].AlreadySeen {
		t.Fatalf("duplicate evidence should retain E1: %#v", second.Items[0])
	}
	if second.Items[0].Content != "林晓雨的直属上级是周文博。" {
		t.Fatalf("duplicate search overwrote frozen snapshot: %#v", second.Items[0])
	}
	if second.Items[1].Citation != "[E3]" || second.Items[1].AlreadySeen {
		t.Fatalf("new evidence should receive E3: %#v", second.Items[1])
	}

	var records []model.WritingRunEvidence
	if err := db.Where("run_id = ?", 42).Order("rank").Find(&records).Error; err != nil {
		t.Fatalf("read frozen evidence: %v", err)
	}
	if len(records) != 3 || records[0].Rank != 1 || records[1].Rank != 2 || records[2].Rank != 3 {
		t.Fatalf("unexpected frozen evidence: %#v", records)
	}
}

func TestRemainingKnowledgeSearchCallsCountsAllAttempts(t *testing.T) {
	db := useWritingKnowledgeSearchTestDB(t)
	traces := []model.WritingRunToolTrace{
		{RunID: 7, ToolName: writingKnowledgeSearchTool, Kind: "query", Status: "ok"},
		{RunID: 7, ToolName: writingKnowledgeSearchTool, Kind: "query", Status: "ok"},
		{RunID: 7, ToolName: writingKnowledgeSearchTool, Kind: "query", Status: "error"},
		{RunID: 7, ToolName: "writing.retrieve_evidence", Kind: "mutation", Status: "ok"},
		{RunID: 8, ToolName: writingKnowledgeSearchTool, Kind: "query", Status: "ok"},
	}
	if err := db.Create(&traces).Error; err != nil {
		t.Fatalf("create traces: %v", err)
	}

	remaining, err := (&WritingRunService{}).remainingKnowledgeSearchCalls(context.Background(), 7)
	if err != nil {
		t.Fatalf("remaining calls: %v", err)
	}
	if remaining != writingKnowledgeSearchMaxCalls-3 {
		t.Fatalf("expected %d remaining calls, got %d", writingKnowledgeSearchMaxCalls-3, remaining)
	}
}

func TestFrozenKnowledgeEvidenceKeepsCompleteContent(t *testing.T) {
	content := strings.Repeat("证", 1200)
	evidence := frozenKnowledgeEvidence(model.WritingRunEvidence{Rank: 9, ContentSnapshot: content}, false)
	if evidence.Citation != "[E9]" || evidence.Content != content {
		t.Fatalf("search evidence was unexpectedly shortened: %#v", evidence)
	}
}

func TestControlledWritingPromptIncludesAllFrozenEvidence(t *testing.T) {
	evidence := make([]response.KnowledgeEvidence, 0, 9)
	for rank := 1; rank <= 9; rank++ {
		evidence = append(evidence, response.KnowledgeEvidence{
			DocumentName: "测试文档", Title: fmt.Sprintf("第%d节", rank), Content: fmt.Sprintf("证据正文-%d", rank),
		})
	}
	_, prompt := controlledWritingPrompt("outline", &model.WritingTask{Title: "测试任务", Requirement: "验证上下文边界"}, model.DocumentTemplate{Name: "测试模板"}, evidence)
	if !strings.Contains(prompt, "证据正文-1") || !strings.Contains(prompt, "证据正文-9") {
		t.Fatalf("prompt lost complete frozen evidence: %s", prompt)
	}
	if strings.Contains(prompt, "writing.compress_evidence") {
		t.Fatalf("internal compactor leaked into the model prompt: %s", prompt)
	}
}

func TestWritingEvidenceSelectionRemovesOnlyChosenRecords(t *testing.T) {
	selection := newWritingEvidenceSelection(0)
	selection.observe([]writingKnowledgeSearchEvidence{
		{Citation: "[E1]", Document: "甲", Content: "必须保留的完整事实和数字 123"},
		{Citation: "[E2]", Document: "乙", Content: "无关材料"},
	})
	selection.observe([]writingKnowledgeSearchEvidence{
		{Citation: "[E1]", Document: "重复来源", Content: "不应覆盖原文"},
		{Citation: "[E3]", Document: "丙", Content: "后续检索的完整事实"},
	})
	candidates := selection.candidates
	if len(candidates) != 3 || candidates[0].Content != "必须保留的完整事实和数字 123" {
		t.Fatalf("candidates lost original evidence: %#v", candidates)
	}
	context, err := pruneWritingEvidenceContext(candidates, `{"drop":["E2"]}`)
	if err != nil {
		t.Fatalf("prune evidence: %v", err)
	}
	var retained writingPrunedEvidenceContext
	if err := json.Unmarshal([]byte(context), &retained); err != nil {
		t.Fatalf("decode retained evidence: %v", err)
	}
	if len(retained.Items) != 2 || retained.Items[0].Citation != "[E1]" || retained.Items[1].Citation != "[E3]" || retained.Items[0].Content != candidates[0].Content || retained.Items[1].Content != candidates[2].Content {
		t.Fatalf("retained evidence was altered: %#v", retained)
	}
	if len(retained.CandidateCitations) != 3 || retained.CandidateCitations[0] != "[E1]" || retained.CandidateCitations[1] != "[E2]" || retained.CandidateCitations[2] != "[E3]" {
		t.Fatalf("candidate IDs missing from audit result: %#v", retained.CandidateCitations)
	}
	if strings.Contains(context, "无关材料") || len(retained.RemovedCitations) != 1 || retained.RemovedCitations[0] != "[E2]" {
		t.Fatalf("removed evidence leaked into context: %s", context)
	}
	withUnknown, err := pruneWritingEvidenceContext(candidates, `{"drop":["[E2]","[E99]"]}`)
	if err != nil {
		t.Fatalf("unknown citation must not fail the writing run: %v", err)
	}
	var reviewed writingPrunedEvidenceContext
	if err := json.Unmarshal([]byte(withUnknown), &reviewed); err != nil {
		t.Fatalf("decode reviewed evidence: %v", err)
	}
	if len(reviewed.RemovedCitations) != 1 || reviewed.RemovedCitations[0] != "[E2]" || len(reviewed.IgnoredCitations) != 1 || reviewed.IgnoredCitations[0] != "[E99]" || len(reviewed.Items) != 2 {
		t.Fatalf("unknown citation affected retained evidence: %#v", reviewed)
	}
	pinnedSelection := newWritingEvidenceSelection(2)
	pinnedSelection.observe(candidates)
	pinned := pinnedSelection.candidates
	if len(pinned) != 1 || pinned[0].Citation != "[E3]" {
		t.Fatalf("initial prompt evidence was offered for removal: %#v", pinned)
	}
	withPinned, err := pruneWritingEvidenceContext(pinned, `{"drop":["[E1]","[E2]"]}`)
	if err != nil {
		t.Fatalf("pinned citations must be ignored: %v", err)
	}
	var pinnedReview writingPrunedEvidenceContext
	if err := json.Unmarshal([]byte(withPinned), &pinnedReview); err != nil || len(pinnedReview.Items) != 1 || pinnedReview.Items[0].Citation != "[E3]" {
		t.Fatalf("pinned citations removed a candidate: %#v, %v", pinnedReview, err)
	}
	if _, err := pruneWritingEvidenceContext(candidates, `{"drop":[]}`); err != nil {
		t.Fatalf("keeping all evidence should be valid: %v", err)
	}
	removed, err := selection.remove(json.RawMessage(`{"drop":["[E2]"]}`))
	if err != nil || len(removed.Items) != 2 {
		t.Fatalf("apply removal: %#v, %v", removed, err)
	}
	selection.observe([]writingKnowledgeSearchEvidence{{Citation: "[E2]", Content: "又返回的旧证据"}})
	if len(selection.candidates) != 2 {
		t.Fatalf("removed evidence re-entered context: %#v", selection.candidates)
	}
}

func useWritingKnowledgeSearchTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:writing-knowledge-search-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := db.AutoMigrate(&model.WritingRunEvidence{}, &model.WritingRunToolTrace{}); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	previousDB := global.GVA_DB
	global.GVA_DB = db
	t.Cleanup(func() {
		global.GVA_DB = previousDB
	})
	return db
}
