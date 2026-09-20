package system

import (
	"strconv"

	commonResponse "InkFlow/model/common/response"
	model "InkFlow/model/system"
	request "InkFlow/model/system/request"
	systemService "InkFlow/service/system"

	"github.com/gin-gonic/gin"
)

// SysMenuApi handles frontend navigation configuration.
type SysMenuApi struct{}

func (api *SysMenuApi) ListSysMenus(c *gin.Context) {
	items, err := systemService.ServiceGroupApp.SysMenuService.ListSysMenus(c.Request.Context())
	commonResponse.Respond(items, err, commonResponse.ErrForbidden, c)
}

func (api *SysMenuApi) SyncSysMenus(c *gin.Context) {
	var req request.SysMenuSync
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("请求参数无效", c)
		return
	}
	commonResponse.Respond(gin.H{}, systemService.ServiceGroupApp.SysMenuService.SyncSysMenus(c.Request.Context(), req.Menus), commonResponse.ErrForbidden, c)
}

func (api *SysMenuApi) CreateSysMenu(c *gin.Context) {
	var menu model.SysMenu
	if err := c.ShouldBindJSON(&menu); err != nil {
		commonResponse.BadRequest("请求参数无效", c)
		return
	}
	item, err := systemService.ServiceGroupApp.SysMenuService.CreateSysMenu(c.Request.Context(), menu)
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}

func (api *SysMenuApi) UpdateSysMenu(c *gin.Context) {
	menuID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || menuID == 0 {
		commonResponse.BadRequest("菜单 ID 无效", c)
		return
	}
	var menu model.SysMenu
	if err := c.ShouldBindJSON(&menu); err != nil {
		commonResponse.BadRequest("请求参数无效", c)
		return
	}
	item, err := systemService.ServiceGroupApp.SysMenuService.UpdateSysMenu(c.Request.Context(), uint(menuID), menu)
	commonResponse.Respond(item, err, commonResponse.ErrForbidden, c)
}
