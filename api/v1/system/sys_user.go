package system

import (
	commonResponse "InkFlow/model/common/response"
	systemService "InkFlow/service/system"
	"InkFlow/utils/ginctx"

	"github.com/gin-gonic/gin"
)

// SysUserApi handles tenant-scoped global-user directory requests.
type SysUserApi struct{}

// ListGlobalUsers returns users available for owner-managed member assignment.
func (api *SysUserApi) ListGlobalUsers(c *gin.Context) {
	items, err := systemService.ServiceGroupApp.SysUserService.ListGlobalUsers(c.Request.Context(), ginctx.CurrentTenantID(c), ginctx.CurrentUserID(c))
	commonResponse.Respond(items, err, commonResponse.ErrForbidden, c)
}
