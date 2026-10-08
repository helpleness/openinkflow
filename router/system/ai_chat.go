package system

import (
	v1 "InkFlow/api/v1/system"
	"InkFlow/middleware"

	"github.com/gin-gonic/gin"
)

// AIChatRouter registers chat, prompt-skill, and conversation history APIs.
// All routes are tenant-scoped and use the same authorization guard as model
// and MCP settings.
type AIChatRouter struct{}

func (router *AIChatRouter) InitAIChatRouter(Router, _ *gin.RouterGroup) {
	api := v1.ApiGroupApp.AIChatApi
	group := Router.Group("/system").Use(middleware.RequireTenant(), middleware.SystemAuthorize())
	{
		group.GET("/ai-chat/conversations", api.ListConversations)
		group.POST("/ai-chat/conversations", api.CreateConversation)
		group.GET("/ai-chat/conversations/:id", api.GetConversation)
		group.DELETE("/ai-chat/conversations/:id", api.DeleteConversation)
		group.POST("/ai-chat/conversations/:id/messages", api.SendMessage)
		group.POST("/ai-chat/conversations/:id/messages/stream", api.StreamMessage)

		group.GET("/ai-skills", api.ListSkills)
		group.POST("/ai-skills", api.CreateSkill)
		group.PUT("/ai-skills/:id", api.UpdateSkill)
		group.DELETE("/ai-skills/:id", api.DeleteSkill)
	}
}
