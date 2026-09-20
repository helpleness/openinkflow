package system

import (
	commonResponse "InkFlow/model/common/response"
	request "InkFlow/model/system/request"
	systemService "InkFlow/service/system"
	"InkFlow/utils/ginctx"

	"github.com/gin-gonic/gin"
)

// SysModelSettingApi 提供当前用户可配置的模型连接接口。
type SysModelSettingApi struct{}

// GetModelSettings 返回当前租户内当前用户的模型配置，不返回密钥明文。
func (api *SysModelSettingApi) GetModelSettings(c *gin.Context) {
	data, err := systemService.ServiceGroupApp.SysModelSettingService.Get(c.Request.Context(), ginctx.CurrentTenantID(c), ginctx.CurrentUserID(c))
	commonResponse.Respond(data, err, commonResponse.ErrForbidden, c)
}

// UpdateModelSettings 保存当前用户的模型配置与可选的新密钥。
func (api *SysModelSettingApi) UpdateModelSettings(c *gin.Context) {
	var req request.SysModelSettingUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("请求参数无效", c)
		return
	}
	data, err := systemService.ServiceGroupApp.SysModelSettingService.Update(c.Request.Context(), ginctx.CurrentTenantID(c), ginctx.CurrentUserID(c), req)
	commonResponse.Respond(data, err, commonResponse.ErrForbidden, c)
}
