package system

import (
	"InkFlow/global"
	casbinUtils "InkFlow/utils/casbin"
	"context"
	"errors"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"regexp"
	"strings"

	model "InkFlow/model/system"
	response "InkFlow/model/system/response"

	"gorm.io/gorm"
)

// SysTenantService owns tenant lifecycle operations.
type SysTenantService struct{}

var tenantCodePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{2,63}$`)

// CreateTenant 创建租户、根组织、内置角色及所有者成员关系。
func (s *SysTenantService) CreateTenant(ctx context.Context, ownerID uint, name, code string) (*response.SysTenantBootstrap, error) {
	db := global.GVA_DB

	name = strings.TrimSpace(name)
	if name == "" {
		name = "我的公文空间"
	}
	if code == "" {
		code = fmt.Sprintf("tenant-%d", ownerID)
	}
	code = strings.ToLower(strings.TrimSpace(code))
	if !tenantCodePattern.MatchString(code) {
		return nil, errors.New("租户代码必须以小写字母开头，只能包含小写字母、数字和连字符，长度为 3 到 64")
	}

	var bootstrap response.SysTenantBootstrap
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tenant := model.SysTenant{
			Name:        name,
			Code:        code,
			Status:      model.UserStatusActive,
			OwnerUserID: ownerID,
		}
		if err := tx.Create(&tenant).Error; err != nil {
			return err
		}

		organization := model.SysOrganization{
			TenantID:  tenant.ID,
			Name:      name,
			Code:      "root",
			IsVisible: true,
			Status:    model.UserStatusActive,
		}
		if err := tx.Create(&organization).Error; err != nil {
			return err
		}

		roles := builtinRoles(tenant.ID)
		if err := tx.Create(&roles).Error; err != nil {
			return err
		}
		ownerRole := findRole(roles, model.RoleOwner)
		if ownerRole == nil {
			return errors.New("租户所有者角色初始化失败")
		}
		membership := model.SysMembership{
			TenantID:       tenant.ID,
			OrganizationID: organization.ID,
			UserID:         ownerID,
			RoleID:         ownerRole.ID,
			Status:         model.UserStatusActive,
		}
		if err := tx.Create(&membership).Error; err != nil {
			return err
		}
		bootstrap = response.SysTenantBootstrap{
			Tenant:       &tenant,
			Organization: &organization,
			OwnerRole:    ownerRole,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if err := casbinUtils.SeedTenantPolicies(ownerID, bootstrap.Tenant.ID, builtinRoles(bootstrap.Tenant.ID), bootstrap.OwnerRole.ID); err != nil {
		return nil, err
	}
	return &bootstrap, nil
}

// ListTenants 返回用户作为启用成员可访问的租户列表。
func (s *SysTenantService) ListTenants(ctx context.Context, userID uint) ([]model.SysTenant, error) {
	db := global.GVA_DB

	var tenants []model.SysTenant
	err := db.WithContext(ctx).
		Table("sys_tenants").
		Joins("JOIN sys_memberships ON sys_memberships.tenant_id = sys_tenants.id").
		Where("sys_memberships.user_id = ? AND sys_memberships.status = ?", userID, model.UserStatusActive).
		Order("sys_tenants.name").
		Find(&tenants).Error
	return tenants, err
}

// BootstrapOwner ensures the configured bootstrap account is an owner of the
// bootstrap tenant. A tenant can have multiple owners, so an existing owner
// must never suppress the configured account's membership.
func (s *SysTenantService) BootstrapOwner(ctx context.Context, username, password, organizationName string) error {
	db := global.GVA_DB

	username = strings.TrimSpace(username)
	organizationName = strings.TrimSpace(organizationName)
	if username == "" && password == "" {
		// Desktop/test installations can opt out of a server bootstrap account.
		return nil
	}
	if username == "" || len(password) < 8 {
		return errors.New("首次初始化需要配置 system.bootstrap-owner-username 和至少 8 位的 system.bootstrap-owner-password")
	}
	if organizationName == "" {
		organizationName = "默认组织"
	}

	var user model.SysUser
	err := db.WithContext(ctx).Where("username = ?", username).First(&user).Error
	if err != nil {
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if hashErr != nil {
			return hashErr
		}
		user = model.SysUser{Username: username, LocalPasswordHash: string(hash), Status: model.UserStatusActive}
		if err := db.WithContext(ctx).Create(&user).Error; err != nil {
			return err
		}
	} else if user.Status != model.UserStatusActive {
		return errors.New("初始所有者账号已停用，请先启用该账号")
	}

	var tenant model.SysTenant
	err = db.WithContext(ctx).Where("code = ?", "default").First(&tenant).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		_, err = s.CreateTenant(ctx, user.ID, organizationName, "default")
		return err
	}
	if err != nil {
		return err
	}

	return s.ensureBootstrapOwnerMembership(ctx, &tenant, &user, organizationName)
}

// ensureBootstrapOwnerMembership creates the owner role when necessary and
// upserts the configured bootstrap user's membership in the existing bootstrap
// tenant. It intentionally permits multiple owner memberships.
func (s *SysTenantService) ensureBootstrapOwnerMembership(ctx context.Context, tenant *model.SysTenant, user *model.SysUser, organizationName string) error {
	db := global.GVA_DB
	var ownerRole model.SysRole
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("tenant_id = ? AND code = ?", tenant.ID, model.RoleOwner).First(&ownerRole).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			ownerRole = model.SysRole{
				TenantID: tenant.ID, Name: "所有者", Code: model.RoleOwner,
				Description: "组织完全控制权限", MenuKeys: encodeMenuKeys(nil), IsBuiltin: true,
			}
			if err := tx.Create(&ownerRole).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}

		var rootOrganization model.SysOrganization
		if err := tx.Where("tenant_id = ? AND code = ?", tenant.ID, "root").First(&rootOrganization).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			rootOrganization = model.SysOrganization{
				TenantID: tenant.ID, Name: organizationName, Code: "root", IsVisible: true, Status: model.UserStatusActive,
			}
			if err := tx.Create(&rootOrganization).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}

		var membership model.SysMembership
		err := tx.Where("tenant_id = ? AND user_id = ?", tenant.ID, user.ID).First(&membership).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			membership = model.SysMembership{
				TenantID: tenant.ID, OrganizationID: rootOrganization.ID, UserID: user.ID,
				RoleID: ownerRole.ID, Status: model.UserStatusActive,
			}
			return tx.Create(&membership).Error
		}
		if err != nil {
			return err
		}
		return tx.Model(&membership).Updates(map[string]any{
			"organization_id": rootOrganization.ID,
			"role_id":         ownerRole.ID,
			"status":          model.UserStatusActive,
		}).Error
	})
	if err != nil {
		return err
	}
	return casbinUtils.EnsureBuiltinPolicies(ctx)
}
