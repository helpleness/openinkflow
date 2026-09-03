package officialdoc

import (
	v1 "InkFlow/api/v1/officialdoc"
	"InkFlow/middleware"

	"github.com/gin-gonic/gin"
)

type DocumentTemplateRouter struct{}

func (router *DocumentTemplateRouter) InitDocumentTemplateRouter(Router, _ *gin.RouterGroup) {
	documentTemplateApi := v1.ApiGroupApp.DocumentTemplateApi
	templateRouter := Router.Group("/officialdoc/document-templates").Use(middleware.RequireTenant(), middleware.SystemAuthorize())
	{
		templateRouter.GET("", documentTemplateApi.List)
		templateRouter.POST("", documentTemplateApi.Create)
		templateRouter.PUT("/:id", documentTemplateApi.Update)
	}
}
