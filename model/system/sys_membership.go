package system

import "gorm.io/gorm"

type SysMembership struct {
	gorm.Model
	TenantID       uint   `json:"tenant_id" gorm:"not null;index;uniqueIndex:idx_system_memberships_user_tenant,priority:1"`
	OrganizationID uint   `json:"organization_id" gorm:"index"`
	UserID         uint   `json:"user_id" gorm:"not null;index;uniqueIndex:idx_system_memberships_user_tenant,priority:2"`
	RoleID         uint   `json:"role_id" gorm:"not null;index"`
	Status         string `json:"status" gorm:"size:32;not null;default:active;index"`
}

// TableName 返回成员关系模型对应的数据表名。
func (SysMembership) TableName() string { return "sys_memberships" }
