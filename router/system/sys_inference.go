package system

import (
	v1 "InkFlow/api/v1/system"
	"InkFlow/middleware"

	"github.com/gin-gonic/gin"
)

// SysInferenceRouter 绑定浏览器本地推理 worker 的受控 WebSocket 入口。
type SysInferenceRouter struct{}

func (router *SysInferenceRouter) InitSysInferenceRouter(Router, _ *gin.RouterGroup) {
	api := v1.ApiGroupApp.SysInferenceApi
	group := Router.Group("/system/inference").Use(middleware.RequireTenantQuery(), middleware.SystemAuthorize())
	{
		group.GET("/ws", api.FrontendWorkerWS)
	}
}
