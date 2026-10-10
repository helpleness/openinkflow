package officialdoc

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	commonResponse "InkFlow/model/common/response"
	request "InkFlow/model/officialdoc/request"
	response "InkFlow/model/officialdoc/response"
	service "InkFlow/service/officialdoc"
	"InkFlow/utils/ginctx"

	"github.com/gin-gonic/gin"
)

// KnowledgeSearchApi exposes source management, re-indexing and hybrid retrieval.
type KnowledgeSearchApi struct{}

func (api *KnowledgeSearchApi) ListDocuments(c *gin.Context) {
	var req request.KnowledgeDocumentList
	if err := c.ShouldBindQuery(&req); err != nil {
		commonResponse.BadRequest("缺少有效的 organization_id", c)
		return
	}
	items, err := service.ServiceGroupApp.KnowledgeSearchService.ListDocuments(c.Request.Context(), ginctx.CurrentTenantID(c), req.OrganizationID, ginctx.CurrentUserID(c))
	commonResponse.Respond(items, err, commonResponse.ErrForbidden, c)
}

func (api *KnowledgeSearchApi) GetDocument(c *gin.Context) {
	documentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || documentID == 0 {
		commonResponse.BadRequest("无效的文档 ID", c)
		return
	}
	document, chunks, err := service.ServiceGroupApp.KnowledgeSearchService.GetDocument(c.Request.Context(), ginctx.CurrentTenantID(c), uint(documentID), ginctx.CurrentUserID(c))
	if err != nil {
		commonResponse.Respond(nil, err, commonResponse.ErrForbidden, c)
		return
	}
	items := make([]response.KnowledgeChunkView, 0, len(chunks))
	for _, chunk := range chunks {
		items = append(items, response.KnowledgeChunkView{ID: chunk.ID, ChunkIndex: chunk.ChunkIndex, Title: chunk.Title, ParentTitle: chunk.ParentTitle, Content: chunk.Content, Metadata: chunk.Metadata})
	}
	commonResponse.OkWithData(response.KnowledgeDocumentDetail{Document: document, Chunks: items}, c)
}

func (api *KnowledgeSearchApi) DownloadDocument(c *gin.Context) {
	documentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || documentID == 0 {
		commonResponse.BadRequest("无效的文档 ID", c)
		return
	}
	if c.Query("content") == "1" {
		api.streamDocumentContent(c, uint(documentID))
		return
	}
	download, err := service.ServiceGroupApp.KnowledgeSearchService.DownloadDocument(c.Request.Context(), ginctx.CurrentTenantID(c), uint(documentID), ginctx.CurrentUserID(c))
	commonResponse.Respond(download, err, commonResponse.ErrForbidden, c)
}

func (api *KnowledgeSearchApi) streamDocumentContent(c *gin.Context, documentID uint) {
	reader, _, contentType, err := service.ServiceGroupApp.KnowledgeSearchService.OpenDocumentSource(c.Request.Context(), ginctx.CurrentTenantID(c), documentID, ginctx.CurrentUserID(c))
	if err != nil {
		commonResponse.Respond(nil, err, commonResponse.ErrForbidden, c)
		return
	}
	defer reader.Close()
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", "attachment")
	c.Header("Content-Type", contentType)
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, reader)
}

func (api *KnowledgeSearchApi) ReindexDocument(c *gin.Context) {
	documentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || documentID == 0 {
		commonResponse.BadRequest("无效的文档 ID", c)
		return
	}
	document, err := service.ServiceGroupApp.KnowledgeSearchService.IndexDocument(c.Request.Context(), ginctx.CurrentTenantID(c), uint(documentID), ginctx.CurrentUserID(c))
	if document != nil {
		commonResponse.OkWithDetailed(response.KnowledgeDocumentView{ID: document.ID, OrganizationID: document.OrganizationID, Name: document.Name, OriginalName: document.OriginalName, ContentType: document.ContentType, ChunkCount: document.ChunkCount, Status: document.Status, ProcessingStage: document.ProcessingStage, ProcessingProgress: document.ProcessingProgress, FailureReason: document.FailureReason, CreatedAt: document.CreatedAt, IndexedAt: document.IndexedAt}, "索引已完成或已记录失败原因", c)
		return
	}
	commonResponse.Respond(nil, err, commonResponse.ErrForbidden, c)
}

func (api *KnowledgeSearchApi) ReprocessDocument(c *gin.Context) {
	documentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || documentID == 0 {
		commonResponse.BadRequest("无效的文档 ID", c)
		return
	}
	document, err := service.ServiceGroupApp.KnowledgeDocumentService.Reprocess(c.Request.Context(), ginctx.CurrentTenantID(c), uint(documentID), ginctx.CurrentUserID(c))
	if err != nil {
		commonResponse.Respond(nil, err, commonResponse.ErrForbidden, c)
		return
	}
	commonResponse.OkWithDetailed(response.KnowledgeDocumentView{
		ID: document.ID, OrganizationID: document.OrganizationID, Name: document.Name, OriginalName: document.OriginalName,
		ContentType: document.ContentType, ChunkCount: document.ChunkCount, Status: document.Status,
		ProcessingStage: document.ProcessingStage, ProcessingProgress: document.ProcessingProgress,
		FailureReason: document.FailureReason, CreatedAt: document.CreatedAt, IndexedAt: document.IndexedAt,
	}, "文档已重新排队解析", c)
}

// Events streams persisted lifecycle changes for one document. It is safe to
// reconnect because every payload is read from the database, not process memory.
func (api *KnowledgeSearchApi) Events(c *gin.Context) {
	documentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || documentID == 0 {
		commonResponse.BadRequest("无效的文档 ID", c)
		return
	}
	tenantID := ginctx.CurrentTenantID(c)
	userID := ginctx.CurrentUserID(c)
	document, err := service.ServiceGroupApp.KnowledgeSearchService.GetDocumentView(c.Request.Context(), tenantID, uint(documentID), userID)
	if err != nil {
		commonResponse.Respond(nil, err, commonResponse.ErrForbidden, c)
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	lastPayload := ""
	lastKeepAlive := time.Now()
	send := func(event string, payload any) {
		c.SSEvent(event, payload)
		c.Writer.Flush()
	}
	sendDocument := func(item response.KnowledgeDocumentView) {
		encoded, marshalErr := json.Marshal(item)
		if marshalErr != nil || string(encoded) == lastPayload {
			return
		}
		lastPayload = string(encoded)
		send("document", item)
	}
	completed := func(status string) bool {
		return status == "ready" || status == "processing_failed" || status == "index_failed" || status == "delete_failed"
	}
	sendDocument(document)
	if completed(document.Status) {
		send("done", document)
		return
	}

	ticker := time.NewTicker(700 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
			document, err = service.ServiceGroupApp.KnowledgeSearchService.GetDocumentView(c.Request.Context(), tenantID, uint(documentID), userID)
			if err != nil {
				send("error", gin.H{"message": err.Error()})
				return
			}
			sendDocument(document)
			if completed(document.Status) {
				send("done", document)
				return
			}
			if time.Since(lastKeepAlive) >= 15*time.Second {
				send("ping", gin.H{"at": time.Now().UTC().Format(time.RFC3339)})
				lastKeepAlive = time.Now()
			}
		}
	}
}

func (api *KnowledgeSearchApi) DeleteDocument(c *gin.Context) {
	documentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || documentID == 0 {
		commonResponse.BadRequest("无效的文档 ID", c)
		return
	}
	err = service.ServiceGroupApp.KnowledgeSearchService.DeleteDocument(c.Request.Context(), ginctx.CurrentTenantID(c), uint(documentID), ginctx.CurrentUserID(c))
	commonResponse.Respond(gin.H{}, err, commonResponse.ErrForbidden, c)
}

func (api *KnowledgeSearchApi) Search(c *gin.Context) {
	var req request.KnowledgeDocumentSearch
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("请输入组织和检索词", c)
		return
	}
	result, err := service.ServiceGroupApp.KnowledgeSearchService.Search(c.Request.Context(), ginctx.CurrentTenantID(c), req.OrganizationID, ginctx.CurrentUserID(c), req.Query, req.Limit)
	commonResponse.Respond(result, err, commonResponse.ErrForbidden, c)
}
