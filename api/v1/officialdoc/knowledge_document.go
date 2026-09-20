package officialdoc

import (
	commonResponse "InkFlow/model/common/response"
	service "InkFlow/service/officialdoc"
	"InkFlow/utils/ginctx"
	"strconv"

	"github.com/gin-gonic/gin"
)

// KnowledgeDocumentApi handles knowledge-base source document imports.
type KnowledgeDocumentApi struct{}

func (api *KnowledgeDocumentApi) Import(c *gin.Context) {
	organizationID, parseErr := strconv.ParseUint(c.PostForm("organization_id"), 10, 64)
	if parseErr != nil || organizationID == 0 {
		commonResponse.BadRequest("缺少有效的 organization_id", c)
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		commonResponse.BadRequest("请选择要导入的文件", c)
		return
	}
	document, err := service.ServiceGroupApp.KnowledgeDocumentService.Import(c.Request.Context(), ginctx.CurrentTenantID(c), uint(organizationID), ginctx.CurrentUserID(c), file)
	commonResponse.Respond(document, err, commonResponse.ErrForbidden, c)
}
