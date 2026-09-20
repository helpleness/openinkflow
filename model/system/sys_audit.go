package system

import "gorm.io/gorm"

type SysAuditLog struct {
	gorm.Model
	TenantID       uint   `json:"tenant_id" gorm:"index"`
	OrganizationID uint   `json:"organization_id" gorm:"index"`
	UserID         uint   `json:"user_id" gorm:"index"`
	Action         string `json:"action" gorm:"size:128;not null;index"`
	Resource       string `json:"resource" gorm:"size:255;index"`
	Method         string `json:"method" gorm:"size:12"`
	Path           string `json:"path" gorm:"size:512"`
	Result         string `json:"result" gorm:"size:32;index"`
	StatusCode     int    `json:"status_code"`
	ClientIP       string `json:"client_ip" gorm:"size:64"`
	Detail         string `json:"detail" gorm:"type:text"`
}

// TableName 返回审计日志模型对应的数据表名。
func (SysAuditLog) TableName() string { return "sys_audit_logs" }
