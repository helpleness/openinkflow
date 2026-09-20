package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"InkFlow/global"
	model "InkFlow/model/system"
	"InkFlow/utils/ginctx"
	"InkFlow/utils/inference"

	"github.com/gin-gonic/gin"
)

// SystemAuth 校验会话令牌，并将当前用户写入请求上下文。
func SystemAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := ginctx.SessionToken(c)
		var session model.SysSession
		if token == "" || global.GVA_DB.WithContext(c.Request.Context()).Where("token_hash = ? AND expires_at > ?", sessionTokenHash(token), time.Now()).First(&session).Error != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "msg": "未登录或会话已过期"})
			return
		}
		var user model.SysUser
		if global.GVA_DB.WithContext(c.Request.Context()).First(&user, session.UserID).Error != nil || user.Status != model.UserStatusActive {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "msg": "未登录或会话已过期"})
			return
		}
		// Keep the device inventory useful without writing on every API request.
		// Failing to refresh metadata must never turn an otherwise valid session
		// into an authentication failure.
		if session.LastSeenAt.Before(time.Now().Add(-5 * time.Minute)) {
			_ = global.GVA_DB.WithContext(c.Request.Context()).Model(&session).Update("last_seen_at", time.Now()).Error
		}
		c.Set(ginctx.ContextUserID, user.ID)
		c.Set("system_user", &user)
		c.Set("system_session_token", token)
		// 前端 WebGPU worker 按“用户 + 浏览器实例”隔离。普通 HTTP 请求从自定义头
		// 读取 client ID，WebSocket 则因浏览器限制从查询参数读取；二者写入同一个
		// request context 后，FrontendProvider 才能找到当前页面建立的 broker。
		clientID := strings.TrimSpace(c.GetHeader("X-InkFlow-Inference-Client"))
		if clientID == "" {
			clientID = strings.TrimSpace(c.Query("client_id"))
		}
		c.Request = c.Request.WithContext(inference.WithFrontendClient(c.Request.Context(), user.ID, clientID))
		c.Next()
	}
}

func sessionTokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}
