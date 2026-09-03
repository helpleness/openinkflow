package system

import (
	v1 "InkFlow/api/v1/system"
	"InkFlow/middleware"

	"github.com/gin-gonic/gin"
)

// SysMembershipApplicationRouter binds organization-application APIs.
type SysMembershipApplicationRouter struct{}

func (router *SysMembershipApplicationRouter) InitSysMembershipApplicationRouter(Router, _ *gin.RouterGroup) {
	sysMembershipApplicationApi := v1.ApiGroupApp.SysMembershipApplicationApi
	memberApplicationRouter := Router.Group("/system").Use(middleware.RequireTenant(), middleware.SystemAuthorize())
	reviewRouter := Router.Group("/system").Use(middleware.RequireTenant(), middleware.SystemAuthorize())
	{
		memberApplicationRouter.GET("/membership-applications", sysMembershipApplicationApi.ListMembershipApplications)
		memberApplicationRouter.POST("/membership-applications", sysMembershipApplicationApi.ApplyToOrganization)
	}
	{
		reviewRouter.PUT("/membership-applications/:id", sysMembershipApplicationApi.ReviewMembershipApplication)
	}
}
