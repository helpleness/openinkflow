package system

import (
	v1 "InkFlow/api/v1/system"
	"InkFlow/middleware"

	"github.com/gin-gonic/gin"
)

// SysMenuRouter binds frontend menu configuration APIs.
type SysMenuRouter struct{}

func (router *SysMenuRouter) InitSysMenuRouter(Router, _ *gin.RouterGroup) {
	sysMenuApi := v1.ApiGroupApp.SysMenuApi
	menuReadRouter := Router.Group("/system").Use(middleware.RequireTenant())
	menuManageRouter := Router.Group("/system").Use(middleware.RequireTenant(), middleware.SystemAuthorize())
	{
		menuReadRouter.GET("/menu-configs", sysMenuApi.ListSysMenus)
	}
	{
		menuManageRouter.POST("/menu-configs/sync", sysMenuApi.SyncSysMenus)
		menuManageRouter.POST("/menu-configs", sysMenuApi.CreateSysMenu)
		menuManageRouter.PUT("/menu-configs/:id", sysMenuApi.UpdateSysMenu)
	}
}
