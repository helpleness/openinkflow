package officialdoc

import (
	v1 "InkFlow/api/v1/officialdoc"
	"InkFlow/middleware"

	"github.com/gin-gonic/gin"
)

type KnowledgeDocumentRouter struct{}

func (router *KnowledgeDocumentRouter) InitKnowledgeDocumentRouter(Router, _ *gin.RouterGroup) {
	knowledgeDocumentApi := v1.ApiGroupApp.KnowledgeDocumentApi
	knowledgeRouter := Router.Group("/officialdoc/knowledge-documents").Use(middleware.RequireTenant(), middleware.SystemAuthorize())
	{
		knowledgeRouter.POST("/import", knowledgeDocumentApi.Import)
	}
}
