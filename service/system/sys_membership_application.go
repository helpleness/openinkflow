package system

import (
	"InkFlow/global"
	commonResponse "InkFlow/model/common/response"
	"context"
	"errors"

	model "InkFlow/model/system"
	response "InkFlow/model/system/response"
)

// SysMembershipApplicationService owns membership application operations.
type SysMembershipApplicationService struct{}

// ApplyToOrganization 为用户提交加入公开组织的申请。
func (s *SysMembershipApplicationService) ApplyToOrganization(ctx context.Context, tenantID, organizationID, userID uint) (*model.SysMembershipApplication, error) {
	db := global.GVA_DB
	var organization model.SysOrganization
	if err := db.WithContext(ctx).Where("id = ? AND tenant_id = ? AND is_visible = ? AND status = ?", organizationID, tenantID, true, model.UserStatusActive).First(&organization).Error; err != nil {
		return nil, errors.New("该组织不存在、不可见或已停用")
	}
	application := &model.SysMembershipApplication{TenantID: tenantID, OrganizationID: organizationID, UserID: userID, Status: model.ApplicationPending}
	err := db.WithContext(ctx).Where("tenant_id = ? AND organization_id = ? AND user_id = ?", tenantID, organizationID, userID).Assign(map[string]any{"status": model.ApplicationPending}).FirstOrCreate(application).Error
	return application, err
}

// ListMembershipApplications 查询用户有权查看的组织加入申请。
func (s *SysMembershipApplicationService) ListMembershipApplications(ctx context.Context, tenantID, actorID uint) ([]response.SysMembershipApplicationSummary, error) {
	db := global.GVA_DB
	query := db.WithContext(ctx).Table("sys_membership_applications AS application").
		Select("application.id, application.organization_id, organization.name AS organization_name, application.user_id, user_account.username, application.status, application.created_at").
		Joins("JOIN sys_organizations AS organization ON organization.id = application.organization_id").Joins("JOIN sys_users AS user_account ON user_account.id = application.user_id").
		Where("application.tenant_id = ? AND application.deleted_at IS NULL", tenantID)
	if !ServiceGroupApp.SysMembershipService.canManageTenant(ctx, tenantID, actorID) {
		query = query.Where("application.user_id = ?", actorID)
	}
	var items []response.SysMembershipApplicationSummary
	err := query.Order("application.created_at DESC").Scan(&items).Error
	return items, err
}

// ReviewMembershipApplication 审核加入申请。批准仅分配组织，不变更角色。
func (s *SysMembershipApplicationService) ReviewMembershipApplication(ctx context.Context, tenantID, applicationID, actorID uint, approve bool) error {
	if !ServiceGroupApp.SysMembershipService.canManageTenant(ctx, tenantID, actorID) {
		return commonResponse.ErrForbidden
	}
	db := global.GVA_DB
	var application model.SysMembershipApplication
	if err := db.WithContext(ctx).Where("id = ? AND tenant_id = ? AND status = ?", applicationID, tenantID, model.ApplicationPending).First(&application).Error; err != nil {
		return errors.New("申请不存在或已处理")
	}
	status := model.ApplicationRejected
	if approve {
		if _, err := ServiceGroupApp.SysMembershipService.AssignOrganization(ctx, tenantID, application.OrganizationID, application.UserID); err != nil {
			return err
		}
		status = model.ApplicationApproved
	}
	return db.WithContext(ctx).Model(&application).Update("status", status).Error
}
