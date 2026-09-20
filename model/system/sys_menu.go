package system

import "gorm.io/gorm"

// SysMenu describes a frontend navigation node. MenuKey is the stable value
// persisted in SysRole.MenuKeys; labels and hierarchy may be configured later.
type SysMenu struct {
	gorm.Model
	Name        string `json:"name" gorm:"size:128;not null"`
	MenuKey     string `json:"menu_key" gorm:"size:128;not null;uniqueIndex:idx_sys_menus_menu_key"`
	ParentKey   string `json:"parent_key" gorm:"size:128;index"`
	ViewKey     string `json:"view_key" gorm:"size:128;index"`
	Description string `json:"description" gorm:"type:text"`
	Sort        int    `json:"sort" gorm:"not null;default:0;index"`
	IsEnabled   bool   `json:"is_enabled" gorm:"not null;default:true;index"`
}

func (SysMenu) TableName() string { return "sys_menus" }
