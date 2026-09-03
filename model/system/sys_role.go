package system

import "gorm.io/gorm"

type SysRole struct {
	gorm.Model
	TenantID    uint   `json:"tenant_id" gorm:"not null;index;uniqueIndex:idx_system_roles_tenant_code,priority:1"`
	Name        string `json:"name" gorm:"size:128;not null"`
	Code        string `json:"code" gorm:"size:64;not null;uniqueIndex:idx_system_roles_tenant_code,priority:2"`
	Description string `json:"description" gorm:"type:text"`
	MenuKeys    string `json:"menu_keys" gorm:"type:text"`
	IsBuiltin   bool   `json:"is_builtin" gorm:"not null;default:false"`
	// APIIDs 仅用于权限配置界面回显，真实授权关系仍以 Casbin 策略表为准，不落库。
	APIIDs []uint `json:"api_ids" gorm:"-"`
}

// TableName 返回角色模型对应的数据表名。
func (SysRole) TableName() string { return "sys_roles" }
