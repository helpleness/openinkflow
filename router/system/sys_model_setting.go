package system

import (
	v1 "InkFlow/api/v1/system"
	"InkFlow/middleware"

	"github.com/gin-gonic/gin"
)

// SysModelSettingRouter 绑定受角色权限控制的当前用户模型配置接口。
type SysModelSettingRouter struct{}

func (router *SysModelSettingRouter) InitSysModelSettingRouter(Router, _ *gin.RouterGroup) {
	api := v1.ApiGroupApp.SysModelSettingApi
	group := Router.Group("/system").Use(middleware.RequireTenant(), middleware.SystemAuthorize())
	{
		group.GET("/model-settings", api.GetModelSettings)
		group.PUT("/model-settings", api.UpdateModelSettings)
	}
}
