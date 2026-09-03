package system

import "gorm.io/gorm"

type SysTenant struct {
	gorm.Model
	Name        string `json:"name" gorm:"size:128;not null"`
	Code        string `json:"code" gorm:"size:64;not null;uniqueIndex:idx_system_tenants_code"`
	Status      string `json:"status" gorm:"size:32;not null;default:active;index"`
	OwnerUserID uint   `json:"owner_user_id" gorm:"index;not null"`
}

// TableName 返回租户模型对应的数据表名。
func (SysTenant) TableName() string { return "sys_tenants" }
