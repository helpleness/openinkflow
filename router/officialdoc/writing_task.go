package officialdoc

import (
	v1 "InkFlow/api/v1/officialdoc"
	"InkFlow/middleware"

	"github.com/gin-gonic/gin"
)

type WritingTaskRouter struct{}

func (router *WritingTaskRouter) InitWritingTaskRouter(Router, _ *gin.RouterGroup) {
	writingTaskApi := v1.ApiGroupApp.WritingTaskApi
	taskRouter := Router.Group("/officialdoc/writing-tasks").Use(middleware.RequireTenant(), middleware.SystemAuthorize())
	{
		taskRouter.GET("", writingTaskApi.List)
		taskRouter.POST("", writingTaskApi.Create)
		taskRouter.GET("/:id", writingTaskApi.Get)
		taskRouter.POST("/:id/versions", writingTaskApi.SaveVersion)
		taskRouter.GET("/:id/versions/:version_id/export", writingTaskApi.ExportVersion)
	}
}
