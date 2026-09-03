package system

import (
	commonResponse "InkFlow/model/common/response"
	request "InkFlow/model/system/request"
	systemService "InkFlow/service/system"
	"InkFlow/utils/ginctx"

	"github.com/gin-gonic/gin"
)

// SysTenantApi handles tenant-directory HTTP requests.
type SysTenantApi struct{}

// ListTenants 返回当前用户可访问的租户。
func (api *SysTenantApi) ListTenants(c *gin.Context) {
	items, err := systemService.ServiceGroupApp.SysTenantService.ListTenants(c.Request.Context(), ginctx.CurrentUserID(c))
	commonResponse.Respond(items, err, commonResponse.ErrForbidden, c)
}

// CreateTenant 为当前用户创建租户及其初始化资源。
func (api *SysTenantApi) CreateTenant(c *gin.Context) {
	var req request.SysTenantCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("请求参数无效", c)
		return
	}

	item, err := systemService.ServiceGroupApp.SysTenantService.CreateTenant(c.Request.Context(), ginctx.CurrentUserID(c), req.Name, req.Code)
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}
