package officialdoc

import (
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"

	commonResponse "InkFlow/model/common/response"
	request "InkFlow/model/officialdoc/request"
	service "InkFlow/service/officialdoc"
	"InkFlow/utils/documentexport"
	"InkFlow/utils/ginctx"

	"github.com/gin-gonic/gin"
)

type WritingTaskApi struct{}

func (api *WritingTaskApi) List(c *gin.Context) {
	var req request.WritingTaskList
	if err := c.ShouldBindQuery(&req); err != nil {
		commonResponse.BadRequest("缺少有效的 organization_id", c)
		return
	}
	items, err := service.ServiceGroupApp.WritingTaskService.List(c.Request.Context(), ginctx.CurrentTenantID(c), req.OrganizationID, ginctx.CurrentUserID(c))
	commonResponse.Respond(items, err, commonResponse.ErrForbidden, c)
}

func (api *WritingTaskApi) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		commonResponse.BadRequest("无效的写作任务 ID", c)
		return
	}
	item, err := service.ServiceGroupApp.WritingTaskService.Get(c.Request.Context(), ginctx.CurrentTenantID(c), uint(id), ginctx.CurrentUserID(c))
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}

func (api *WritingTaskApi) Create(c *gin.Context) {
	var req request.WritingTaskCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("写作任务参数不完整", c)
		return
	}
	item, err := service.ServiceGroupApp.WritingTaskService.Create(c.Request.Context(), ginctx.CurrentTenantID(c), ginctx.CurrentUserID(c), req)
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}

func (api *WritingTaskApi) SaveVersion(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		commonResponse.BadRequest("无效的写作任务 ID", c)
		return
	}
	var req request.DocumentVersionCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("版本正文不能为空", c)
		return
	}
	item, err := service.ServiceGroupApp.WritingTaskService.SaveVersion(c.Request.Context(), ginctx.CurrentTenantID(c), uint(id), ginctx.CurrentUserID(c), req)
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}

// ExportVersion streams Markdown, DOCX or PDF for an immutable version. It
// never accepts raw editor content, so downloaded files always map to history.
func (api *WritingTaskApi) ExportVersion(c *gin.Context) {
	taskID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || taskID == 0 {
		commonResponse.BadRequest("无效的写作任务 ID", c)
		return
	}
	versionID, err := strconv.ParseUint(c.Param("version_id"), 10, 64)
	if err != nil || versionID == 0 {
		commonResponse.BadRequest("无效的版本 ID", c)
		return
	}
	data, filename, contentType, exportErr := service.ServiceGroupApp.WritingTaskService.ExportVersion(c.Request.Context(), ginctx.CurrentTenantID(c), uint(taskID), uint(versionID), ginctx.CurrentUserID(c), c.Query("format"))
	if exportErr != nil {
		if errors.Is(exportErr, documentexport.ErrPDFExportBusy) {
			commonResponse.ResultWithStatus(http.StatusTooManyRequests, http.StatusTooManyRequests, nil, exportErr.Error(), c)
			return
		}
		commonResponse.Respond(nil, exportErr, commonResponse.ErrForbidden, c)
		return
	}
	// Keep an ASCII fallback with the correct extension for clients that do not
	// understand filename*. The UTF-8 name preserves the document title.
	c.Header("Content-Disposition", `attachment; filename="inkflow-document`+filepath.Ext(filename)+`"; filename*=UTF-8''`+url.PathEscape(filename))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(200, contentType, data)
}
