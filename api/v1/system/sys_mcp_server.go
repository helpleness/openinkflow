package system

import (
	"strconv"

	commonResponse "InkFlow/model/common/response"
	request "InkFlow/model/system/request"
	systemService "InkFlow/service/system"
	"InkFlow/utils/ginctx"

	"github.com/gin-gonic/gin"
)

// SysMCPServerApi manages the current user's remote MCP-server definitions.
type SysMCPServerApi struct{}

func (api *SysMCPServerApi) ListMCPServers(c *gin.Context) {
	items, err := systemService.ServiceGroupApp.SysMCPServerService.List(c.Request.Context(), ginctx.CurrentTenantID(c), ginctx.CurrentUserID(c))
	commonResponse.Respond(items, err, commonResponse.ErrForbidden, c)
}

func (api *SysMCPServerApi) CreateMCPServer(c *gin.Context) {
	var req request.SysMCPServerCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("请求参数无效", c)
		return
	}
	item, err := systemService.ServiceGroupApp.SysMCPServerService.Create(c.Request.Context(), ginctx.CurrentTenantID(c), ginctx.CurrentUserID(c), req)
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}

func (api *SysMCPServerApi) UpdateMCPServer(c *gin.Context) {
	serverID, ok := parseMCPServerID(c)
	if !ok {
		return
	}
	var req request.SysMCPServerUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("请求参数无效", c)
		return
	}
	item, err := systemService.ServiceGroupApp.SysMCPServerService.Update(c.Request.Context(), ginctx.CurrentTenantID(c), ginctx.CurrentUserID(c), serverID, req)
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}

func (api *SysMCPServerApi) DeleteMCPServer(c *gin.Context) {
	serverID, ok := parseMCPServerID(c)
	if !ok {
		return
	}
	err := systemService.ServiceGroupApp.SysMCPServerService.Delete(c.Request.Context(), ginctx.CurrentTenantID(c), ginctx.CurrentUserID(c), serverID)
	commonResponse.Respond(gin.H{}, err, commonResponse.ErrForbidden, c)
}

func parseMCPServerID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		commonResponse.BadRequest("MCP 服务编号无效", c)
		return 0, false
	}
	return uint(id), true
}
