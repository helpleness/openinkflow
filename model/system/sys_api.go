package system

import "gorm.io/gorm"

// SysApi is the registry of server APIs available for role configuration.
// APIGroup and menu fields keep the permission screen understandable when routes grow.
//
// The database column deliberately uses api_group rather than group. GROUP is a
// SQL keyword, and using it as a column name requires driver-specific quoting.
type SysApi struct {
	gorm.Model
	APIGroup    string `json:"group" gorm:"column:api_group;size:128;not null;index;uniqueIndex:idx_system_api_route,priority:1"`
	Name        string `json:"name" gorm:"size:128;not null"`
	Description string `json:"description" gorm:"type:text"`
	Path        string `json:"path" gorm:"size:255;not null;uniqueIndex:idx_system_api_route,priority:2"`
	Method      string `json:"method" gorm:"size:12;not null;uniqueIndex:idx_system_api_route,priority:3"`
	MenuKey     string `json:"menu_key" gorm:"size:128;index"`
	MenuName    string `json:"menu_name" gorm:"size:128"`
	Sort        int    `json:"sort" gorm:"not null;default:0"`
	IsPublic    bool   `json:"is_public" gorm:"not null;default:false;index"`
}

// TableName returns the persisted system API registry table.
func (SysApi) TableName() string { return "sys_apis" }
