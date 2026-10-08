package system

import (
	commonResponse "InkFlow/model/common/response"
	request "InkFlow/model/system/request"
	systemService "InkFlow/service/system"
	"InkFlow/utils/ginctx"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// AIChatApi exposes the private, user-scoped chat and prompt-skill APIs.
type AIChatApi struct{}

func (api *AIChatApi) ListConversations(c *gin.Context) {
	organizationID, ok := parseAIChatOrganizationID(c)
	if !ok {
		return
	}
	items, err := systemService.ServiceGroupApp.AIChatService.ListConversations(c.Request.Context(), ginctx.CurrentTenantID(c), ginctx.CurrentUserID(c), organizationID)
	commonResponse.Respond(items, err, commonResponse.ErrForbidden, c)
}

func (api *AIChatApi) CreateConversation(c *gin.Context) {
	var req request.AIChatConversationCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("请求参数无效", c)
		return
	}
	item, err := systemService.ServiceGroupApp.AIChatService.CreateConversation(c.Request.Context(), ginctx.CurrentTenantID(c), ginctx.CurrentUserID(c), req)
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}

func (api *AIChatApi) GetConversation(c *gin.Context) {
	conversationID, ok := parseAIChatID(c, "会话编号无效")
	if !ok {
		return
	}
	item, err := systemService.ServiceGroupApp.AIChatService.GetConversation(c.Request.Context(), ginctx.CurrentTenantID(c), ginctx.CurrentUserID(c), conversationID)
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}

func (api *AIChatApi) DeleteConversation(c *gin.Context) {
	conversationID, ok := parseAIChatID(c, "会话编号无效")
	if !ok {
		return
	}
	err := systemService.ServiceGroupApp.AIChatService.DeleteConversation(c.Request.Context(), ginctx.CurrentTenantID(c), ginctx.CurrentUserID(c), conversationID)
	commonResponse.Respond(gin.H{}, err, commonResponse.ErrForbidden, c)
}

func (api *AIChatApi) SendMessage(c *gin.Context) {
	conversationID, ok := parseAIChatID(c, "会话编号无效")
	if !ok {
		return
	}
	var req request.AIChatSendMessage
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("请求参数无效", c)
		return
	}
	item, err := systemService.ServiceGroupApp.AIChatService.SendMessage(c.Request.Context(), ginctx.CurrentTenantID(c), ginctx.CurrentUserID(c), conversationID, req)
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}

// StreamMessage sends one chat turn as Server-Sent Events. Unlike the regular
// JSON endpoint, it exposes MCP trace entries and assistant token deltas while
// retaining the final response only after it has been committed to storage.
func (api *AIChatApi) StreamMessage(c *gin.Context) {
	conversationID, ok := parseAIChatID(c, "会话编号无效")
	if !ok {
		return
	}
	var req request.AIChatSendMessage
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("请求参数无效", c)
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	c.Writer.Flush()
	send := func(event string, payload any) {
		c.SSEvent(event, payload)
		c.Writer.Flush()
	}

	send("started", gin.H{"message": "正在准备对话"})
	item, err := systemService.ServiceGroupApp.AIChatService.StreamMessage(c.Request.Context(), ginctx.CurrentTenantID(c), ginctx.CurrentUserID(c), conversationID, req, send)
	if err != nil {
		send("error", gin.H{"message": err.Error()})
		return
	}
	send("done", item)
}

func (api *AIChatApi) ListSkills(c *gin.Context) {
	items, err := systemService.ServiceGroupApp.AIChatService.ListSkills(c.Request.Context(), ginctx.CurrentTenantID(c), ginctx.CurrentUserID(c))
	commonResponse.Respond(items, err, commonResponse.ErrForbidden, c)
}

func (api *AIChatApi) CreateSkill(c *gin.Context) {
	var req request.AISkillCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("请求参数无效", c)
		return
	}
	item, err := systemService.ServiceGroupApp.AIChatService.CreateSkill(c.Request.Context(), ginctx.CurrentTenantID(c), ginctx.CurrentUserID(c), req)
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}

func (api *AIChatApi) UpdateSkill(c *gin.Context) {
	skillID, ok := parseAIChatID(c, "Skill 编号无效")
	if !ok {
		return
	}
	var req request.AISkillUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("请求参数无效", c)
		return
	}
	item, err := systemService.ServiceGroupApp.AIChatService.UpdateSkill(c.Request.Context(), ginctx.CurrentTenantID(c), ginctx.CurrentUserID(c), skillID, req)
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}

func (api *AIChatApi) DeleteSkill(c *gin.Context) {
	skillID, ok := parseAIChatID(c, "Skill 编号无效")
	if !ok {
		return
	}
	err := systemService.ServiceGroupApp.AIChatService.DeleteSkill(c.Request.Context(), ginctx.CurrentTenantID(c), ginctx.CurrentUserID(c), skillID)
	commonResponse.Respond(gin.H{}, err, commonResponse.ErrForbidden, c)
}

func parseAIChatOrganizationID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Query("organization_id"), 10, 64)
	if err != nil || id == 0 {
		commonResponse.BadRequest("缺少有效的 organization_id", c)
		return 0, false
	}
	return uint(id), true
}

func parseAIChatID(c *gin.Context, message string) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		commonResponse.BadRequest(message, c)
		return 0, false
	}
	return uint(id), true
}
