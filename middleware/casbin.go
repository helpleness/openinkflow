package middleware

import (
	"net/http"

	casbinUtils "InkFlow/utils/casbin"
	"InkFlow/utils/ginctx"
	"github.com/gin-gonic/gin"
)

// SystemAuthorize 根据当前用户、租户及请求资源执行 Casbin 权限校验。
func SystemAuthorize() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, _ := c.Get(ginctx.ContextUserID)
		tenantID, _ := c.Get(ginctx.ContextTenantID)
		user, userOK := userID.(uint)
		tenant, tenantOK := tenantID.(uint)
		if !userOK || !tenantOK {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "msg": "身份或租户上下文缺失"})
			return
		}

		allowed, err := casbinUtils.Enforce(user, tenant, c.Request.URL.Path, c.Request.Method)
		if err != nil || !allowed {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "msg": "无权访问当前租户资源"})
			return
		}
		c.Next()
	}
}
