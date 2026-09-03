package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"InkFlow/utils/ginctx"

	"github.com/gin-gonic/gin"
)

// RequireTenant 校验并保存请求指定的租户上下文。
func RequireTenant() gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID, err := strconv.ParseUint(strings.TrimSpace(c.GetHeader("X-InkFlow-Tenant-ID")), 10, 64)
		if err != nil || tenantID == 0 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "msg": "缺少有效的 X-InkFlow-Tenant-ID"})
			return
		}
		c.Set(ginctx.ContextTenantID, uint(tenantID))
		c.Next()
	}
}

// RequireTenantQuery 供浏览器 WebSocket 使用。浏览器原生 WebSocket API 无法自定义
// X-InkFlow-Tenant-ID 请求头，因此只允许它通过 tenant_id 查询参数携带同样的租户上下文。
func RequireTenantQuery() gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID, err := strconv.ParseUint(strings.TrimSpace(c.Query("tenant_id")), 10, 64)
		if err != nil || tenantID == 0 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "msg": "缺少有效的 tenant_id"})
			return
		}
		c.Set(ginctx.ContextTenantID, uint(tenantID))
		c.Next()
	}
}
