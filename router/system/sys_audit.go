package system

import (
	v1 "InkFlow/api/v1/system"
	"InkFlow/middleware"

	"github.com/gin-gonic/gin"
)

// SysAuditRouter binds audit-log access APIs.
type SysAuditRouter struct{}

func (router *SysAuditRouter) InitSysAuditRouter(Router, _ *gin.RouterGroup) {
	sysAuditApi := v1.ApiGroupApp.SysAuditApi
	sysAuditRouter := Router.Group("/system").Use(middleware.RequireTenant(), middleware.SystemAuthorize())
	{
		sysAuditRouter.GET("/audit-logs", sysAuditApi.ListAuditLogs)
	}
}
