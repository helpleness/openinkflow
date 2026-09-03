package request

import model "InkFlow/model/system"

// SysMenuSync carries frontend-owned default menu definitions. The server only
// inserts missing MenuKey values and never overwrites an administrator's edits.
type SysMenuSync struct {
	Menus []model.SysMenu `json:"menus" binding:"required"`
}
