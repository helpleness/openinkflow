package officialdoc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"path/filepath"
	"strings"

	"InkFlow/global"
	commonResponse "InkFlow/model/common/response"
	model "InkFlow/model/officialdoc"
	systemModel "InkFlow/model/system"
	systemService "InkFlow/service/system"
	"InkFlow/utils"
	"InkFlow/utils/chunker"
	"InkFlow/utils/documentparser"
	llmutil "InkFlow/utils/llm"
	"InkFlow/utils/storage"
	"InkFlow/utils/vectorstore"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

const maxKnowledgeDocumentSize = 200 << 20

const (
	knowledgeStageQueued          = "queued"
	knowledgeStageParsing         = "parsing"
	knowledgeStageAnalyzingImages = "analyzing_images"
	knowledgeStageChunking        = "chunking"
	knowledgeStageIndexing        = "indexing"
	knowledgeStageCompleted       = "completed"
	knowledgeStageFailed          = "failed"
)

// KnowledgeDocumentService imports source files and persists chunker output.
type KnowledgeDocumentService struct{}

// Import persists the source and returns immediately. Parsing and indexing run
// in the background so a large document never holds the browser upload dialog
// open. The durable document status is exposed through the document SSE stream.
func (s *KnowledgeDocumentService) Import(ctx context.Context, tenantID, organizationID, userID uint, file *multipart.FileHeader) (*model.KnowledgeDocument, error) {
	if tenantID == 0 || organizationID == 0 || userID == 0 {
		return nil, fmt.Errorf("缺少导入知识库所需的租户、组织或用户上下文")
	}
	db := global.GVA_DB
	var organization systemModel.SysOrganization
	if err := db.WithContext(ctx).Where("id = ? AND tenant_id = ?", organizationID, tenantID).First(&organization).Error; err != nil {
		return nil, commonResponse.ErrForbidden
	}
	var membership systemModel.SysMembership
	if err := db.WithContext(ctx).Where("tenant_id = ? AND organization_id = ? AND user_id = ? AND status = ?", tenantID, organizationID, userID, systemModel.UserStatusActive).First(&membership).Error; err != nil {
		return nil, commonResponse.ErrForbidden
	}
	if file == nil {
		return nil, fmt.Errorf("请选择要导入的文档")
	}
	originalName, err := storage.SanitizeFilename(file.Filename)
	if err != nil {
		return nil, fmt.Errorf("文件名无效: %w", err)
	}
	if !documentparser.IsSupportedFilename(originalName) {
		return nil, fmt.Errorf("不支持的文件类型；仅支持 .md、.markdown、.txt、.csv、.pdf、.docx、.xlsx、.pptx")
	}
	if file.Size > maxKnowledgeDocumentSize {
		return nil, fmt.Errorf("文档不能超过 200 MB")
	}
	stream, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	data, err := io.ReadAll(io.LimitReader(stream, maxKnowledgeDocumentSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxKnowledgeDocumentSize {
		return nil, fmt.Errorf("文档不能超过 200 MB")
	}
	digest, err := utils.FileSHA256(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("计算文档 SHA-256 失败: %w", err)
	}

	var existing model.KnowledgeDocument
	lookup := db.WithContext(ctx).Unscoped().
		Where("tenant_id = ? AND organization_id = ? AND sha256 = ?", tenantID, organizationID, digest).
		First(&existing)
	var deletedTombstone *model.KnowledgeDocument
	if lookup.Error == nil {
		if !existing.DeletedAt.Valid {
			return nil, fmt.Errorf("文档已存在：%s", existing.OriginalName)
		}
		deletedTombstone = &existing
	}
	if lookup.Error != nil && !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
		return nil, lookup.Error
	}

	objectStore, err := knowledgeObjectStorage()
	if err != nil {
		return nil, err
	}
	objectKey, err := storage.NewKnowledgeObjectKey(organizationID, originalName, knowledgeNow())
	if err != nil {
		return nil, fmt.Errorf("生成知识库对象路径失败: %w", err)
	}
	contentType := knowledgeContentType(originalName, file.Header.Get("Content-Type"))
	if err := objectStore.Upload(ctx, objectKey, bytes.NewReader(data), int64(len(data)), contentType); err != nil {
		return nil, fmt.Errorf("上传知识库原文件到 OSS 失败: %w", err)
	}

	// The uniqueness index also covers soft-deleted rows. Remove a known tombstone
	// only after the replacement source has been safely uploaded; source upload is
	// compensated below if the database operation cannot proceed.
	if deletedTombstone != nil {
		if err := purgeDeletedKnowledgeDocument(ctx, db, deletedTombstone.ID); err != nil {
			rollbackKnowledgeObjects(ctx, objectStore, nil, []string{objectKey}, "purge_deleted_document")
			return nil, fmt.Errorf("清理已删除的同内容知识文档失败: %w", err)
		}
	}

	document := model.KnowledgeDocument{
		TenantID:           tenantID,
		OrganizationID:     organizationID,
		CreatedBy:          userID,
		Name:               strings.TrimSuffix(originalName, filepath.Ext(originalName)),
		OriginalName:       originalName,
		ContentType:        contentType,
		ObjectKey:          objectKey,
		SHA256:             digest,
		Status:             "processing",
		ProcessingStage:    knowledgeStageQueued,
		ProcessingProgress: 2,
	}
	if err := db.WithContext(ctx).Create(&document).Error; err != nil {
		rollbackKnowledgeObjects(ctx, objectStore, &document, []string{objectKey}, "create_document")
		if isKnowledgeDocumentDuplicate(err) {
			return nil, fmt.Errorf("相同内容的文档正在导入或已存在，请刷新文档列表后重试")
		}
		return nil, err
	}
	s.startProcessing(document.ID, tenantID, userID)
	return &document, nil
}

// Reprocess replaces all derived content from the original private object. It
// is intentionally distinct from reindex: use it after a parser upgrade or
// when the source extraction is incomplete, even if the old document reached
// the ready state.
func (s *KnowledgeDocumentService) Reprocess(ctx context.Context, tenantID, documentID, userID uint) (*model.KnowledgeDocument, error) {
	db := global.GVA_DB
	var document model.KnowledgeDocument
	if err := db.WithContext(ctx).Where("id = ? AND tenant_id = ?", documentID, tenantID).First(&document).Error; err != nil {
		return nil, err
	}
	if err := ensureKnowledgeMember(ctx, tenantID, document.OrganizationID, userID); err != nil {
		return nil, err
	}
	if document.Status != "processing_failed" && document.Status != "ready" && document.Status != "index_failed" {
		return nil, fmt.Errorf("仅处理失败、可检索或索引失败的文档可以重新解析")
	}

	var chunks []model.KnowledgeChunk
	if err := db.WithContext(ctx).Where("document_id = ?", document.ID).Find(&chunks).Error; err != nil {
		return nil, fmt.Errorf("读取待替换的知识库切片失败: %w", err)
	}
	if global.GVA_VECTOR_STORE != nil && len(chunks) > 0 {
		keys := make([]vectorstore.StoreRequest, 0, len(chunks))
		for _, chunk := range chunks {
			keys = append(keys, vectorstore.StoreRequest{Collection: knowledgeChunkCollection, ID: chunk.ID})
		}
		if err := global.GVA_VECTOR_STORE.Delete(ctx, keys); err != nil {
			return nil, fmt.Errorf("清理旧知识库向量索引失败: %w", err)
		}
	}

	// Image object keys are deterministic for a source document. Keep the old
	// objects until the new parse has durably replaced their metadata: matching
	// images are overwritten during processing, and this avoids making a ready
	// document lose its image source if a later reset step fails.
	if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("document_id = ?", document.ID).Delete(&model.KnowledgeImage{}).Error; err != nil {
			return err
		}
		if err := tx.Where("document_id = ?", document.ID).Delete(&model.KnowledgeChunk{}).Error; err != nil {
			return err
		}
		return tx.Model(&document).Updates(map[string]any{
			"chunk_count":         0,
			"status":              "processing",
			"processing_stage":    knowledgeStageQueued,
			"processing_progress": 2,
			"failure_reason":      "",
			"indexed_at":          nil,
		}).Error
	}); err != nil {
		return nil, fmt.Errorf("重置失败的文档处理状态失败: %w", err)
	}
	document.ChunkCount = 0
	document.Status = "processing"
	document.ProcessingStage = knowledgeStageQueued
	document.ProcessingProgress = 2
	document.FailureReason = ""
	document.IndexedAt = nil
	s.startProcessing(document.ID, tenantID, userID)
	return &document, nil
}

func (s *KnowledgeDocumentService) startProcessing(documentID, tenantID, userID uint) {
	go func() {
		if err := s.processStoredDocument(context.Background(), documentID, tenantID, userID); err != nil && global.GVA_LOG != nil {
			global.GVA_LOG.Error("knowledge document background processing stopped", zap.Uint("document_id", documentID), zap.Error(err))
		}
	}()
}

func (s *KnowledgeDocumentService) processStoredDocument(ctx context.Context, documentID, tenantID, userID uint) error {
	db := global.GVA_DB
	var document model.KnowledgeDocument
	if err := db.WithContext(ctx).Where("id = ? AND tenant_id = ?", documentID, tenantID).First(&document).Error; err != nil {
		return err
	}
	objectStore, err := knowledgeObjectStorage()
	if err != nil {
		return processKnowledgeDocumentFailure(ctx, &document, err)
	}
	setKnowledgeDocumentProgress(ctx, &document, knowledgeStageParsing, 10)
	source, err := objectStore.Download(ctx, document.ObjectKey)
	if err != nil {
		return processKnowledgeDocumentFailure(ctx, &document, fmt.Errorf("读取已上传的原文件失败: %w", err))
	}
	data, readErr := io.ReadAll(io.LimitReader(source, maxKnowledgeDocumentSize+1))
	closeErr := source.Close()
	if readErr != nil {
		return processKnowledgeDocumentFailure(ctx, &document, fmt.Errorf("读取已上传的原文件失败: %w", readErr))
	}
	if closeErr != nil {
		return processKnowledgeDocumentFailure(ctx, &document, fmt.Errorf("关闭已上传的原文件失败: %w", closeErr))
	}
	if len(data) > maxKnowledgeDocumentSize {
		return processKnowledgeDocumentFailure(ctx, &document, fmt.Errorf("文档不能超过 200 MB"))
	}
	parsed, err := documentparser.New().Parse(ctx, document.OriginalName, bytes.NewReader(data))
	if err != nil {
		return processKnowledgeDocumentFailure(ctx, &document, err)
	}
	// A PDF without a text layer is processed through the configured visual
	// model. The parser supplies page images; no independent OCR runtime exists.
	scannedPDF := strings.TrimSpace(parsed.Text) == "" && strings.EqualFold(filepath.Ext(document.OriginalName), ".pdf")
	if strings.TrimSpace(parsed.Text) == "" && !scannedPDF {
		return processKnowledgeDocumentFailure(ctx, &document, fmt.Errorf("未从文档中提取到可切片的文本；OCR 未识别出正文"))
	}
	imageProgress := func(done, total int) {
		if total <= 0 {
			return
		}
		progress := 25 + done*40/total
		setKnowledgeDocumentProgress(ctx, &document, knowledgeStageAnalyzingImages, progress)
	}
	if len(parsed.Images) > 0 {
		setKnowledgeDocumentProgress(ctx, &document, knowledgeStageAnalyzingImages, 25)
	}
	images, imageChunks, uploadedImageKeys, err := prepareKnowledgeImages(ctx, objectStore, &document, tenantID, userID, parsed.Images, scannedPDF, imageProgress)
	if err != nil {
		rollbackKnowledgeObjects(ctx, objectStore, &document, uploadedImageKeys, "process_embedded_images")
		return processKnowledgeDocumentFailure(ctx, &document, err)
	}

	setKnowledgeDocumentProgress(ctx, &document, knowledgeStageChunking, 70)
	var chunks []model.KnowledgeChunk
	if scannedPDF {
		if len(imageChunks) == 0 {
			rollbackKnowledgeObjects(ctx, objectStore, &document, uploadedImageKeys, "scanned_pdf_no_page_image")
			return processKnowledgeDocumentFailure(ctx, &document, fmt.Errorf("扫描版 PDF 未包含可提取的页面图片，暂无法使用已配置的 OCR 图片语义模型识别"))
		}
		for index := range imageChunks {
			imageChunks[index].ChunkIndex = index
		}
		chunks = imageChunks
	} else {
		blocks, splitErr := chunker.NewLocalSplitter().Split(parsed.Text)
		if splitErr != nil {
			rollbackKnowledgeObjects(ctx, objectStore, &document, uploadedImageKeys, "split_document")
			return processKnowledgeDocumentFailure(ctx, &document, fmt.Errorf("知识库切片失败: %w", splitErr))
		}
		if len(blocks) == 0 {
			rollbackKnowledgeObjects(ctx, objectStore, &document, uploadedImageKeys, "empty_document_chunks")
			return processKnowledgeDocumentFailure(ctx, &document, fmt.Errorf("文档未生成知识库切片"))
		}
		chunks = knowledgeChunks(&document, tenantID, document.OrganizationID, blocks)
		for index := range imageChunks {
			imageChunks[index].ChunkIndex = len(chunks) + index
		}
		chunks = append(chunks, imageChunks...)
	}
	if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&chunks).Error; err != nil {
			return err
		}
		if len(images) > 0 {
			if err := tx.Create(&images).Error; err != nil {
				return err
			}
		}
		return tx.Model(&document).Updates(map[string]any{
			"chunk_count":         len(chunks),
			"status":              "indexing",
			"processing_stage":    knowledgeStageIndexing,
			"processing_progress": 82,
			"failure_reason":      "",
		}).Error
	}); err != nil {
		rollbackKnowledgeObjects(ctx, objectStore, &document, uploadedImageKeys, "persist_chunks")
		return processKnowledgeDocumentFailure(ctx, &document, fmt.Errorf("保存知识库切片失败: %w", err))
	}
	document.ChunkCount = len(chunks)
	document.Status = "indexing"
	document.ProcessingStage = knowledgeStageIndexing
	document.ProcessingProgress = 82
	document.FailureReason = ""
	setKnowledgeDocumentProgress(ctx, &document, knowledgeStageIndexing, 85)

	// Source data and chunks are durable now. IndexDocument records index_failed
	// itself when an embedding backend is unavailable, leaving a retryable state.
	indexed, indexErr := ServiceGroupApp.KnowledgeSearchService.IndexDocument(ctx, tenantID, document.ID, userID)
	if indexErr != nil {
		if indexed != nil && indexed.Status == "index_failed" {
			return nil
		}
		return processKnowledgeDocumentFailure(ctx, &document, fmt.Errorf("建立知识库索引失败: %w", indexErr))
	}
	return nil
}

func processKnowledgeDocumentFailure(ctx context.Context, document *model.KnowledgeDocument, cause error) error {
	_, markErr := markKnowledgeDocumentProcessingFailed(ctx, document, cause)
	if markErr != nil {
		return markErr
	}
	return cause
}

func setKnowledgeDocumentProgress(ctx context.Context, document *model.KnowledgeDocument, stage string, progress int) {
	if document == nil || global.GVA_DB == nil {
		return
	}
	if progress < 0 {
		progress = 0
	} else if progress > 100 {
		progress = 100
	}
	document.ProcessingStage = stage
	document.ProcessingProgress = progress
	if err := global.GVA_DB.WithContext(ctx).Model(document).Updates(map[string]any{"processing_stage": stage, "processing_progress": progress}).Error; err != nil && global.GVA_LOG != nil {
		global.GVA_LOG.Warn("update knowledge document processing progress failed", zap.Uint("document_id", document.ID), zap.Error(err))
	}
}

func knowledgeChunks(document *model.KnowledgeDocument, tenantID, organizationID uint, blocks []chunker.MarkdownBlock) []model.KnowledgeChunk {
	chunks := make([]model.KnowledgeChunk, 0, len(blocks))
	for index, block := range blocks {
		metadata, _ := json.Marshal(map[string]any{"path": block.Path, "heading_path": block.HeadingPath, "section_type": block.SectionType, "token_estimate": block.TokenEstimate})
		chunks = append(chunks, model.KnowledgeChunk{DocumentID: document.ID, TenantID: tenantID, OrganizationID: organizationID, ChunkIndex: index, Title: block.Title, ParentTitle: block.ParentTitle, Content: block.Content, Metadata: string(metadata)})
	}
	return chunks
}

func prepareKnowledgeImages(ctx context.Context, objectStore storage.ObjectStorage, document *model.KnowledgeDocument, tenantID, userID uint, parsedImages []documentparser.Image, forceVisualOCR bool, onProgress func(done, total int)) ([]model.KnowledgeImage, []model.KnowledgeChunk, []string, error) {
	var analyzer *llmutil.ImageSemanticAnalyzer
	if len(parsedImages) > 0 {
		semanticLLM, err := systemService.ServiceGroupApp.SysModelSettingService.ResolveOCRSemanticLLM(ctx, tenantID, userID)
		if err != nil {
			if forceVisualOCR {
				return nil, nil, nil, fmt.Errorf("读取 OCR 图片语义总结模型配置失败: %w", err)
			}
			if global.GVA_LOG != nil {
				global.GVA_LOG.Warn("读取图片语义模型配置失败，跳过非扫描文档的图片语义增强", zap.Uint("document_id", document.ID), zap.Error(err))
			}
		} else {
			analyzer = llmutil.NewImageSemanticAnalyzer(semanticLLM)
		}
	}
	if forceVisualOCR && analyzer == nil {
		return nil, nil, nil, fmt.Errorf("扫描版 PDF 需要在模型配置中设置主模型或 OCR 图片语义总结模型")
	}
	seenImages := make(map[string]struct{}, len(parsedImages))
	images := make([]model.KnowledgeImage, 0, len(parsedImages))
	imageChunks := make([]model.KnowledgeChunk, 0, len(parsedImages))
	uploadedKeys := make([]string, 0, len(parsedImages))
	for imageIndex, image := range parsedImages {
		imageDigest, hashErr := utils.FileSHA256(bytes.NewReader(image.Data))
		if hashErr != nil {
			return nil, nil, uploadedKeys, fmt.Errorf("计算图片 %s SHA-256 失败: %w", image.Name, hashErr)
		}
		if _, duplicate := seenImages[imageDigest]; duplicate {
			if onProgress != nil {
				onProgress(imageIndex+1, len(parsedImages))
			}
			continue
		}
		seenImages[imageDigest] = struct{}{}
		imageName, nameErr := storage.SanitizeFilename(image.Name)
		if nameErr != nil {
			return nil, nil, uploadedKeys, fmt.Errorf("内嵌图片文件名无效: %w", nameErr)
		}
		imageKey, keyErr := storage.NewKnowledgeImageObjectKey(document.OrganizationID, document.ObjectKey, imageName)
		if keyErr != nil {
			return nil, nil, uploadedKeys, fmt.Errorf("生成内嵌图片对象路径失败: %w", keyErr)
		}
		if err := objectStore.Upload(ctx, imageKey, bytes.NewReader(image.Data), int64(len(image.Data)), image.MIME); err != nil {
			return nil, nil, uploadedKeys, fmt.Errorf("上传内嵌图片到 OSS 失败: %w", err)
		}
		uploadedKeys = append(uploadedKeys, imageKey)

		imageKind := "image"
		parseableDocument := forceVisualOCR
		if forceVisualOCR {
			imageKind = "scanned_page"
		}
		imageKnowledge := ""
		if !forceVisualOCR && global.GVA_OCR != nil {
			detector := global.GVA_OCR
			layout, detectErr := detector.DetectBytes(ctx, image.Data)
			if detectErr != nil {
				if global.GVA_LOG != nil {
					global.GVA_LOG.Warn("本地图片版面识别失败，跳过该图片的语义增强", zap.Uint("document_id", document.ID), zap.String("image_name", imageName), zap.Error(detectErr))
				}
			} else {
				parseableDocument = layout.HasText || layout.HasTable
				if layout.HasTable {
					imageKind = "table"
				} else if parseableDocument {
					imageKind = "document"
				}
			}
		}
		if analyzer != nil && parseableDocument {
			knowledge, analyzeErr := analyzer.AnalyzeImage(ctx, image.MIME, image.Data)
			if analyzeErr != nil {
				if global.GVA_LOG != nil {
					global.GVA_LOG.Warn("图片视觉解析失败，跳过该图片", zap.Uint("document_id", document.ID), zap.String("image_name", imageName), zap.Error(analyzeErr))
				}
			} else {
				imageKnowledge = knowledge
			}
		}
		// One model response may be a literal transcription, a chart summary, or
		// both. Keep it in Semantic as the legacy free-form knowledge field rather
		// than incorrectly labelling a chart summary as extracted source text.
		images = append(images, model.KnowledgeImage{DocumentID: document.ID, Name: imageName, MIME: image.MIME, ObjectKey: imageKey, SHA256: imageDigest, Kind: imageKind, Semantic: imageKnowledge})
		imageContent := imageKnowledge
		if imageContent == "" {
			if onProgress != nil {
				onProgress(imageIndex+1, len(parsedImages))
			}
			continue
		}
		sectionType := "image_semantic"
		if forceVisualOCR {
			sectionType = "scanned_pdf_ocr"
		}
		imageChunks = append(imageChunks, model.KnowledgeChunk{
			DocumentID: document.ID, TenantID: tenantID, OrganizationID: document.OrganizationID,
			Title: "图片语义：" + imageName, ParentTitle: document.Name, Content: imageContent,
			Metadata: fmt.Sprintf(`{"section_type":%q,"image_name":%q,"image_sha256":%q}`, sectionType, imageName, imageDigest),
		})
		if onProgress != nil {
			onProgress(imageIndex+1, len(parsedImages))
		}
	}
	return images, imageChunks, uploadedKeys, nil
}

func knowledgeContentType(filename, supplied string) string {
	if contentType := strings.TrimSpace(strings.Split(supplied, ";")[0]); contentType != "" {
		return contentType
	}
	if contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename))); contentType != "" {
		return contentType
	}
	return "application/octet-stream"
}

// purgeDeletedKnowledgeDocument physically clears a soft-deleted duplicate so
// the scoped SHA-256 uniqueness index allows a replacement import.
func purgeDeletedKnowledgeDocument(ctx context.Context, db *gorm.DB, documentID uint) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("document_id = ?", documentID).Delete(&model.KnowledgeImage{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("document_id = ?", documentID).Delete(&model.KnowledgeChunk{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Delete(&model.KnowledgeDocument{}, documentID).Error
	})
}

func isKnowledgeDocumentDuplicate(err error) bool {
	return err != nil && strings.Contains(err.Error(), "idx_knowledge_documents_scope_sha256")
}
