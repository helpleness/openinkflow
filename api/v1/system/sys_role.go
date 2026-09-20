package system

import (
	"strconv"

	commonResponse "InkFlow/model/common/response"
	request "InkFlow/model/system/request"
	systemService "InkFlow/service/system"
	"InkFlow/utils/ginctx"

	"github.com/gin-gonic/gin"
)

// SysRoleApi handles role, menu and API-permission management requests.
type SysRoleApi struct{}

// ListRoles 返回当前租户的角色列表。
func (api *SysRoleApi) ListRoles(c *gin.Context) {
	items, err := systemService.ServiceGroupApp.SysRoleService.ListRoles(c.Request.Context(), ginctx.CurrentTenantID(c))
	commonResponse.Respond(items, err, commonResponse.ErrForbidden, c)
}

// CreateRole 在当前租户内创建自定义角色及其权限。
func (api *SysRoleApi) CreateRole(c *gin.Context) {
	var req request.SysRoleCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("请求参数无效", c)
		return
	}
	item, err := systemService.ServiceGroupApp.SysRoleService.CreateRole(c.Request.Context(), ginctx.CurrentTenantID(c), req.Name, req.Code, req.Description, req.MenuKeys, req.APIIDs)
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}

// UpdateRolePermissions 更新指定角色的菜单与 API 权限。
func (api *SysRoleApi) UpdateRolePermissions(c *gin.Context) {
	roleID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || roleID == 0 {
		commonResponse.BadRequest("角色 ID 无效", c)
		return
	}
	var req request.SysRolePermissionsUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("请求参数无效", c)
		return
	}
	commonResponse.Respond(gin.H{}, systemService.ServiceGroupApp.SysRoleService.ConfigureRolePermissions(c.Request.Context(), ginctx.CurrentTenantID(c), uint(roleID), req.MenuKeys, req.APIIDs), commonResponse.ErrForbidden, c)
}

// MyMenus 返回当前用户可见菜单及所属组织。
func (api *SysRoleApi) MyMenus(c *gin.Context) {
	item, err := systemService.ServiceGroupApp.SysRoleService.AccessForUser(c.Request.Context(), ginctx.CurrentTenantID(c), ginctx.CurrentUserID(c))
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}
