package ginctx

import (
	"strings"

	"InkFlow/global"

	"github.com/gin-gonic/gin"
)

const (
	ContextUserID   = "system_user_id"
	ContextTenantID = "system_tenant_id"
)

// CurrentUserID returns the authenticated user ID placed in the Gin context.
func CurrentUserID(c *gin.Context) uint {
	value, _ := c.Get(ContextUserID)
	id, _ := value.(uint)
	return id
}

// CurrentTenantID returns the selected tenant ID placed in the Gin context.
func CurrentTenantID(c *gin.Context) uint {
	value, _ := c.Get(ContextTenantID)
	id, _ := value.(uint)
	return id
}

// BearerToken returns the request bearer token without its Authorization scheme.
func BearerToken(c *gin.Context) string {
	return strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer"))
}

// SessionToken reads browser sessions from the HttpOnly cookie first and keeps
// Authorization/query support for the desktop loopback runtime and SSE.
func SessionToken(c *gin.Context) string {
	name := strings.TrimSpace(global.GVA_CONFIG.Auth.SessionCookieName)
	if name == "" {
		name = "inkflow_session"
	}
	if token, err := c.Cookie(name); err == nil && strings.TrimSpace(token) != "" {
		return strings.TrimSpace(token)
	}
	if token := BearerToken(c); token != "" {
		return token
	}
	return strings.TrimSpace(c.Query("token"))
}
