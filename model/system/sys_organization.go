package system

import "gorm.io/gorm"

type SysOrganization struct {
	gorm.Model
	TenantID  uint   `json:"tenant_id" gorm:"not null;index;uniqueIndex:idx_system_orgs_tenant_code,priority:1"`
	ParentID  uint   `json:"parent_id" gorm:"index"`
	Name      string `json:"name" gorm:"size:128;not null"`
	Code      string `json:"code" gorm:"size:64;not null;uniqueIndex:idx_system_orgs_tenant_code,priority:2"`
	IsVisible bool   `json:"is_visible" gorm:"not null;default:true;index"`
	Status    string `json:"status" gorm:"size:32;not null;default:active;index"`
}

// TableName 返回组织模型对应的数据表名。
func (SysOrganization) TableName() string { return "sys_organizations" }
