package system

import (
	v1 "InkFlow/api/v1/system"
	"InkFlow/middleware"

	"github.com/gin-gonic/gin"
)

// SysTenantApplicationRouter separates account-level workspace requests from
// tenant-scoped review permissions.
type SysTenantApplicationRouter struct{}

func (router *SysTenantApplicationRouter) InitSysTenantApplicationRouter(Router, _ *gin.RouterGroup) {
	api := v1.ApiGroupApp.SysTenantApplicationApi
	global := Router.Group("/system")
	global.GET("/public-workspaces", api.ListPublicTenants)
	global.GET("/workspace-applications", api.ListOwnApplications)
	global.POST("/workspace-applications", api.ApplyToTenant)

	review := Router.Group("/system").Use(middleware.RequireTenant(), middleware.SystemAuthorize())
	review.GET("/workspace-applications/reviews", api.ListTenantApplications)
	review.PUT("/workspace-applications/:id", api.ReviewTenantApplication)
}
