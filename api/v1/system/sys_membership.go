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

	var (
		item *model.SysMembership
		err  error
	)
	if req.Username != "" {
		if req.RoleID == 0 {
			item, err = systemService.ServiceGroupApp.SysMembershipService.AssignOrganizationByUsername(c.Request.Context(), ginctx.CurrentTenantID(c), req.OrganizationID, req.Username)
		} else {
			item, err = systemService.ServiceGroupApp.SysMembershipService.AddMembershipByUsername(c.Request.Context(), ginctx.CurrentTenantID(c), req.OrganizationID, req.Username, req.RoleID)
		}
	} else if req.UserID != 0 {
		if req.RoleID == 0 {
			item, err = systemService.ServiceGroupApp.SysMembershipService.AssignOrganization(c.Request.Context(), ginctx.CurrentTenantID(c), req.OrganizationID, req.UserID)
		} else {
			item, err = systemService.ServiceGroupApp.SysMembershipService.AddMembership(c.Request.Context(), ginctx.CurrentTenantID(c), req.OrganizationID, req.UserID, req.RoleID)
		}
	} else {
		commonResponse.BadRequest("请输入用户名", c)
		return
	}
	if err == nil && req.MFAEnrollmentRequired != nil {
		err = systemService.ServiceGroupApp.SysMembershipService.SetMFAEnrollmentRequired(c.Request.Context(), ginctx.CurrentTenantID(c), item.UserID, *req.MFAEnrollmentRequired)
	}
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}
