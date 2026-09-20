package system

import (
	v1 "InkFlow/api/v1/system"
	"InkFlow/middleware"

	"github.com/gin-gonic/gin"
)

// SysMCPServerRouter binds tenant-scoped, per-user remote MCP APIs.
type SysMCPServerRouter struct{}

func (router *SysMCPServerRouter) InitSysMCPServerRouter(Router, _ *gin.RouterGroup) {
	api := v1.ApiGroupApp.SysMCPServerApi
	group := Router.Group("/system").Use(middleware.RequireTenant(), middleware.SystemAuthorize())
	{
		group.GET("/mcp-servers", api.ListMCPServers)
		group.POST("/mcp-servers", api.CreateMCPServer)
		group.PUT("/mcp-servers/:id", api.UpdateMCPServer)
		group.DELETE("/mcp-servers/:id", api.DeleteMCPServer)
	}
}
