package officialdoc

import (
	"strconv"

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
	download, err := service.ServiceGroupApp.KnowledgeSearchService.DownloadDocument(c.Request.Context(), ginctx.CurrentTenantID(c), uint(documentID), ginctx.CurrentUserID(c))
	commonResponse.Respond(download, err, commonResponse.ErrForbidden, c)
}

func (api *KnowledgeSearchApi) ReindexDocument(c *gin.Context) {
	documentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || documentID == 0 {
		commonResponse.BadRequest("无效的文档 ID", c)
		return
	}
	document, err := service.ServiceGroupApp.KnowledgeSearchService.IndexDocument(c.Request.Context(), ginctx.CurrentTenantID(c), uint(documentID), ginctx.CurrentUserID(c))
	if document != nil {
		commonResponse.OkWithDetailed(response.KnowledgeDocumentView{ID: document.ID, OrganizationID: document.OrganizationID, Name: document.Name, OriginalName: document.OriginalName, ContentType: document.ContentType, ChunkCount: document.ChunkCount, Status: document.Status, FailureReason: document.FailureReason, CreatedAt: document.CreatedAt, IndexedAt: document.IndexedAt}, "索引已完成或已记录失败原因", c)
		return
	}
	commonResponse.Respond(nil, err, commonResponse.ErrForbidden, c)
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
