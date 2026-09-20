package system

import (
	"context"
	"errors"
	"strings"

	"InkFlow/global"
	model "InkFlow/model/system"
	strutil "InkFlow/utils"
)

// SysOrganizationService owns organization operations.
type SysOrganizationService struct{}

// CreateOrganization 在租户内创建一个组织，可指定上级组织。
func (s *SysOrganizationService) CreateOrganization(ctx context.Context, tenantID, parentID uint, name, code string) (*model.SysOrganization, error) {
	db := global.GVA_DB

	organization := &model.SysOrganization{
		TenantID:  tenantID,
		ParentID:  parentID,
		Name:      strutil.NormalizeText(name),
		Code:      strings.ToLower(strutil.NormalizeText(code)),
		IsVisible: true,
		Status:    model.UserStatusActive,
	}
	if organization.Name == "" || !tenantCodePattern.MatchString(organization.Code) {
		return nil, errors.New("组织名称不能为空，组织代码格式不正确")
	}
	if parentID != 0 {
		var parent model.SysOrganization
		if err := db.WithContext(ctx).Where("id = ? AND tenant_id = ?", parentID, tenantID).First(&parent).Error; err != nil {
			return nil, errors.New("上级组织不存在或不属于当前租户")
		}
	}
	if err := db.WithContext(ctx).Create(organization).Error; err != nil {
		return nil, err
	}
	return organization, nil
}

// ListOrganizations 返回用户在指定租户中可访问的组织列表。
func (s *SysOrganizationService) ListOrganizations(ctx context.Context, tenantID, userID uint) ([]model.SysOrganization, error) {
	db := global.GVA_DB

	query := db.WithContext(ctx).Model(&model.SysOrganization{}).Where("tenant_id = ?", tenantID)
	if !ServiceGroupApp.SysMembershipService.canManageTenant(ctx, tenantID, userID) {
		var membership model.SysMembership
		_ = db.WithContext(ctx).Where("tenant_id = ? AND user_id = ?", tenantID, userID).First(&membership).Error
		query = query.Where("is_visible = ? OR id = ?", true, membership.OrganizationID)
	}
	var organizations []model.SysOrganization
	err := query.
		Order("parent_id, name").
		Find(&organizations).Error
	return organizations, err
}

// ListPublicOrganizations 返回可供成员申请加入的启用且公开的组织。
func (s *SysOrganizationService) ListPublicOrganizations(ctx context.Context, tenantID uint) ([]model.SysOrganization, error) {
	db := global.GVA_DB
	var organizations []model.SysOrganization
	err := db.WithContext(ctx).Where("tenant_id = ? AND is_visible = ? AND status = ?", tenantID, true, model.UserStatusActive).Order("parent_id, name").Find(&organizations).Error
	return organizations, err
}

// SetOrganizationVisibility 设置指定组织是否允许公开申请加入。
func (s *SysOrganizationService) SetOrganizationVisibility(ctx context.Context, tenantID, organizationID uint, visible bool) (*model.SysOrganization, error) {
	db := global.GVA_DB
	var organization model.SysOrganization
	if err := db.WithContext(ctx).Where("id = ? AND tenant_id = ?", organizationID, tenantID).First(&organization).Error; err != nil {
		return nil, errors.New("组织不存在或不属于当前租户")
	}
	if err := db.WithContext(ctx).Model(&organization).Update("is_visible", visible).Error; err != nil {
		return nil, err
	}
	organization.IsVisible = visible
	return &organization, nil
}
