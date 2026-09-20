package system

import (
	"strconv"

	commonResponse "InkFlow/model/common/response"
	systemService "InkFlow/service/system"
	"InkFlow/utils/ginctx"

	"github.com/gin-gonic/gin"
)

// SysAuditApi handles audit-log HTTP requests.
type SysAuditApi struct{}

// ListAuditLogs 返回当前租户最近的审计日志。
func (api *SysAuditApi) ListAuditLogs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	items, err := systemService.ServiceGroupApp.SysAuditService.ListAuditLogs(c.Request.Context(), ginctx.CurrentTenantID(c), limit)
	commonResponse.Respond(items, err, commonResponse.ErrForbidden, c)
}
