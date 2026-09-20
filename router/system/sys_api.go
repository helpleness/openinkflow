package system

import (
	v1 "InkFlow/api/v1/system"
	"InkFlow/middleware"

	"github.com/gin-gonic/gin"
)

// SysApiRouter binds the role-configurable system API registry endpoint.
type SysApiRouter struct{}

func (router *SysApiRouter) InitSysApiRouter(Router, _ *gin.RouterGroup) {
	sysApiApi := v1.ApiGroupApp.SysApiApi
	sysApiRouter := Router.Group("/system").Use(middleware.RequireTenant(), middleware.SystemAuthorize())
	{
		sysApiRouter.GET("/apis", sysApiApi.ListSysApis)
	}
}
