package officialdoc

import (
	v1 "InkFlow/api/v1/officialdoc"
	"InkFlow/middleware"

	"github.com/gin-gonic/gin"
)

type KnowledgeSearchRouter struct{}

func (router *KnowledgeSearchRouter) InitKnowledgeSearchRouter(Router, _ *gin.RouterGroup) {
	knowledgeSearchApi := v1.ApiGroupApp.KnowledgeSearchApi
	knowledgeRouter := Router.Group("/officialdoc/knowledge-documents").Use(middleware.RequireTenant(), middleware.SystemAuthorize())
	{
		knowledgeRouter.GET("", knowledgeSearchApi.ListDocuments)
		knowledgeRouter.GET("/:id", knowledgeSearchApi.GetDocument)
		knowledgeRouter.GET("/:id/download", knowledgeSearchApi.DownloadDocument)
		knowledgeRouter.POST("/:id/reindex", knowledgeSearchApi.ReindexDocument)
		knowledgeRouter.DELETE("/:id", knowledgeSearchApi.DeleteDocument)
	}
	searchRouter := Router.Group("/officialdoc/knowledge-search").Use(middleware.RequireTenant(), middleware.SystemAuthorize())
	{
		searchRouter.POST("", knowledgeSearchApi.Search)
	}
}
