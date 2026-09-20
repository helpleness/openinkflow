package system

import (
	"InkFlow/global"
	commonResponse "InkFlow/model/common/response"
	model "InkFlow/model/system"
	response "InkFlow/model/system/response"
	"context"
	"errors"

	"gorm.io/gorm"
)

// SysTenantApplicationService owns public workspace discovery and requests.
type SysTenantApplicationService struct{}

// ListPublicTenants returns active, publicly joinable workspaces that the
// caller has not already joined. This account-level directory is available to
// every authenticated user, including users who already belong elsewhere.
func (s *SysTenantApplicationService) ListPublicTenants(ctx context.Context, userID uint) ([]model.SysTenant, error) {
	var tenants []model.SysTenant
	err := global.GVA_DB.WithContext(ctx).Model(&model.SysTenant{}).
		Joins("JOIN sys_roles AS default_role ON default_role.id = sys_tenants.default_role_id AND default_role.tenant_id = sys_tenants.id AND default_role.deleted_at IS NULL").
		Where(`sys_tenants.status = ? AND sys_tenants.is_visible = ? AND NOT EXISTS (
			SELECT 1 FROM sys_memberships AS membership
			WHERE membership.tenant_id = sys_tenants.id
			  AND membership.user_id = ?
			  AND membership.status = ?
			  AND membership.deleted_at IS NULL
		)`, model.UserStatusActive, true, userID, model.UserStatusActive).
		Order("sys_tenants.name").
		Find(&tenants).Error
	return tenants, err
}

// ApplyToTenant submits (or reopens) a request for a visible workspace.
func (s *SysTenantApplicationService) ApplyToTenant(ctx context.Context, tenantID, userID uint) (*model.SysTenantApplication, error) {
	if tenantID == 0 || userID == 0 {
		return nil, errors.New("工作空间或用户无效")
	}
	db := global.GVA_DB
	var tenant model.SysTenant
	if err := db.WithContext(ctx).Where("id = ? AND status = ? AND is_visible = ?", tenantID, model.UserStatusActive, true).First(&tenant).Error; err != nil {
		return nil, errors.New("该工作空间未开放申请或已停用")
	}
	var role model.SysRole
	if tenant.DefaultRoleID == 0 || db.WithContext(ctx).Where("id = ? AND tenant_id = ?", tenant.DefaultRoleID, tenant.ID).First(&role).Error != nil {
		return nil, errors.New("该工作空间尚未配置有效的默认角色")
	}
	var membership model.SysMembership
	if err := db.WithContext(ctx).Table("sys_memberships AS membership").
		Joins("JOIN sys_roles AS role ON role.id = membership.role_id AND role.deleted_at IS NULL").
		Where("membership.tenant_id = ? AND membership.user_id = ? AND membership.status = ? AND membership.deleted_at IS NULL", tenant.ID, userID, model.UserStatusActive).
		First(&membership).Error; err == nil {
		return nil, errors.New("你已加入该工作空间")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	application := &model.SysTenantApplication{TenantID: tenant.ID, UserID: userID, Status: model.ApplicationPending}
	err := db.WithContext(ctx).Where("tenant_id = ? AND user_id = ?", tenant.ID, userID).
		Assign(map[string]any{"status": model.ApplicationPending}).FirstOrCreate(application).Error
	return application, err
}

// ListOwnApplications lists an account's requests without requiring any tenant
// membership. This is the data source for the memberless onboarding menu.
func (s *SysTenantApplicationService) ListOwnApplications(ctx context.Context, userID uint) ([]response.SysTenantApplicationSummary, error) {
	return s.listApplications(ctx, "application.user_id = ?", userID)
}

// ListTenantApplications returns a workspace's requests to an authorized
// manager. Authorization is also checked in the service for defense in depth.
func (s *SysTenantApplicationService) ListTenantApplications(ctx context.Context, tenantID, actorID uint) ([]response.SysTenantApplicationSummary, error) {
	if !ServiceGroupApp.SysMembershipService.canManageTenant(ctx, tenantID, actorID) {
		return nil, commonResponse.ErrForbidden
	}
	return s.listApplications(ctx, "application.tenant_id = ?", tenantID)
}

func (s *SysTenantApplicationService) listApplications(ctx context.Context, condition string, args ...any) ([]response.SysTenantApplicationSummary, error) {
	var items []response.SysTenantApplicationSummary
	err := global.GVA_DB.WithContext(ctx).Table("sys_tenant_applications AS application").
		Select(`application.id, application.tenant_id, tenant.name AS tenant_name, tenant.code AS tenant_code,
			application.user_id, user_account.username, application.status, application.created_at`).
		Joins("JOIN sys_tenants AS tenant ON tenant.id = application.tenant_id AND tenant.deleted_at IS NULL").
		Joins("JOIN sys_users AS user_account ON user_account.id = application.user_id AND user_account.deleted_at IS NULL").
		Where("application.deleted_at IS NULL AND "+condition, args...).
		Order("application.created_at DESC").
		Scan(&items).Error
	return items, err
}

// ReviewTenantApplication approves a workspace request using the workspace's
// currently configured default role and root organization.
func (s *SysTenantApplicationService) ReviewTenantApplication(ctx context.Context, tenantID, applicationID, actorID uint, approve bool) error {
	if !ServiceGroupApp.SysMembershipService.canManageTenant(ctx, tenantID, actorID) {
		return commonResponse.ErrForbidden
	}
	db := global.GVA_DB
	var application model.SysTenantApplication
	if err := db.WithContext(ctx).Where("id = ? AND tenant_id = ? AND status = ?", applicationID, tenantID, model.ApplicationPending).First(&application).Error; err != nil {
		return errors.New("申请不存在或已处理")
	}
	status := model.ApplicationRejected
	if approve {
		var tenant model.SysTenant
		if err := db.WithContext(ctx).Where("id = ? AND status = ?", tenantID, model.UserStatusActive).First(&tenant).Error; err != nil {
			return errors.New("工作空间不存在或已停用")
		}
		var role model.SysRole
		if tenant.DefaultRoleID == 0 || db.WithContext(ctx).Where("id = ? AND tenant_id = ?", tenant.DefaultRoleID, tenantID).First(&role).Error != nil {
			return errors.New("工作空间尚未配置有效的默认角色")
		}
		var root model.SysOrganization
		if err := db.WithContext(ctx).Where("tenant_id = ? AND code = ? AND status = ?", tenantID, "root", model.UserStatusActive).First(&root).Error; err != nil {
			return errors.New("工作空间根组织不存在或已停用")
		}
		var user model.SysUser
		if err := db.WithContext(ctx).Where("id = ? AND status = ?", application.UserID, model.UserStatusActive).First(&user).Error; err != nil {
			return errors.New("申请用户不存在或已停用")
		}
		// saveMembership validates the configured role and synchronizes Casbin
		// membership policies before the application is marked approved.
		if _, err := ServiceGroupApp.SysMembershipService.saveMembership(ctx, tenantID, root.ID, &user, role.ID); err != nil {
			return err
		}
		status = model.ApplicationApproved
	}
	return db.WithContext(ctx).Model(&application).Update("status", status).Error
}
