package system

import (
	"InkFlow/global"
	"context"

	model "InkFlow/model/system"
)

// SysAuditService owns system audit-log operations.
type SysAuditService struct{}

type AuditEntry struct {
	TenantID       uint
	OrganizationID uint
	UserID         uint
	Action         string
	Resource       string
	Method         string
	Path           string
	Result         string
	StatusCode     int
	ClientIP       string
	Detail         string
}

// RecordAudit 将一次系统操作写入审计日志。
func (s *SysAuditService) RecordAudit(ctx context.Context, entry AuditEntry) error {
	db := global.GVA_DB
	log := model.SysAuditLog{
		TenantID:       entry.TenantID,
		OrganizationID: entry.OrganizationID,
		UserID:         entry.UserID,
		Action:         entry.Action,
		Resource:       entry.Resource,
		Method:         entry.Method,
		Path:           entry.Path,
		Result:         entry.Result,
		StatusCode:     entry.StatusCode,
		ClientIP:       entry.ClientIP,
		Detail:         entry.Detail,
	}
	return db.WithContext(ctx).Create(&log).Error
}

// ListAuditLogs 按时间倒序读取指定租户的审计日志。
func (s *SysAuditService) ListAuditLogs(ctx context.Context, tenantID uint, limit int) ([]model.SysAuditLog, error) {
	db := global.GVA_DB
	if limit <= 0 || limit > 200 {
		limit = 100
	}

	var logs []model.SysAuditLog
	err := db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("id DESC").
		Limit(limit).
		Find(&logs).Error
	return logs, err
}
