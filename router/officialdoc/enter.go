// Package officialdoc provides document-writing domain HTTP routes.
package officialdoc

import "github.com/gin-gonic/gin"

// RouterGroup follows the GVA domain aggregation pattern: concrete route
// definitions stay in their own files while this entry only composes them.
type RouterGroup struct {
	KnowledgeDocumentRouter
	KnowledgeSearchRouter
	DocumentTemplateRouter
	WritingTaskRouter
	DocumentGovernanceRouter
	WritingRunRouter
}

func (router *RouterGroup) InitOfficialDocRouter(Router, PublicRouter *gin.RouterGroup) {
	router.InitKnowledgeDocumentRouter(Router, PublicRouter)
	router.InitKnowledgeSearchRouter(Router, PublicRouter)
	router.InitDocumentTemplateRouter(Router, PublicRouter)
	router.InitWritingTaskRouter(Router, PublicRouter)
	router.InitDocumentGovernanceRouter(Router, PublicRouter)
	router.InitWritingRunRouter(Router, PublicRouter)
}

var RouterGroupApp = new(RouterGroup)
