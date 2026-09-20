package officialdoc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"InkFlow/global"
	commonResponse "InkFlow/model/common/response"
	model "InkFlow/model/officialdoc"
	response "InkFlow/model/officialdoc/response"
	systemModel "InkFlow/model/system"
	llmutil "InkFlow/utils/llm"
	"InkFlow/utils/storage"
	"InkFlow/utils/vectorstore"

	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
)

const (
	knowledgeChunkCollection     vectorstore.Collection = "officialdoc_knowledge_chunks"
	knowledgeSearchMaxCandidates                        = 24
)

// KnowledgeSearchService owns document indexing, hybrid retrieval and evidence lookup.
// Its relational queries always include tenant and organization scope before calling a store.
type KnowledgeSearchService struct{}

type knowledgeCandidate struct {
	chunk                   model.KnowledgeChunk
	vectorRank, lexicalRank int
	score                   float64
}

func (service *KnowledgeSearchService) ListDocuments(ctx context.Context, tenantID, organizationID, userID uint) ([]response.KnowledgeDocumentView, error) {
	if err := ensureKnowledgeMember(ctx, tenantID, organizationID, userID); err != nil {
		return nil, err
	}
	var documents []model.KnowledgeDocument
	db := global.GVA_DB
	if err := db.WithContext(ctx).Where("tenant_id = ? AND organization_id = ?", tenantID, organizationID).Order("created_at DESC").Find(&documents).Error; err != nil {
		return nil, err
	}
	views := make([]response.KnowledgeDocumentView, 0, len(documents))
	for _, document := range documents {
		views = append(views, documentView(document))
	}
	return views, nil
}

func (service *KnowledgeSearchService) GetDocument(ctx context.Context, tenantID, documentID, userID uint) (response.KnowledgeDocumentView, []model.KnowledgeChunk, error) {
	db := global.GVA_DB
	var document model.KnowledgeDocument
	if err := db.WithContext(ctx).Where("id = ? AND tenant_id = ?", documentID, tenantID).First(&document).Error; err != nil {
		return response.KnowledgeDocumentView{}, nil, err
	}
	if err := ensureKnowledgeMember(ctx, tenantID, document.OrganizationID, userID); err != nil {
		return response.KnowledgeDocumentView{}, nil, err
	}
	var chunks []model.KnowledgeChunk
	if err := db.WithContext(ctx).Where("document_id = ?", document.ID).Order("chunk_index").Find(&chunks).Error; err != nil {
		return response.KnowledgeDocumentView{}, nil, err
	}
	return documentView(document), chunks, nil
}

// DownloadDocument signs a short-lived private OSS URL only after resolving the
// document's tenant and organization membership. The stored key is never
// returned in API responses.
func (service *KnowledgeSearchService) DownloadDocument(ctx context.Context, tenantID, documentID, userID uint) (response.KnowledgeDocumentDownload, error) {
	db := global.GVA_DB
	var document model.KnowledgeDocument
	if err := db.WithContext(ctx).Where("id = ? AND tenant_id = ?", documentID, tenantID).First(&document).Error; err != nil {
		return response.KnowledgeDocumentDownload{}, err
	}
	if err := ensureKnowledgeMember(ctx, tenantID, document.OrganizationID, userID); err != nil {
		return response.KnowledgeDocumentDownload{}, err
	}
	if !storage.IsKnowledgeObjectKeyForOrganization(document.OrganizationID, document.ObjectKey) {
		return response.KnowledgeDocumentDownload{}, fmt.Errorf("文档对象路径无效，无法提供下载")
	}
	objectStore, err := knowledgeObjectStorage()
	if err != nil {
		return response.KnowledgeDocumentDownload{}, err
	}
	expiration, err := knowledgeSignedURLExpiration()
	if err != nil {
		return response.KnowledgeDocumentDownload{}, err
	}
	signedURL, err := objectStore.SignedGetURL(ctx, document.ObjectKey, expiration)
	if err != nil {
		logKnowledgeStorageFailure("sign_get_url", "download", &document, document.ObjectKey, err)
		return response.KnowledgeDocumentDownload{}, fmt.Errorf("生成文档下载地址失败: %w", err)
	}
	return response.KnowledgeDocumentDownload{URL: signedURL, ExpiresAt: time.Now().Add(expiration)}, nil
}

func (service *KnowledgeSearchService) DeleteDocument(ctx context.Context, tenantID, documentID, userID uint) error {
	db := global.GVA_DB
	var document model.KnowledgeDocument
	if err := db.WithContext(ctx).Where("id = ? AND tenant_id = ?", documentID, tenantID).First(&document).Error; err != nil {
		return err
	}
	if err := ensureKnowledgeMember(ctx, tenantID, document.OrganizationID, userID); err != nil {
		return err
	}
	if !storage.IsKnowledgeObjectKeyForOrganization(document.OrganizationID, document.ObjectKey) {
		return fmt.Errorf("文档对象路径无效，拒绝删除")
	}
	objectStore, err := knowledgeObjectStorage()
	if err != nil {
		return err
	}
	var images []model.KnowledgeImage
	if err := db.WithContext(ctx).Where("document_id = ?", document.ID).Find(&images).Error; err != nil {
		return err
	}
	var chunks []model.KnowledgeChunk
	if err := db.WithContext(ctx).Where("document_id = ?", document.ID).Find(&chunks).Error; err != nil {
		return err
	}
	objectKeys := make([]string, 0, len(images)+1)
	objectKeys = append(objectKeys, document.ObjectKey)
	for _, image := range images {
		if !storage.IsKnowledgeObjectKeyForOrganization(document.OrganizationID, image.ObjectKey) {
			return fmt.Errorf("内嵌图片对象路径无效，拒绝删除")
		}
		objectKeys = append(objectKeys, image.ObjectKey)
	}
	// DeleteObject is idempotent in OSS. If a later vector/database operation
	// fails, the document stays visible with delete_failed and a retry resumes the
	// same lifecycle instead of silently leaving an untracked object behind.
	for _, key := range objectKeys {
		if err := objectStore.Delete(ctx, key); err != nil {
			logKnowledgeStorageFailure("delete", "oss", &document, key, err)
			markKnowledgeDocumentDeletionFailed(ctx, &document, err)
			return fmt.Errorf("删除 OSS 文档对象失败，未删除数据库记录: %w", err)
		}
	}
	if global.GVA_VECTOR_STORE != nil && len(chunks) > 0 {
		keys := make([]vectorstore.StoreRequest, 0, len(chunks))
		for _, chunk := range chunks {
			keys = append(keys, vectorstore.StoreRequest{Collection: knowledgeChunkCollection, ID: chunk.ID})
		}
		if err := global.GVA_VECTOR_STORE.Delete(ctx, keys); err != nil {
			markKnowledgeDocumentDeletionFailed(ctx, &document, err)
			return fmt.Errorf("OSS 对象已删除，但删除向量索引失败；可重试删除: %w", err)
		}
	}
	if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("document_id = ?", document.ID).Delete(&model.KnowledgeImage{}).Error; err != nil {
			return err
		}
		if err := tx.Where("document_id = ?", document.ID).Delete(&model.KnowledgeChunk{}).Error; err != nil {
			return err
		}
		return tx.Delete(&document).Error
	}); err != nil {
		markKnowledgeDocumentDeletionFailed(ctx, &document, err)
		return fmt.Errorf("OSS 对象已删除，但删除数据库记录失败；可重试删除: %w", err)
	}
	return nil
}

// IndexDocument generates embeddings and synchronizes the selected vector backend.
// The function is intentionally explicit so an administrator can re-index documents after
// changing an embedding model or restoring a local HNSW index.
func (service *KnowledgeSearchService) IndexDocument(ctx context.Context, tenantID, documentID, userID uint) (*model.KnowledgeDocument, error) {
	db := global.GVA_DB
	var document model.KnowledgeDocument
	if err := db.WithContext(ctx).Where("id = ? AND tenant_id = ?", documentID, tenantID).First(&document).Error; err != nil {
		return nil, err
	}
	if err := ensureKnowledgeMember(ctx, tenantID, document.OrganizationID, userID); err != nil {
		return nil, err
	}
	if err := db.WithContext(ctx).Model(&document).Updates(map[string]any{"status": "indexing", "failure_reason": "", "indexed_at": nil}).Error; err != nil {
		return nil, err
	}
	var chunks []model.KnowledgeChunk
	if err := db.WithContext(ctx).Where("document_id = ?", document.ID).Order("chunk_index").Find(&chunks).Error; err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		return service.indexFailure(ctx, &document, "文档没有可索引的切片")
	}
	if global.GVA_VECTOR_STORE == nil {
		return service.indexFailure(ctx, &document, "向量索引尚未初始化")
	}
	records := make([]vectorstore.StoreRequest, 0, len(chunks))
	now := time.Now()
	for index := range chunks {
		text := strings.TrimSpace(strings.Join([]string{chunks[index].Title, chunks[index].ParentTitle, chunks[index].Content}, "\n"))
		vector, err := llmutil.GetEmbedding(ctx, text)
		if err != nil {
			return service.indexFailure(ctx, &document, fmt.Sprintf("第 %d 个切片生成向量失败: %v", index+1, err))
		}
		if len(vector) == 0 {
			return service.indexFailure(ctx, &document, fmt.Sprintf("第 %d 个切片未返回向量", index+1))
		}
		expectedDimension := global.GVA_CONFIG.RAG.VectorDimension
		if expectedDimension <= 0 {
			expectedDimension = 1024
		}
		if len(vector) != expectedDimension {
			return service.indexFailure(ctx, &document, fmt.Sprintf("第 %d 个切片向量维度为 %d，与 rag.vector-dimension=%d 不一致", index+1, len(vector), expectedDimension))
		}
		embedding := pgvector.NewVector(vector)
		chunks[index].Embedding = &embedding
		chunks[index].IndexedAt = &now
		if err := db.WithContext(ctx).Model(&model.KnowledgeChunk{}).Where("id = ?", chunks[index].ID).Updates(map[string]any{"embedding": chunks[index].Embedding, "indexed_at": now}).Error; err != nil {
			return service.indexFailure(ctx, &document, fmt.Sprintf("保存第 %d 个切片向量失败: %v", index+1, err))
		}
		records = append(records, vectorstore.StoreRequest{Collection: knowledgeChunkCollection, ID: chunks[index].ID, Vector: vector})
	}
	if err := global.GVA_VECTOR_STORE.Upsert(ctx, records); err != nil {
		return service.indexFailure(ctx, &document, fmt.Sprintf("同步向量索引失败: %v", err))
	}
	document.Status = "ready"
	document.FailureReason = ""
	document.IndexedAt = &now
	if err := db.WithContext(ctx).Model(&document).Updates(map[string]any{"status": document.Status, "failure_reason": "", "indexed_at": now}).Error; err != nil {
		return nil, err
	}
	return &document, nil
}

func (service *KnowledgeSearchService) indexFailure(ctx context.Context, document *model.KnowledgeDocument, reason string) (*model.KnowledgeDocument, error) {
	document.Status = "index_failed"
	document.FailureReason = reason
	if err := global.GVA_DB.WithContext(ctx).Model(document).Updates(map[string]any{"status": document.Status, "failure_reason": reason}).Error; err != nil {
		return nil, err
	}
	return document, fmt.Errorf("知识库索引失败: %s", reason)
}

// Search retrieves bounded vector and lexical candidate lists, unions them by
// chunk ID, and sends the complete union to the required reranker.
func (service *KnowledgeSearchService) Search(ctx context.Context, tenantID, organizationID, userID uint, query string, limit int) (response.KnowledgeSearchResult, error) {
	if err := ensureKnowledgeMember(ctx, tenantID, organizationID, userID); err != nil {
		return response.KnowledgeSearchResult{}, err
	}
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 2 {
		return response.KnowledgeSearchResult{}, fmt.Errorf("检索词至少需要 2 个字符")
	}
	if limit <= 0 || limit > 20 {
		limit = 8
	}
	db := global.GVA_DB
	scope := db.Model(&model.KnowledgeChunk{}).Where("tenant_id = ? AND organization_id = ?", tenantID, organizationID)
	// 向量检索只考虑确实完成向量化的切片；词法检索则仍能覆盖刚导入、
	// 正在等待本地模型的内容，保证索引暂不可用时知识库不会整体失效。
	vectorBase := scope.Where("embedding IS NOT NULL")
	lexicalBase := db.Model(&model.KnowledgeChunk{}).Where("tenant_id = ? AND organization_id = ?", tenantID, organizationID)
	// Split the bounded candidate budget evenly between the two independent
	// recall channels. Their complete deduplicated union is then reranked.
	recallLimit := limit * 3
	if perChannelLimit := knowledgeSearchMaxCandidates / 2; recallLimit > perChannelLimit {
		recallLimit = perChannelLimit
	}
	vectorChunks, vectorErr := searchKnowledgeVectors(ctx, vectorBase, query, recallLimit)
	lexicalChunks, lexicalErr := searchKnowledgeLexical(ctx, lexicalBase, query, recallLimit)
	if len(vectorChunks) == 0 && len(lexicalChunks) == 0 && vectorErr != nil && lexicalErr != nil {
		return response.KnowledgeSearchResult{}, fmt.Errorf("向量和词法检索均不可用: %v；%v", vectorErr, lexicalErr)
	}

	candidates := make(map[uint]*knowledgeCandidate, len(vectorChunks)+len(lexicalChunks))
	candidateOrder := make([]uint, 0, len(vectorChunks)+len(lexicalChunks))
	for index, chunk := range vectorChunks {
		if candidates[chunk.ID] == nil {
			candidateOrder = append(candidateOrder, chunk.ID)
		}
		candidates[chunk.ID] = &knowledgeCandidate{chunk: chunk, vectorRank: index + 1}
	}
	for index, chunk := range lexicalChunks {
		candidateItem := candidates[chunk.ID]
		if candidateItem == nil {
			candidateItem = &knowledgeCandidate{chunk: chunk}
			candidates[chunk.ID] = candidateItem
			candidateOrder = append(candidateOrder, chunk.ID)
		}
		candidateItem.lexicalRank = index + 1
	}
	ordered := make([]knowledgeCandidate, 0, len(candidates))
	for _, chunkID := range candidateOrder {
		ordered = append(ordered, *candidates[chunkID])
	}

	warnings := make([]string, 0, 2)
	if vectorErr != nil {
		warnings = append(warnings, "向量召回不可用，已使用词法检索："+vectorErr.Error())
	}
	if lexicalErr != nil {
		warnings = append(warnings, "词法召回不可用，已使用向量检索："+lexicalErr.Error())
	}
	if len(ordered) == 0 {
		return response.KnowledgeSearchResult{Items: []response.KnowledgeEvidence{}, Warnings: warnings}, nil
	}

	reranked, err := rerankKnowledge(ctx, query, ordered, limit)
	if err != nil {
		return response.KnowledgeSearchResult{}, fmt.Errorf("知识检索重排失败: %w", err)
	}
	ordered = reranked
	return response.KnowledgeSearchResult{Items: knowledgeEvidenceViews(ctx, ordered), Warnings: warnings}, nil
}

func searchKnowledgeVectors(ctx context.Context, base *gorm.DB, query string, limit int) ([]model.KnowledgeChunk, error) {
	if global.GVA_VECTOR_STORE == nil {
		return nil, vectorstore.ErrNotConfigured
	}
	vector, err := llmutil.GetEmbedding(ctx, query)
	if err != nil {
		return nil, err
	}
	queryDB, err := global.GVA_VECTOR_STORE.Search(ctx, vectorstore.StoreRequest{Collection: knowledgeChunkCollection, Vector: vector, Limit: limit, Db: base})
	if err != nil {
		return nil, err
	}
	var chunks []model.KnowledgeChunk
	if err := queryDB.Find(&chunks).Error; err != nil {
		return nil, err
	}
	return chunks, nil
}

func searchKnowledgeLexical(ctx context.Context, base *gorm.DB, query string, limit int) ([]model.KnowledgeChunk, error) {
	if global.GVA_LEXICAL_STORE != nil {
		queryDB, err := global.GVA_LEXICAL_STORE.Search(ctx, vectorstore.StoreRequest{Collection: knowledgeChunkCollection, Query: query, Limit: limit, Db: base})
		if err == nil {
			var chunks []model.KnowledgeChunk
			if err = queryDB.Find(&chunks).Error; err == nil {
				return chunks, nil
			}
		}
	}
	terms := vectorstore.LexicalQueryTerms(query)
	if len(terms) == 0 {
		return []model.KnowledgeChunk{}, nil
	}
	whereParts := make([]string, 0, len(terms))
	whereArgs := make([]any, 0, len(terms)*3)
	scoreParts := make([]string, 0, len(terms))
	scoreArgs := make([]any, 0, len(terms)*3)
	for _, term := range terms {
		// LIKE values remain parameters. Removing wildcard metacharacters keeps a
		// model-generated query from accidentally widening the candidate set.
		term = strings.ReplaceAll(strings.ReplaceAll(term, "%", ""), "_", "")
		if term == "" {
			continue
		}
		pattern := "%" + strings.ToLower(term) + "%"
		whereParts = append(whereParts, "(LOWER(title) LIKE ? OR LOWER(parent_title) LIKE ? OR LOWER(content) LIKE ?)")
		whereArgs = append(whereArgs, pattern, pattern, pattern)
		weight := len([]rune(term))
		if weight > 12 {
			weight = 12
		}
		scoreParts = append(scoreParts, fmt.Sprintf("(CASE WHEN LOWER(title) LIKE ? THEN %d ELSE 0 END + CASE WHEN LOWER(parent_title) LIKE ? THEN %d ELSE 0 END + CASE WHEN LOWER(content) LIKE ? THEN %d ELSE 0 END)", weight*3, weight*2, weight))
		scoreArgs = append(scoreArgs, pattern, pattern, pattern)
	}
	if len(whereParts) == 0 {
		return []model.KnowledgeChunk{}, nil
	}
	var chunks []model.KnowledgeChunk
	queryDB := base.WithContext(ctx).Where(strings.Join(whereParts, " OR "), whereArgs...)
	if len(scoreParts) > 0 {
		queryDB = queryDB.Order(gorm.Expr("("+strings.Join(scoreParts, " + ")+") DESC", scoreArgs...))
	}
	if err := queryDB.Order("updated_at DESC").Limit(limit).Find(&chunks).Error; err != nil {
		return nil, err
	}
	return chunks, nil
}

func rerankKnowledge(ctx context.Context, query string, ordered []knowledgeCandidate, limit int) ([]knowledgeCandidate, error) {
	documents := make([]string, 0, len(ordered))
	for _, item := range ordered {
		documents = append(documents, strings.TrimSpace(strings.Join([]string{
			item.chunk.Title,
			item.chunk.ParentTitle,
			item.chunk.Content,
		}, "\n")))
	}
	rerankResponse, err := llmutil.Rerank(ctx, query, documents, limit)
	if err != nil {
		return nil, err
	}
	if len(rerankResponse.Results) == 0 {
		return nil, fmt.Errorf("重排模型未返回候选结果")
	}
	ranked := make([]knowledgeCandidate, 0, len(rerankResponse.Results))
	seen := make(map[int]struct{}, len(rerankResponse.Results))
	for _, item := range rerankResponse.Results {
		if item.Index < 0 || item.Index >= len(ordered) {
			continue
		}
		if _, duplicate := seen[item.Index]; duplicate {
			continue
		}
		seen[item.Index] = struct{}{}
		candidate := ordered[item.Index]
		candidate.score = float64(item.Score)
		ranked = append(ranked, candidate)
	}
	if len(ranked) == 0 {
		return nil, fmt.Errorf("重排模型返回了无效候选索引")
	}
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ranked, nil
}

func knowledgeEvidenceViews(ctx context.Context, ordered []knowledgeCandidate) []response.KnowledgeEvidence {
	if len(ordered) == 0 {
		return []response.KnowledgeEvidence{}
	}
	documentIDs := make([]uint, 0, len(ordered))
	for _, item := range ordered {
		documentIDs = append(documentIDs, item.chunk.DocumentID)
	}
	var documents []model.KnowledgeDocument
	_ = global.GVA_DB.WithContext(ctx).Where("id IN ?", documentIDs).Find(&documents).Error
	names := make(map[uint]string, len(documents))
	for _, document := range documents {
		names[document.ID] = document.Name
	}
	items := make([]response.KnowledgeEvidence, 0, len(ordered))
	for _, item := range ordered {
		items = append(items, response.KnowledgeEvidence{DocumentID: item.chunk.DocumentID, DocumentName: names[item.chunk.DocumentID], ChunkID: item.chunk.ID, ChunkIndex: item.chunk.ChunkIndex, Title: item.chunk.Title, ParentTitle: item.chunk.ParentTitle, Content: item.chunk.Content, Score: item.score, VectorRank: item.vectorRank, LexicalRank: item.lexicalRank})
	}
	return items
}

func ensureKnowledgeMember(ctx context.Context, tenantID, organizationID, userID uint) error {
	if tenantID == 0 || organizationID == 0 || userID == 0 {
		return fmt.Errorf("缺少租户、组织或用户上下文")
	}
	var membership systemModel.SysMembership
	err := global.GVA_DB.WithContext(ctx).Where("tenant_id = ? AND organization_id = ? AND user_id = ? AND status = ?", tenantID, organizationID, userID, systemModel.UserStatusActive).First(&membership).Error
	if err != nil {
		return commonResponse.ErrForbidden
	}
	return nil
}

func documentView(document model.KnowledgeDocument) response.KnowledgeDocumentView {
	return response.KnowledgeDocumentView{ID: document.ID, OrganizationID: document.OrganizationID, Name: document.Name, OriginalName: document.OriginalName, ContentType: document.ContentType, ChunkCount: document.ChunkCount, Status: document.Status, FailureReason: document.FailureReason, CreatedAt: document.CreatedAt, IndexedAt: document.IndexedAt}
}
