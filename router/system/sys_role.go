package system

import (
	v1 "InkFlow/api/v1/system"
	"InkFlow/middleware"

	"github.com/gin-gonic/gin"
)

// SysRoleRouter binds menu metadata and role-permission management APIs.
type SysRoleRouter struct{}

func (router *SysRoleRouter) InitSysRoleRouter(Router, _ *gin.RouterGroup) {
	sysRoleApi := v1.ApiGroupApp.SysRoleApi
	menuRouter := Router.Group("/system").Use(middleware.RequireTenant())
	roleRouter := Router.Group("/system").Use(middleware.RequireTenant(), middleware.SystemAuthorize())
	{
		menuRouter.GET("/menus", sysRoleApi.MyMenus)
	}
	{
		roleRouter.GET("/roles", sysRoleApi.ListRoles)
		roleRouter.POST("/roles", sysRoleApi.CreateRole)
		roleRouter.PUT("/roles/:id/permissions", sysRoleApi.UpdateRolePermissions)
	}
}
