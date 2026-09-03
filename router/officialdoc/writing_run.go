package officialdoc

import (
	v1 "InkFlow/api/v1/officialdoc"
	"InkFlow/middleware"

	"github.com/gin-gonic/gin"
)

// WritingRunRouter binds the durable MCP workflow lifecycle. Route grouping
// follows the same GVA-style convention as the other officialdoc routers;
// route authorization stays explicit and close to the endpoint declaration.
type WritingRunRouter struct{}

func (router *WritingRunRouter) InitWritingRunRouter(Router, _ *gin.RouterGroup) {
	writingRunApi := v1.ApiGroupApp.WritingRunApi
	taskRouter := Router.Group("/officialdoc/writing-tasks").Use(middleware.RequireTenant(), middleware.SystemAuthorize())
	{
		taskRouter.GET("/:id/runs", writingRunApi.List)
		taskRouter.POST("/:id/runs", writingRunApi.Start)
	}
	runRouter := Router.Group("/officialdoc/writing-runs").Use(middleware.RequireTenant(), middleware.SystemAuthorize())
	{
		runRouter.GET("/:id/events", writingRunApi.Events)
		runRouter.GET("/:id", writingRunApi.Get)
		runRouter.POST("/:id/pause", writingRunApi.Pause)
		runRouter.POST("/:id/resume", writingRunApi.Resume)
	}
}
