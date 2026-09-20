package system

import (
	v1 "InkFlow/api/v1/system"
	"InkFlow/middleware"

	"github.com/gin-gonic/gin"
)

// SysMembershipRouter binds organization applications and member authorization APIs.
type SysMembershipRouter struct{}

func (router *SysMembershipRouter) InitSysMembershipRouter(Router, _ *gin.RouterGroup) {
	sysMembershipApi := v1.ApiGroupApp.SysMembershipApi
	sysUserApi := v1.ApiGroupApp.SysUserApi
	membershipRouter := Router.Group("/system").Use(middleware.RequireTenant(), middleware.SystemAuthorize())
	{
		membershipRouter.GET("/memberships", sysMembershipApi.ListMemberships)
		membershipRouter.POST("/memberships", sysMembershipApi.AddMembership)
		membershipRouter.GET("/users", sysUserApi.ListGlobalUsers)
	}
}
