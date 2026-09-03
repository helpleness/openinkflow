package system

import (
	"InkFlow/global"
	commonResponse "InkFlow/model/common/response"
	strutil "InkFlow/utils"
	casbinUtils "InkFlow/utils/casbin"
	"context"
	"errors"

	model "InkFlow/model/system"
	response "InkFlow/model/system/response"

	"gorm.io/gorm"
)

// SysMembershipService owns organization membership operations.
type SysMembershipService struct{}

// canManageTenant 判断用户是否拥有管理指定租户成员的权限。
func (s *SysMembershipService) canManageTenant(ctx context.Context, tenantID, userID uint) bool {
	db := global.GVA_DB
	var count int64
	err := db.WithContext(ctx).Table("sys_memberships AS membership").
		Joins("JOIN sys_roles AS role ON role.id = membership.role_id AND role.deleted_at IS NULL").
		Where("membership.tenant_id = ? AND membership.user_id = ? AND membership.status = ? AND role.code IN ? AND membership.deleted_at IS NULL", tenantID, userID, model.UserStatusActive, []string{model.RoleOwner, model.RoleAdmin}).Count(&count).Error
	return err == nil && count > 0
}

// isTenantOwner 判断用户是否为指定租户的所有者。
func (s *SysMembershipService) isTenantOwner(ctx context.Context, tenantID, userID uint) bool {
	db := global.GVA_DB
	var count int64
	err := db.WithContext(ctx).Table("sys_memberships AS membership").
		Joins("JOIN sys_roles AS role ON role.id = membership.role_id AND role.deleted_at IS NULL").
		Where("membership.tenant_id = ? AND membership.user_id = ? AND role.code = ? AND membership.deleted_at IS NULL", tenantID, userID, model.RoleOwner).Count(&count).Error
	return err == nil && count > 0
}

// ListGlobalUsers 返回可分配成员关系的全局用户池，仅限租户所有者访问。
func (s *SysUserService) ListGlobalUsers(ctx context.Context, tenantID, actorID uint) ([]response.SysUserDirectoryItem, error) {
	if !ServiceGroupApp.SysMembershipService.isTenantOwner(ctx, tenantID, actorID) {
		return nil, commonResponse.ErrForbidden
	}
	db := global.GVA_DB
	var users []response.SysUserDirectoryItem
	err := db.WithContext(ctx).Table("sys_users AS user_account").
		Select("user_account.id, user_account.username, user_account.status, COALESCE(membership.organization_id, 0) AS organization_id, COALESCE(organization.name, '') AS organization_name, COALESCE(role.name, '') AS role_name").
		Joins("LEFT JOIN sys_memberships AS membership ON membership.user_id = user_account.id AND membership.tenant_id = ? AND membership.deleted_at IS NULL", tenantID).
		Joins("LEFT JOIN sys_organizations AS organization ON organization.id = membership.organization_id AND organization.deleted_at IS NULL").
		Joins("LEFT JOIN sys_roles AS role ON role.id = membership.role_id AND role.deleted_at IS NULL").
		Where("user_account.deleted_at IS NULL").Order("user_account.username").Scan(&users).Error
	return users, err
}

// AddMembership 为用户创建或更新其在租户内的组织与角色关系。
func (s *SysMembershipService) AddMembership(ctx context.Context, tenantID, organizationID, userID, roleID uint) (*model.SysMembership, error) {
	db := global.GVA_DB

	var user model.SysUser
	if err := db.WithContext(ctx).Where("id = ? AND status = ?", userID, model.UserStatusActive).First(&user).Error; err != nil {
		return nil, errors.New("用户不存在或已停用")
	}
	return s.saveMembership(ctx, tenantID, organizationID, &user, roleID)
}

// AddMembershipByUsername 根据用户名为用户创建或更新成员关系。
func (s *SysMembershipService) AddMembershipByUsername(ctx context.Context, tenantID, organizationID uint, username string, roleID uint) (*model.SysMembership, error) {
	db := global.GVA_DB

	username = strutil.NormalizeText(username)
	if username == "" {
		return nil, errors.New("请输入用户名")
	}
	var user model.SysUser
	if err := db.WithContext(ctx).Where("username = ? AND status = ?", username, model.UserStatusActive).First(&user).Error; err != nil {
		return nil, errors.New("未找到该用户名；请让成员先完成账号注册")
	}
	return s.saveMembership(ctx, tenantID, organizationID, &user, roleID)
}

// SetMFAEnrollmentRequired changes the tenant administrator's per-account MFA
// requirement. It deliberately does not set user.MFA.Enabled: that flag is proof
// that the user has bound and verified their own authenticator.
func (s *SysMembershipService) SetMFAEnrollmentRequired(ctx context.Context, tenantID, userID uint, required bool) error {
	if tenantID == 0 || userID == 0 {
		return errors.New("成员信息无效")
	}
	var membership model.SysMembership
	if err := global.GVA_DB.WithContext(ctx).
		Where("tenant_id = ? AND user_id = ? AND status = ?", tenantID, userID, model.UserStatusActive).
		First(&membership).Error; err != nil {
		return errors.New("成员不存在或不属于当前租户")
	}
	if err := global.GVA_DB.WithContext(ctx).Model(&model.SysUser{}).
		Where("id = ? AND status = ?", userID, model.UserStatusActive).
		Update("mfa_enrollment_required", required).Error; err != nil {
		return err
	}
	return nil
}

// AssignOrganizationByUsername adds a member to an organization without
// granting a role. Role changes are intentionally performed separately from
// the per-member authorization dialog.
func (s *SysMembershipService) AssignOrganizationByUsername(ctx context.Context, tenantID, organizationID uint, username string) (*model.SysMembership, error) {
	db := global.GVA_DB
	username = strutil.NormalizeText(username)
	if username == "" {
		return nil, errors.New("请输入用户名")
	}
	var user model.SysUser
	if err := db.WithContext(ctx).Where("username = ? AND status = ?", username, model.UserStatusActive).First(&user).Error; err != nil {
		return nil, errors.New("未找到该用户名；请让成员先完成账号注册")
	}
	return s.AssignOrganization(ctx, tenantID, organizationID, user.ID)
}

// AssignOrganization updates only the organization part of a membership.
// It deliberately does not create, grant, or replace a role: an organization
// application answers where a user belongs, while role authorization remains
// the responsibility of the member-authorization workflow.
func (s *SysMembershipService) AssignOrganization(ctx context.Context, tenantID, organizationID, userID uint) (*model.SysMembership, error) {
	db := global.GVA_DB
	var user model.SysUser
	if err := db.WithContext(ctx).Where("id = ? AND status = ?", userID, model.UserStatusActive).First(&user).Error; err != nil {
		return nil, errors.New("用户不存在或已停用")
	}
	var organization model.SysOrganization
	if err := db.WithContext(ctx).Where("id = ? AND tenant_id = ? AND status = ?", organizationID, tenantID, model.UserStatusActive).First(&organization).Error; err != nil {
		return nil, errors.New("组织不存在、不属于当前租户或已停用")
	}

	var membership model.SysMembership
	err := db.WithContext(ctx).Where("tenant_id = ? AND user_id = ?", tenantID, userID).First(&membership).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		membership = model.SysMembership{
			TenantID:       tenantID,
			OrganizationID: organizationID,
			UserID:         userID,
			// RoleID remains zero: approval must never grant a role.
			Status: model.UserStatusActive,
		}
		if err := db.WithContext(ctx).Create(&membership).Error; err != nil {
			return nil, err
		}
		return &membership, nil
	}
	if err != nil {
		return nil, err
	}
	if err := db.WithContext(ctx).Model(&membership).Updates(map[string]any{
		"organization_id": organizationID,
		"status":          model.UserStatusActive,
	}).Error; err != nil {
		return nil, err
	}
	if err := db.WithContext(ctx).First(&membership, membership.ID).Error; err != nil {
		return nil, err
	}
	return &membership, nil
}

// saveMembership 校验成员关系后持久化用户的组织和角色分配。
func (s *SysMembershipService) saveMembership(ctx context.Context, tenantID, organizationID uint, user *model.SysUser, roleID uint) (*model.SysMembership, error) {
	db := global.GVA_DB
	if user == nil || user.ID == 0 {
		return nil, errors.New("用户不存在或已停用")
	}
	var role model.SysRole
	if err := db.WithContext(ctx).Where("id = ? AND tenant_id = ?", roleID, tenantID).First(&role).Error; err != nil {
		return nil, errors.New("角色不存在或不属于当前租户")
	}
	if organizationID != 0 {
		var organization model.SysOrganization
		if err := db.WithContext(ctx).Where("id = ? AND tenant_id = ?", organizationID, tenantID).First(&organization).Error; err != nil {
			return nil, errors.New("组织不存在或不属于当前租户")
		}
	}

	var previous model.SysMembership
	err := db.WithContext(ctx).
		Where("tenant_id = ? AND user_id = ?", tenantID, user.ID).
		First(&previous).Error
	previousFound := err == nil
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if previousFound && previous.RoleID != roleID {
		var previousRole model.SysRole
		if err := db.WithContext(ctx).First(&previousRole, previous.RoleID).Error; err != nil {
			return nil, err
		}
		if previousRole.Code == model.RoleOwner && role.Code != model.RoleOwner {
			var ownerCount int64
			if err := db.WithContext(ctx).Table("sys_memberships AS membership").
				Joins("JOIN sys_roles AS owner_role ON owner_role.id = membership.role_id AND owner_role.deleted_at IS NULL").
				Where("membership.tenant_id = ? AND membership.status = ? AND membership.deleted_at IS NULL AND owner_role.code = ?", tenantID, model.UserStatusActive, model.RoleOwner).
				Count(&ownerCount).Error; err != nil {
				return nil, err
			}
			if ownerCount <= 1 {
				return nil, errors.New("不能降级当前唯一所有者；请先为另一已注册用户授予所有者角色")
			}
		}
	}

	membership := &model.SysMembership{}
	if previousFound {
		// Do not use FirstOrCreate+Assign here. On some GORM/database
		// combinations it returns the pre-update row, making a successful role
		// reassignment appear to revert after refresh. Update and reload
		// explicitly so both the database and API response are authoritative.
		if err := db.WithContext(ctx).Model(&previous).Updates(map[string]any{
			"organization_id": organizationID,
			"role_id":         roleID,
			"status":          model.UserStatusActive,
		}).Error; err != nil {
			return nil, err
		}
		if err := db.WithContext(ctx).First(membership, previous.ID).Error; err != nil {
			return nil, err
		}
	} else {
		membership = &model.SysMembership{
			TenantID:       tenantID,
			OrganizationID: organizationID,
			UserID:         user.ID,
			RoleID:         roleID,
			Status:         model.UserStatusActive,
		}
		if err := db.WithContext(ctx).Create(membership).Error; err != nil {
			return nil, err
		}
	}

	previousRoleID := uint(0)
	if previousFound {
		previousRoleID = previous.RoleID
	}
	if err := casbinUtils.ReplaceMembershipRole(user.ID, tenantID, previousRoleID, roleID); err != nil {
		return nil, err
	}
	return membership, nil
}

// ListMemberships 查询租户内成员、组织和角色的汇总信息。
func (s *SysMembershipService) ListMemberships(ctx context.Context, tenantID uint) ([]response.SysMembershipSummary, error) {
	db := global.GVA_DB

	var memberships []response.SysMembershipSummary
	err := db.WithContext(ctx).
		Table("sys_memberships AS membership").
		Select(`membership.id, membership.tenant_id, membership.organization_id,
			COALESCE(organization.name, '') AS organization_name,
			membership.user_id, user_account.username, user_account.mfa_enrollment_required, user_account.mfa_enabled,
			membership.role_id, COALESCE(role.name, '') AS role_name, COALESCE(role.code, '') AS role_code,
			membership.status, membership.created_at`).
		Joins("JOIN sys_users AS user_account ON user_account.id = membership.user_id AND user_account.deleted_at IS NULL").
		Joins("LEFT JOIN sys_roles AS role ON role.id = membership.role_id AND role.deleted_at IS NULL").
		Joins("LEFT JOIN sys_organizations AS organization ON organization.id = membership.organization_id AND organization.deleted_at IS NULL").
		Where("membership.tenant_id = ? AND membership.deleted_at IS NULL", tenantID).
		Order("user_account.username, membership.id").
		Scan(&memberships).Error
	return memberships, err
}
