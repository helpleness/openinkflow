package system

import "gorm.io/gorm"

// SysTenantApplication is a request from a registered account to join a
// public workspace. It is intentionally separate from organization
// applications: a successful review creates the user's first membership.
type SysTenantApplication struct {
	gorm.Model
	TenantID uint   `json:"tenant_id" gorm:"not null;index;uniqueIndex:idx_tenant_application,priority:1"`
	UserID   uint   `json:"user_id" gorm:"not null;index;uniqueIndex:idx_tenant_application,priority:2"`
	Status   string `json:"status" gorm:"size:32;not null;default:pending;index"`
}

func (SysTenantApplication) TableName() string { return "sys_tenant_applications" }
