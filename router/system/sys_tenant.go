package system

import (
	v1 "InkFlow/api/v1/system"
	"InkFlow/middleware"

	"github.com/gin-gonic/gin"
)

// SysTenantRouter binds the current user's tenant directory API.
type SysTenantRouter struct{}

func (router *SysTenantRouter) InitSysTenantRouter(Router, _ *gin.RouterGroup) {
	sysTenantApi := v1.ApiGroupApp.SysTenantApi
	tenantRouter := Router.Group("/system")
	{
		tenantRouter.GET("/tenants", sysTenantApi.ListTenants)
		tenantRouter.POST("/tenants", sysTenantApi.CreateTenant)
	}
	settingsRouter := Router.Group("/system").Use(middleware.RequireTenant(), middleware.SystemAuthorize())
	{
		settingsRouter.GET("/tenant-settings", sysTenantApi.GetTenantSettings)
		settingsRouter.PUT("/tenant-settings", sysTenantApi.UpdateTenantSettings)
	}
}
