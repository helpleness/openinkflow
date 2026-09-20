package system

import "gorm.io/gorm"

const (
	ApplicationPending  = "pending"
	ApplicationApproved = "approved"
	ApplicationRejected = "rejected"
)

type SysMembershipApplication struct {
	gorm.Model
	TenantID       uint   `json:"tenant_id" gorm:"not null;index;uniqueIndex:idx_membership_application,priority:1"`
	OrganizationID uint   `json:"organization_id" gorm:"not null;index;uniqueIndex:idx_membership_application,priority:2"`
	UserID         uint   `json:"user_id" gorm:"not null;index;uniqueIndex:idx_membership_application,priority:3"`
	Status         string `json:"status" gorm:"size:32;not null;default:pending;index"`
}

// TableName 返回成员申请模型对应的数据表名。
func (SysMembershipApplication) TableName() string { return "sys_membership_applications" }
