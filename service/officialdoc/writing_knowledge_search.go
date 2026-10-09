package officialdoc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"InkFlow/global"
	model "InkFlow/model/officialdoc"
	response "InkFlow/model/officialdoc/response"
	"InkFlow/utils/toolchain/orchestrator"

	"gorm.io/gorm"
)

const (
	writingKnowledgeSearchTool         = "knowledge.search"
	writingKnowledgeSearchMaxCalls     = 12
	writingKnowledgeSearchDefaultLimit = 3
	writingKnowledgeSearchMaxLimit     = 4
	writingEvidenceCompressionTool     = "writing.compress_evidence"
	writingEvidenceCompressionMaxCalls = 4
	writingRunMaxEvidence              = 40
)

type writingKnowledgeSearchInput struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type writingKnowledgeSearchEvidence struct {
	Citation    string  `json:"citation"`
	Document    string  `json:"document"`
	Title       string  `json:"title"`
	Content     string  `json:"content"`
	Score       float64 `json:"score"`
	AlreadySeen bool    `json:"already_seen,omitempty"`
}

type writingKnowledgeSearchResult struct {
	Query                string                           `json:"query"`
	Items                []writingKnowledgeSearchEvidence `json:"items"`
	RemovableCitations   []string                         `json:"removable_citations,omitempty"`
	Warnings             []string                         `json:"warnings,omitempty"`
	EvidenceLimitReached bool                             `json:"evidence_limit_reached,omitempty"`
}

type writingEvidenceDropDecision struct {
	Drop []string `json:"drop"`
}

type writingPrunedEvidenceContext struct {
	CandidateCitations []string                         `json:"candidate_citations"`
	RemovedCitations   []string                         `json:"removed_citations"`
	IgnoredCitations   []string                         `json:"ignored_citations,omitempty"`
	Items              []writingKnowledgeSearchEvidence `json:"items"`
}

type writingEvidenceSelection struct {
	pinned     map[string]bool
	suppressed map[string]bool
	seen       map[string]bool
	candidates []writingKnowledgeSearchEvidence
}

func newWritingEvidenceSelection(pinnedCount int) *writingEvidenceSelection {
	selection := &writingEvidenceSelection{
		pinned: make(map[string]bool, pinnedCount), suppressed: make(map[string]bool), seen: make(map[string]bool),
	}
	for rank := 1; rank <= pinnedCount; rank++ {
		selection.pinned[fmt.Sprintf("[E%d]", rank)] = true
	}
	return selection
}

func (selection *writingEvidenceSelection) observe(items []writingKnowledgeSearchEvidence) {
	for _, item := range items {
		if selection.pinned[item.Citation] || selection.suppressed[item.Citation] || selection.seen[item.Citation] {
			continue
		}
		selection.seen[item.Citation] = true
		selection.candidates = append(selection.candidates, item)
	}
}

func (selection *writingEvidenceSelection) citations() []string {
	ids := make([]string, 0, len(selection.candidates))
	for _, item := range selection.candidates {
		ids = append(ids, item.Citation)
	}
	return ids
}

func (selection *writingEvidenceSelection) visibleEvidence(items []response.KnowledgeEvidence) []response.KnowledgeEvidence {
	visible := make([]response.KnowledgeEvidence, 0, len(items))
	for _, item := range items {
		if !selection.suppressed[item.Citation] {
			visible = append(visible, item)
		}
	}
	return visible
}

func (selection *writingEvidenceSelection) remove(raw json.RawMessage) (writingPrunedEvidenceContext, error) {
	encoded, err := pruneWritingEvidenceContext(selection.candidates, string(raw))
	if err != nil {
		return writingPrunedEvidenceContext{}, err
	}
	var retained writingPrunedEvidenceContext
	if err := json.Unmarshal([]byte(encoded), &retained); err != nil {
		return writingPrunedEvidenceContext{}, err
	}
	for _, citation := range retained.RemovedCitations {
		selection.suppressed[citation] = true
	}
	selection.candidates = retained.Items
	return retained, nil
}

func (service *WritingRunService) registerKnowledgeSearchTool(registry *orchestrator.Registry, runID uint, maxCalls int, selection *writingEvidenceSelection) error {
	if maxCalls <= 0 {
		return nil
	}
	return registry.Register(orchestrator.Tool{
		Name:        writingKnowledgeSearchTool,
		Kind:        orchestrator.KindQuery,
		Description: "在当前写作任务所属组织的知识库中搜索相关证据。query 既可以是需要连续出现的完整短语，也可以是用空格分隔的多个关键词；检索器会同时尝试完整短语和各关键词。需要多跳检索时，优先沿与目标直接相关的已证实关系继续查询。该工具只读，不会修改或删除知识文档。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "完整短语，或用空格分隔的多个关键词。多跳时可加入上一轮结果中新发现且与目标直接相连的实体或编号。",
					"minLength":   2,
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "本次最多返回的证据数，默认 3，最大 4。",
					"minimum":     1,
					"maximum":     writingKnowledgeSearchMaxLimit,
				},
			},
			"required":             []string{"query"},
			"additionalProperties": false,
		},
		Handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
			result, err := service.searchAndFreezeKnowledge(ctx, runID, raw)
			if err != nil {
				return nil, err
			}
			selection.observe(result.Items)
			result.RemovableCitations = selection.citations()
			return result, nil
		},
		MaxCallsPerRun:    maxCalls,
		MaxAttemptsPerRun: maxCalls,
		SummaryMaxRunes:   1400,
		// Search results must be complete while they are visible to the model.
		// The internal evidence selector can remove complete irrelevant records
		// from later model context without shortening any retained record.
		ContextMaxRunes: -1,
		StopOnError:     true,
	})
}

func (service *WritingRunService) registerEvidenceRemovalTool(registry *orchestrator.Registry, runID uint, selection *writingEvidenceSelection) error {
	return registry.Register(orchestrator.Tool{
		Name:        writingEvidenceCompressionTool,
		Kind:        orchestrator.KindQuery,
		Description: "从后续写作上下文移除不需要的整条新增检索证据。drop 只能填写 knowledge.search 返回的 removable_citations 中的编号；有疑问就保留。不会删除数据库中的冻结证据。请单独调用此工具，不要和知识检索并行调用。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"drop": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "要从后续上下文移除的 [E编号] 列表"},
			},
			"required": []string{"drop"}, "additionalProperties": false,
		},
		Handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
			run, err := service.findRun(ctx, runID)
			if err != nil {
				return nil, err
			}
			if run.CurrentStep != writingStepComposeDocument {
				return nil, fmt.Errorf("当前运行不在文稿生成步骤，不能整理证据上下文")
			}
			return selection.remove(raw)
		},
		MaxCallsPerRun: writingEvidenceCompressionMaxCalls, MaxAttemptsPerRun: writingEvidenceCompressionMaxCalls,
		SummaryMaxRunes: 2000, ContextMaxRunes: -1, DisableCache: true, StopOnError: false,
	})
}

func (service *WritingRunService) remainingKnowledgeSearchCalls(ctx context.Context, runID uint) (int, error) {
	var used int64
	if err := global.GVA_DB.WithContext(ctx).Model(&model.WritingRunToolTrace{}).
		Where("run_id = ? AND tool_name = ?", runID, writingKnowledgeSearchTool).
		Count(&used).Error; err != nil {
		return 0, fmt.Errorf("读取知识检索调用次数失败: %w", err)
	}
	remaining := writingKnowledgeSearchMaxCalls - int(used)
	if remaining < 0 {
		return 0, nil
	}
	return remaining, nil
}

func pruneWritingEvidenceContext(candidates []writingKnowledgeSearchEvidence, decisionText string) (string, error) {
	var decision writingEvidenceDropDecision
	if err := json.Unmarshal([]byte(decisionText), &decision); err != nil || decision.Drop == nil {
		return "", fmt.Errorf("证据整理工具需要有效的 drop 编号列表")
	}
	known := make(map[string]bool, len(candidates))
	candidateIDs := make([]string, 0, len(candidates))
	for _, item := range candidates {
		known[item.Citation] = true
		candidateIDs = append(candidateIDs, item.Citation)
	}
	dropped := make(map[string]bool, len(decision.Drop))
	removed := make([]string, 0, len(decision.Drop))
	ignored := make([]string, 0)
	ignoredSet := make(map[string]bool)
	for _, citation := range decision.Drop {
		citation = strings.TrimSpace(citation)
		if !strings.HasPrefix(citation, "[") {
			citation = "[" + citation + "]"
		}
		if !known[citation] {
			// The model can mention an ID already pinned in the original prompt,
			// or invent one. Neither is a removable candidate; keep all evidence.
			if !ignoredSet[citation] {
				ignoredSet[citation] = true
				ignored = append(ignored, citation)
			}
			continue
		}
		if !dropped[citation] {
			dropped[citation] = true
			removed = append(removed, citation)
		}
	}
	retained := make([]writingKnowledgeSearchEvidence, 0, len(candidates)-len(dropped))
	for _, item := range candidates {
		if !dropped[item.Citation] {
			retained = append(retained, item)
		}
	}
	encoded, err := json.Marshal(writingPrunedEvidenceContext{CandidateCitations: candidateIDs, RemovedCitations: removed, IgnoredCitations: ignored, Items: retained})
	if err != nil {
		return "", fmt.Errorf("序列化保留证据失败: %w", err)
	}
	return string(encoded), nil
}

func (service *WritingRunService) searchAndFreezeKnowledge(ctx context.Context, runID uint, raw json.RawMessage) (writingKnowledgeSearchResult, error) {
	var input writingKnowledgeSearchInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return writingKnowledgeSearchResult{}, fmt.Errorf("解析知识检索参数失败: %w", err)
	}
	input.Query = strings.TrimSpace(input.Query)
	if len([]rune(input.Query)) < 2 {
		return writingKnowledgeSearchResult{}, fmt.Errorf("知识检索词至少需要 2 个字符")
	}
	if input.Limit == 0 {
		input.Limit = writingKnowledgeSearchDefaultLimit
	}
	if input.Limit < 1 || input.Limit > writingKnowledgeSearchMaxLimit {
		return writingKnowledgeSearchResult{}, fmt.Errorf("单次知识检索数量必须在 1 到 %d 之间", writingKnowledgeSearchMaxLimit)
	}

	run, err := service.findRun(ctx, runID)
	if err != nil {
		return writingKnowledgeSearchResult{}, err
	}
	if run.CurrentStep != writingStepComposeDocument {
		return writingKnowledgeSearchResult{}, fmt.Errorf("当前运行不在文稿生成步骤，不能追加检索证据")
	}
	searchResult, err := ServiceGroupApp.KnowledgeSearchService.Search(
		ctx, run.TenantID, run.OrganizationID, run.StartedBy, input.Query, input.Limit,
	)
	if err != nil {
		return writingKnowledgeSearchResult{}, fmt.Errorf("搜索组织知识失败: %w", err)
	}
	return service.freezeKnowledgeSearchResult(ctx, run.ID, input.Query, searchResult)
}

func (service *WritingRunService) freezeKnowledgeSearchResult(ctx context.Context, runID uint, query string, searchResult response.KnowledgeSearchResult) (writingKnowledgeSearchResult, error) {
	output := writingKnowledgeSearchResult{Query: query, Items: []writingKnowledgeSearchEvidence{}, Warnings: searchResult.Warnings}
	err := global.GVA_DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing []model.WritingRunEvidence
		if err := tx.Where("run_id = ?", runID).Order("rank, id").Find(&existing).Error; err != nil {
			return err
		}
		byChunk := make(map[uint]model.WritingRunEvidence, len(existing))
		nextRank := 1
		for _, record := range existing {
			byChunk[record.ChunkID] = record
			if record.Rank >= nextRank {
				nextRank = record.Rank + 1
			}
		}

		for _, item := range searchResult.Items {
			if record, found := byChunk[item.ChunkID]; found {
				output.Items = append(output.Items, frozenKnowledgeEvidence(record, true))
				continue
			}
			if len(byChunk) >= writingRunMaxEvidence {
				output.EvidenceLimitReached = true
				break
			}
			record := model.WritingRunEvidence{
				RunID: runID, DocumentID: item.DocumentID, ChunkID: item.ChunkID,
				Rank: nextRank, Score: item.Score, DocumentName: item.DocumentName,
				ChunkTitle: item.Title, ContentSnapshot: item.Content,
			}
			if err := tx.Create(&record).Error; err != nil {
				return err
			}
			byChunk[item.ChunkID] = record
			nextRank++
			output.Items = append(output.Items, frozenKnowledgeEvidence(record, false))
		}
		return nil
	})
	if err != nil {
		return writingKnowledgeSearchResult{}, fmt.Errorf("冻结追加检索证据失败: %w", err)
	}
	return output, nil
}

func frozenKnowledgeEvidence(record model.WritingRunEvidence, alreadySeen bool) writingKnowledgeSearchEvidence {
	return writingKnowledgeSearchEvidence{
		Citation:    fmt.Sprintf("[E%d]", record.Rank),
		Document:    record.DocumentName,
		Title:       record.ChunkTitle,
		Content:     record.ContentSnapshot,
		Score:       record.Score,
		AlreadySeen: alreadySeen,
	}
}
