package system

import (
	commonResponse "InkFlow/model/common/response"
	request "InkFlow/model/system/request"
	systemService "InkFlow/service/system"

	"github.com/gin-gonic/gin"
)

// SysApiApi handles the role-configurable system API registry.
type SysApiApi struct{}

// ListSysApis returns the system API registry using normalized search parameters.
func (api *SysApiApi) ListSysApis(c *gin.Context) {
	var search request.SysApiSearch
	if err := c.ShouldBindQuery(&search); err != nil {
		commonResponse.BadRequest("查询参数无效", c)
		return
	}
	items, err := systemService.ServiceGroupApp.SysApiService.ListSysApis(c.Request.Context(), search)
	commonResponse.Respond(items, err, commonResponse.ErrForbidden, c)
}
