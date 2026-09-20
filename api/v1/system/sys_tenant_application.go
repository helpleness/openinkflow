package system

import (
	"strconv"

	commonResponse "InkFlow/model/common/response"
	request "InkFlow/model/system/request"
	systemService "InkFlow/service/system"
	"InkFlow/utils/ginctx"

	"github.com/gin-gonic/gin"
)

// SysTenantApplicationApi handles global workspace-application endpoints.
type SysTenantApplicationApi struct{}

func (api *SysTenantApplicationApi) ListPublicTenants(c *gin.Context) {
	items, err := systemService.ServiceGroupApp.SysTenantApplicationService.ListPublicTenants(c.Request.Context(), ginctx.CurrentUserID(c))
	commonResponse.Respond(items, err, commonResponse.ErrForbidden, c)
}

func (api *SysTenantApplicationApi) ListOwnApplications(c *gin.Context) {
	items, err := systemService.ServiceGroupApp.SysTenantApplicationService.ListOwnApplications(c.Request.Context(), ginctx.CurrentUserID(c))
	commonResponse.Respond(items, err, commonResponse.ErrForbidden, c)
}

func (api *SysTenantApplicationApi) ApplyToTenant(c *gin.Context) {
	var req request.SysTenantApplicationCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("请选择工作空间", c)
		return
	}
	item, err := systemService.ServiceGroupApp.SysTenantApplicationService.ApplyToTenant(c.Request.Context(), req.TenantID, ginctx.CurrentUserID(c))
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}

func (api *SysTenantApplicationApi) ListTenantApplications(c *gin.Context) {
	items, err := systemService.ServiceGroupApp.SysTenantApplicationService.ListTenantApplications(c.Request.Context(), ginctx.CurrentTenantID(c), ginctx.CurrentUserID(c))
	commonResponse.Respond(items, err, commonResponse.ErrForbidden, c)
}

func (api *SysTenantApplicationApi) ReviewTenantApplication(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		commonResponse.BadRequest("申请编号无效", c)
		return
	}
	var req request.SysTenantApplicationReview
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("请求参数无效", c)
		return
	}
	err = systemService.ServiceGroupApp.SysTenantApplicationService.ReviewTenantApplication(c.Request.Context(), ginctx.CurrentTenantID(c), uint(id), ginctx.CurrentUserID(c), req.Approve)
	commonResponse.Respond(gin.H{}, err, commonResponse.ErrForbidden, c)
}
