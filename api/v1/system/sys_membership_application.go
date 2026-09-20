package system

import (
	"strconv"

	commonResponse "InkFlow/model/common/response"
	request "InkFlow/model/system/request"
	systemService "InkFlow/service/system"
	"InkFlow/utils/ginctx"
	"github.com/gin-gonic/gin"
)

// SysMembershipApplicationApi handles organization-application HTTP requests.
type SysMembershipApplicationApi struct{}

// ListMembershipApplications 返回当前用户可查看的组织加入申请。
func (api *SysMembershipApplicationApi) ListMembershipApplications(c *gin.Context) {
	items, err := systemService.ServiceGroupApp.SysMembershipApplicationService.ListMembershipApplications(c.Request.Context(), ginctx.CurrentTenantID(c), ginctx.CurrentUserID(c))
	commonResponse.Respond(items, err, commonResponse.ErrForbidden, c)
}

// ApplyToOrganization 提交加入指定公开组织的申请。
func (api *SysMembershipApplicationApi) ApplyToOrganization(c *gin.Context) {
	var req request.SysMembershipApplicationCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("请选择组织", c)
		return
	}
	item, err := systemService.ServiceGroupApp.SysMembershipApplicationService.ApplyToOrganization(c.Request.Context(), ginctx.CurrentTenantID(c), req.OrganizationID, ginctx.CurrentUserID(c))
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}

// ReviewMembershipApplication 审批或驳回组织加入申请。
func (api *SysMembershipApplicationApi) ReviewMembershipApplication(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		commonResponse.BadRequest("申请编号无效", c)
		return
	}
	var req request.SysMembershipApplicationReview
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("请求参数无效", c)
		return
	}
	commonResponse.Respond(gin.H{}, systemService.ServiceGroupApp.SysMembershipApplicationService.ReviewMembershipApplication(c.Request.Context(), ginctx.CurrentTenantID(c), uint(id), ginctx.CurrentUserID(c), req.Approve), commonResponse.ErrForbidden, c)
}
