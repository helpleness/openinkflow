package system

import (
	commonResponse "InkFlow/model/common/response"
	model "InkFlow/model/system"
	request "InkFlow/model/system/request"
	systemService "InkFlow/service/system"
	"InkFlow/utils/ginctx"

	"github.com/gin-gonic/gin"
)

// SysMembershipApi handles direct member authorization requests.
type SysMembershipApi struct{}

// ListMemberships 返回当前租户的成员及其角色信息。
func (api *SysMembershipApi) ListMemberships(c *gin.Context) {
	items, err := systemService.ServiceGroupApp.SysMembershipService.ListMemberships(c.Request.Context(), ginctx.CurrentTenantID(c))
	commonResponse.Respond(items, err, commonResponse.ErrForbidden, c)
}

// AddMembership 为用户新增或更新组织成员关系和角色。
func (api *SysMembershipApi) AddMembership(c *gin.Context) {
	var req request.SysMembershipSave
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("请求参数无效", c)
		return
	}
	// A directly managed member must always receive a real role. Creating an
	// active membership with RoleID 0 makes /system/menus deliberately fall
	// back to the workspace, which looks like a successfully configured role
	// has lost all of its menus. Organization-application approval still uses
	// the dedicated service method and may remain role-less until reviewed.
	if req.RoleID == 0 {
		commonResponse.BadRequest("请选择要授予成员的角色", c)
		return
	}

	var (
		item *model.SysMembership
		err  error
	)
	if req.Username != "" {
		item, err = systemService.ServiceGroupApp.SysMembershipService.AddMembershipByUsername(c.Request.Context(), ginctx.CurrentTenantID(c), req.OrganizationID, req.Username, req.RoleID)
	} else if req.UserID != 0 {
		item, err = systemService.ServiceGroupApp.SysMembershipService.AddMembership(c.Request.Context(), ginctx.CurrentTenantID(c), req.OrganizationID, req.UserID, req.RoleID)
	} else {
		commonResponse.BadRequest("请输入用户名", c)
		return
	}
	if err == nil && req.MFAEnrollmentRequired != nil {
		err = systemService.ServiceGroupApp.SysMembershipService.SetMFAEnrollmentRequired(c.Request.Context(), ginctx.CurrentTenantID(c), item.UserID, *req.MFAEnrollmentRequired)
	}
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}
