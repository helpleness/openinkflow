package system

import (
	"net/http"
	"strings"

	commonResponse "InkFlow/model/common/response"
	"InkFlow/utils/ginctx"
	"InkFlow/utils/inference"
	ws "InkFlow/utils/websocket"

	"github.com/gin-gonic/gin"
)

// SysInferenceApi 管理浏览器本地 WebGPU worker 与 Go 服务间的 WebSocket 通道。
type SysInferenceApi struct{}

// FrontendWorkerWS 将已通过 Casbin 授权的浏览器 worker 注册到当前用户和浏览器实例。
// 浏览器不能为 WebSocket 自定义请求头，因此租户由 Query 参数传给 RequireTenantQuery，
// 会话令牌仍由 SystemAuth 从 token 查询参数读取。
func (api *SysInferenceApi) FrontendWorkerWS(c *gin.Context) {
	clientID := strings.TrimSpace(c.Query("client_id"))
	if clientID == "" {
		commonResponse.BadRequest("缺少浏览器推理客户端标识", c)
		return
	}
	conn, err := ws.Upgrade(c.Writer, c.Request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "msg": "无法建立本地推理连接: " + err.Error()})
		return
	}
	inference.FrontendClients.Register(c.Request.Context(), ginctx.CurrentUserID(c), clientID, conn)
}
