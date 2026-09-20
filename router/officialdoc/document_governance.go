package officialdoc

import (
	v1 "InkFlow/api/v1/officialdoc"
	"InkFlow/middleware"

	"github.com/gin-gonic/gin"
)

type DocumentGovernanceRouter struct{}

func (router *DocumentGovernanceRouter) InitDocumentGovernanceRouter(Router, _ *gin.RouterGroup) {
	api := v1.ApiGroupApp.DocumentGovernanceApi
	group := Router.Group("/officialdoc/writing-tasks").Use(middleware.RequireTenant(), middleware.SystemAuthorize())
	{
		group.GET("/:id/versions/:version_id/diff", api.Diff)
		group.POST("/:id/versions/:version_id/validate", api.Validate)
		group.GET("/:id/versions/:version_id/comments", api.ListComments)
		group.POST("/:id/versions/:version_id/comments", api.CreateComment)
		group.PUT("/:id/versions/:version_id/comments/:comment_id", api.ResolveComment)
	}
}
