package officialdoc

import (
	"strconv"

	commonResponse "InkFlow/model/common/response"
	request "InkFlow/model/officialdoc/request"
	service "InkFlow/service/officialdoc"
	"InkFlow/utils/ginctx"

	"github.com/gin-gonic/gin"
)

type DocumentTemplateApi struct{}

func (api *DocumentTemplateApi) List(c *gin.Context) {
	var req request.DocumentTemplateList
	if err := c.ShouldBindQuery(&req); err != nil {
		commonResponse.BadRequest("缺少有效的 organization_id", c)
		return
	}
	items, err := service.ServiceGroupApp.DocumentTemplateService.List(c.Request.Context(), ginctx.CurrentTenantID(c), req.OrganizationID, ginctx.CurrentUserID(c))
	commonResponse.Respond(items, err, commonResponse.ErrForbidden, c)
}

func (api *DocumentTemplateApi) Create(c *gin.Context) {
	var req request.DocumentTemplateCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("模板参数不完整", c)
		return
	}
	item, err := service.ServiceGroupApp.DocumentTemplateService.Create(c.Request.Context(), ginctx.CurrentTenantID(c), ginctx.CurrentUserID(c), req)
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}

func (api *DocumentTemplateApi) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		commonResponse.BadRequest("无效的模板 ID", c)
		return
	}
	var req request.DocumentTemplateUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("模板参数不完整", c)
		return
	}
	item, err := service.ServiceGroupApp.DocumentTemplateService.Update(c.Request.Context(), ginctx.CurrentTenantID(c), uint(id), ginctx.CurrentUserID(c), req)
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}
