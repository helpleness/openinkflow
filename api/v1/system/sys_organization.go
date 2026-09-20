package system

import (
	"strconv"

	commonResponse "InkFlow/model/common/response"
	request "InkFlow/model/system/request"
	systemService "InkFlow/service/system"
	"InkFlow/utils/ginctx"

	"github.com/gin-gonic/gin"
)

// SysOrganizationApi handles organization discovery and management requests.
type SysOrganizationApi struct{}

// ListOrganizations 返回当前用户在租户内可管理的组织列表。
func (api *SysOrganizationApi) ListOrganizations(c *gin.Context) {
	items, err := systemService.ServiceGroupApp.SysOrganizationService.ListOrganizations(c.Request.Context(), ginctx.CurrentTenantID(c), ginctx.CurrentUserID(c))
	commonResponse.Respond(items, err, commonResponse.ErrForbidden, c)
}

// ListPublicOrganizations 返回当前租户允许申请加入的公开组织。
func (api *SysOrganizationApi) ListPublicOrganizations(c *gin.Context) {
	items, err := systemService.ServiceGroupApp.SysOrganizationService.ListPublicOrganizations(c.Request.Context(), ginctx.CurrentTenantID(c))
	commonResponse.Respond(items, err, commonResponse.ErrForbidden, c)
}

// SetOrganizationVisibility 更新组织是否对成员开放申请。
func (api *SysOrganizationApi) SetOrganizationVisibility(c *gin.Context) {
	var req request.SysOrganizationVisibilityUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("请求参数无效", c)
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		commonResponse.BadRequest("组织编号无效", c)
		return
	}
	item, err := systemService.ServiceGroupApp.SysOrganizationService.SetOrganizationVisibility(c.Request.Context(), ginctx.CurrentTenantID(c), uint(id), req.IsVisible)
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}

// CreateOrganization 在当前租户内创建组织。
func (api *SysOrganizationApi) CreateOrganization(c *gin.Context) {
	var req request.SysOrganizationCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("请求参数无效", c)
		return
	}

	item, err := systemService.ServiceGroupApp.SysOrganizationService.CreateOrganization(c.Request.Context(), ginctx.CurrentTenantID(c), req.ParentID, req.Name, req.Code)
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}
