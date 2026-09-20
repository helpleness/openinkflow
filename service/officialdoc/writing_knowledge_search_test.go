package officialdoc

import (
	"context"
	"fmt"
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
