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

	"gorm.io/gorm"
)

const maxKnowledgeDocumentSize = 200 << 20

// KnowledgeDocumentService imports source files and persists chunker output.
type KnowledgeDocumentService struct{}

// Import keeps the original source in private object storage before parsing it.
// A parsing failure becomes a visible processing_failed document: administrators
// can inspect its cause and reprocess the preserved source rather than asking a
// user to upload a potentially unavailable original file again.
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
		TenantID:       tenantID,
		OrganizationID: organizationID,
		CreatedBy:      userID,
		Name:           strings.TrimSuffix(originalName, filepath.Ext(originalName)),
		OriginalName:   originalName,
		ContentType:    contentType,
		ObjectKey:      objectKey,
		SHA256:         digest,
		Status:         "processing",
	}
	if err := db.WithContext(ctx).Create(&document).Error; err != nil {
		rollbackKnowledgeObjects(ctx, objectStore, &document, []string{objectKey}, "create_document")
		if isKnowledgeDocumentDuplicate(err) {
			return nil, fmt.Errorf("相同内容的文档正在导入或已存在，请刷新文档列表后重试")
		}
		return nil, err
	}

	parsed, err := documentparser.New().Parse(ctx, originalName, bytes.NewReader(data))
	if err != nil {
		return markKnowledgeDocumentProcessingFailed(ctx, &document, err)
	}
	// A PDF without a text layer is processed through the same visual model
	// configured on the model-settings page. The parser supplies embedded JPEG
	// page images; no separate OCR binary, model or deployment setting is used.
	scannedPDF := strings.TrimSpace(parsed.Text) == "" && strings.EqualFold(filepath.Ext(originalName), ".pdf")
	if strings.TrimSpace(parsed.Text) == "" && !scannedPDF {
		return markKnowledgeDocumentProcessingFailed(ctx, &document, fmt.Errorf("未从文档中提取到可切片的文本；OCR 未识别出正文"))
	}

	images, imageChunks, uploadedImageKeys, err := prepareKnowledgeImages(ctx, objectStore, &document, tenantID, userID, parsed.Images, scannedPDF)
	if err != nil {
		rollbackKnowledgeObjects(ctx, objectStore, &document, uploadedImageKeys, "process_embedded_images")
		return markKnowledgeDocumentProcessingFailed(ctx, &document, err)
	}

	var chunks []model.KnowledgeChunk
	if scannedPDF {
		if len(imageChunks) == 0 {
			rollbackKnowledgeObjects(ctx, objectStore, &document, uploadedImageKeys, "scanned_pdf_no_page_image")
			return markKnowledgeDocumentProcessingFailed(ctx, &document, fmt.Errorf("扫描版 PDF 未包含可提取的页面图片，暂无法使用已配置的 OCR 图片语义模型识别"))
		}
		for index := range imageChunks {
			imageChunks[index].ChunkIndex = index
		}
		chunks = imageChunks
	} else {
		blocks, splitErr := chunker.NewLocalSplitter().Split(parsed.Text)
		if splitErr != nil {
			rollbackKnowledgeObjects(ctx, objectStore, &document, uploadedImageKeys, "split_document")
			return markKnowledgeDocumentProcessingFailed(ctx, &document, fmt.Errorf("知识库切片失败: %w", splitErr))
		}
		if len(blocks) == 0 {
			rollbackKnowledgeObjects(ctx, objectStore, &document, uploadedImageKeys, "empty_document_chunks")
			return markKnowledgeDocumentProcessingFailed(ctx, &document, fmt.Errorf("文档未生成知识库切片"))
		}
		chunks = knowledgeChunks(&document, tenantID, organizationID, blocks)
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
		return tx.Model(&document).Updates(map[string]any{"chunk_count": len(chunks), "status": "indexing", "failure_reason": ""}).Error
	}); err != nil {
		rollbackKnowledgeObjects(ctx, objectStore, &document, uploadedImageKeys, "persist_chunks")
		return markKnowledgeDocumentProcessingFailed(ctx, &document, fmt.Errorf("保存知识库切片失败: %w", err))
	}
	document.ChunkCount = len(chunks)
	document.Status = "indexing"
	document.FailureReason = ""

	// Source data and chunks are durable now. An unavailable embedding backend is
	// retryable, so IndexDocument writes index_failed and Import still succeeds.
	indexed, indexErr := ServiceGroupApp.KnowledgeSearchService.IndexDocument(ctx, tenantID, document.ID, userID)
	if indexErr != nil && indexed != nil {
		return indexed, nil
	}
	if indexErr != nil {
		return &document, indexErr
	}
	return indexed, nil
}

func knowledgeChunks(document *model.KnowledgeDocument, tenantID, organizationID uint, blocks []chunker.MarkdownBlock) []model.KnowledgeChunk {
	chunks := make([]model.KnowledgeChunk, 0, len(blocks))
	for index, block := range blocks {
		metadata, _ := json.Marshal(map[string]any{"path": block.Path, "heading_path": block.HeadingPath, "section_type": block.SectionType, "token_estimate": block.TokenEstimate})
		chunks = append(chunks, model.KnowledgeChunk{DocumentID: document.ID, TenantID: tenantID, OrganizationID: organizationID, ChunkIndex: index, Title: block.Title, ParentTitle: block.ParentTitle, Content: block.Content, Metadata: string(metadata)})
	}
	return chunks
}

func prepareKnowledgeImages(ctx context.Context, objectStore storage.ObjectStorage, document *model.KnowledgeDocument, tenantID, userID uint, parsedImages []documentparser.Image, forceVisualOCR bool) ([]model.KnowledgeImage, []model.KnowledgeChunk, []string, error) {
	semanticLLM, err := systemService.ServiceGroupApp.SysModelSettingService.ResolveOCRSemanticLLM(ctx, tenantID, userID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("读取 OCR 图片语义总结模型配置失败: %w", err)
	}
	analyzer := llmutil.NewImageSemanticAnalyzer(semanticLLM)
	if forceVisualOCR && analyzer == nil {
		return nil, nil, nil, fmt.Errorf("扫描版 PDF 需要在模型配置中设置主模型或 OCR 图片语义总结模型")
	}
	seenImages := make(map[string]struct{}, len(parsedImages))
	images := make([]model.KnowledgeImage, 0, len(parsedImages))
	imageChunks := make([]model.KnowledgeChunk, 0, len(parsedImages))
	uploadedKeys := make([]string, 0, len(parsedImages))
	for _, image := range parsedImages {
		imageDigest, hashErr := utils.FileSHA256(bytes.NewReader(image.Data))
		if hashErr != nil {
			return nil, nil, uploadedKeys, fmt.Errorf("计算图片 %s SHA-256 失败: %w", image.Name, hashErr)
		}
		if _, duplicate := seenImages[imageDigest]; duplicate {
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
		extractedText := ""
		semanticSummary := ""
		if !forceVisualOCR && global.GVA_OCR != nil {
			detector := global.GVA_OCR
			layout, detectErr := detector.DetectBytes(ctx, image.Data)
			if detectErr != nil {
				return nil, nil, uploadedKeys, fmt.Errorf("图片 %s 本地 ONNX 版面识别失败: %w", imageName, detectErr)
			}
			parseableDocument = layout.HasText || layout.HasTable
			if layout.HasTable {
				imageKind = "table"
			} else if parseableDocument {
				imageKind = "document"
			}
		}
		if analyzer != nil && parseableDocument {
			semantic, analyzeErr := analyzer.AnalyzeImage(ctx, image.MIME, image.Data)
			if analyzeErr != nil {
				return nil, nil, uploadedKeys, fmt.Errorf("图片 %s 视觉解析失败: %w", imageName, analyzeErr)
			}
			extractedText = semantic.Text
			semanticSummary = semantic.Semantic
		}
		images = append(images, model.KnowledgeImage{DocumentID: document.ID, Name: imageName, MIME: image.MIME, ObjectKey: imageKey, SHA256: imageDigest, Kind: imageKind, ExtractedText: extractedText, Semantic: semanticSummary})
		imageContent := strings.TrimSpace(strings.Join([]string{extractedText, semanticSummary}, "\n"))
		if imageContent == "" {
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
