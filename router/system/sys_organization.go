package system

import (
	v1 "InkFlow/api/v1/system"
	"InkFlow/middleware"

	"github.com/gin-gonic/gin"
)

// SysOrganizationRouter binds public discovery and organization-management APIs.
type SysOrganizationRouter struct{}

func (router *SysOrganizationRouter) InitSysOrganizationRouter(Router, _ *gin.RouterGroup) {
	sysOrganizationApi := v1.ApiGroupApp.SysOrganizationApi
	applicationRouter := Router.Group("/system").Use(middleware.RequireTenant(), middleware.SystemAuthorize())
	organizationRouter := Router.Group("/system").Use(middleware.RequireTenant(), middleware.SystemAuthorize())
	{
		applicationRouter.GET("/public-organizations", sysOrganizationApi.ListPublicOrganizations)
	}
	{
		organizationRouter.GET("/organizations", sysOrganizationApi.ListOrganizations)
		organizationRouter.POST("/organizations", sysOrganizationApi.CreateOrganization)
		organizationRouter.PUT("/organizations/:id/visibility", sysOrganizationApi.SetOrganizationVisibility)
	}
}
