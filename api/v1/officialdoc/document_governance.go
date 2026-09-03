package officialdoc

import (
	"strconv"

	commonResponse "InkFlow/model/common/response"
	request "InkFlow/model/officialdoc/request"
	service "InkFlow/service/officialdoc"
	"InkFlow/utils/ginctx"

	"github.com/gin-gonic/gin"
)

type DocumentGovernanceApi struct{}

func (api *DocumentGovernanceApi) Diff(c *gin.Context) {
	taskID, versionID, ok := writingTaskAndVersionIDs(c)
	if !ok {
		return
	}
	baseVersionID, err := optionalID(c.Query("base_version_id"))
	if err != nil {
		commonResponse.BadRequest("base_version_id 无效", c)
		return
	}
	item, err := service.ServiceGroupApp.DocumentGovernanceService.Diff(c.Request.Context(), ginctx.CurrentTenantID(c), taskID, versionID, baseVersionID, ginctx.CurrentUserID(c))
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}

func (api *DocumentGovernanceApi) Validate(c *gin.Context) {
	taskID, versionID, ok := writingTaskAndVersionIDs(c)
	if !ok {
		return
	}
	item, err := service.ServiceGroupApp.DocumentGovernanceService.Validate(c.Request.Context(), ginctx.CurrentTenantID(c), taskID, versionID, ginctx.CurrentUserID(c))
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}

func (api *DocumentGovernanceApi) ListComments(c *gin.Context) {
	taskID, versionID, ok := writingTaskAndVersionIDs(c)
	if !ok {
		return
	}
	items, err := service.ServiceGroupApp.DocumentGovernanceService.ListComments(c.Request.Context(), ginctx.CurrentTenantID(c), taskID, versionID, ginctx.CurrentUserID(c))
	commonResponse.Respond(items, err, commonResponse.ErrForbidden, c)
}

func (api *DocumentGovernanceApi) CreateComment(c *gin.Context) {
	taskID, versionID, ok := writingTaskAndVersionIDs(c)
	if !ok {
		return
	}
	var input request.DocumentReviewCommentCreate
	if err := c.ShouldBindJSON(&input); err != nil {
		commonResponse.BadRequest("批注参数无效", c)
		return
	}
	item, err := service.ServiceGroupApp.DocumentGovernanceService.CreateComment(c.Request.Context(), ginctx.CurrentTenantID(c), taskID, versionID, ginctx.CurrentUserID(c), input)
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}

func (api *DocumentGovernanceApi) ResolveComment(c *gin.Context) {
	taskID, versionID, ok := writingTaskAndVersionIDs(c)
	if !ok {
		return
	}
	commentID, err := requiredID(c.Param("comment_id"))
	if err != nil {
		commonResponse.BadRequest("comment_id 无效", c)
		return
	}
	var input request.DocumentReviewCommentResolve
	if err := c.ShouldBindJSON(&input); err != nil {
		commonResponse.BadRequest("批注状态参数无效", c)
		return
	}
	item, err := service.ServiceGroupApp.DocumentGovernanceService.ResolveComment(c.Request.Context(), ginctx.CurrentTenantID(c), taskID, versionID, commentID, ginctx.CurrentUserID(c), input)
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}

func writingTaskAndVersionIDs(c *gin.Context) (uint, uint, bool) {
	taskID, err := requiredID(c.Param("id"))
	if err != nil {
		commonResponse.BadRequest("写作任务 ID 无效", c)
		return 0, 0, false
	}
	versionID, err := requiredID(c.Param("version_id"))
	if err != nil {
		commonResponse.BadRequest("版本 ID 无效", c)
		return 0, 0, false
	}
	return taskID, versionID, true
}

func requiredID(raw string) (uint, error) {
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || value == 0 {
		return 0, strconv.ErrSyntax
	}
	return uint(value), nil
}

func optionalID(raw string) (uint, error) {
	if raw == "" {
		return 0, nil
	}
	return requiredID(raw)
}
