package middleware

import (
	"strings"

	"InkFlow/global"
	model "InkFlow/model/system"
	"InkFlow/utils/ginctx"

	"github.com/gin-gonic/gin"
)

// SystemAudit 记录已认证请求的执行结果，形成系统审计日志。
func SystemAudit() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		userID, _ := c.Get(ginctx.ContextUserID)
		tenantID, _ := c.Get(ginctx.ContextTenantID)
		user, _ := userID.(uint)
		tenant, _ := tenantID.(uint)
		if user == 0 {
			return
		}

		result := "success"
		if c.Writer.Status() >= 400 {
			result = "failure"
		}
		_ = global.GVA_DB.WithContext(c.Request.Context()).Create(&model.SysAuditLog{
			TenantID:   tenant,
			UserID:     user,
			Action:     strings.ToLower(c.Request.Method),
			Resource:   c.FullPath(),
			Method:     c.Request.Method,
			Path:       c.Request.URL.Path,
			Result:     result,
			StatusCode: c.Writer.Status(),
			ClientIP:   c.ClientIP(),
		})
	}
}
