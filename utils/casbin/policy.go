package casbin

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"InkFlow/global"
	model "InkFlow/model/system"

	casbinlib "github.com/casbin/casbin/v2"
	casbinmodel "github.com/casbin/casbin/v2/model"
)

type casbinState struct {
	enforcer *casbinlib.Enforcer
	mu       sync.RWMutex
}

var casbinRuntime = new(casbinState)

// InitializeCasbin creates the shared authorization enforcer from global.GVA_DB.
func InitializeCasbin() error {
	if global.GVA_DB == nil {
		return errors.New("system database is nil")
	}
	definition, err := casbinmodel.NewModelFromString(casbinModel)
	if err != nil {
		return err
	}
	enforcer, err := casbinlib.NewEnforcer(definition, casbinAdapter{db: global.GVA_DB})
	if err != nil {
		return err
	}
	casbinRuntime.mu.Lock()
	casbinRuntime.enforcer = enforcer
	casbinRuntime.mu.Unlock()
	return nil
}

const casbinModel = `
[request_definition]
r = sub, dom, obj, act

[policy_definition]
p = sub, dom, obj, act

[role_definition]
g = _, _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub, r.dom) && keyMatch2(r.obj, p.obj) && regexMatch(r.act, p.act)
`

// Enforce 判断用户能否在指定租户中对资源执行某项操作。
func Enforce(userID, tenantID uint, object, action string) (bool, error) {
	casbinRuntime.mu.RLock()
	enforcer := casbinRuntime.enforcer
	casbinRuntime.mu.RUnlock()
	if enforcer == nil {
		return false, errors.New("casbin enforcer is not initialized")
	}
	return enforcer.Enforce(subject(userID), domain(tenantID), object, action)
}

// GrantOwnerAPIs grants every registered API to each tenant owner role.
func GrantOwnerAPIs(owners []model.SysRole, resources []model.SysApi) error {
	casbinRuntime.mu.RLock()
	enforcer := casbinRuntime.enforcer
	casbinRuntime.mu.RUnlock()
	if enforcer == nil {
		return errors.New("casbin enforcer is not initialized")
	}
	for _, owner := range owners {
		for _, resource := range resources {
			if _, err := enforcer.AddPolicy(roleSubject(owner.ID), domain(owner.TenantID), resource.Path, resource.Method); err != nil {
				return err
			}
		}
	}
	return nil
}

// ReplaceOwnerAPIs makes owner Casbin policies exactly match the current API
// registry. It is called after route synchronization so renamed or removed
// APIs cannot leave stale owner policies behind.
func ReplaceOwnerAPIs(owners []model.SysRole, resources []model.SysApi) error {
	for _, owner := range owners {
		if err := ReplaceRolePolicies(owner.TenantID, owner.ID, resources); err != nil {
			return err
		}
	}
	return nil
}

// ReplaceRolePolicies replaces the API policies assigned to one role.
func ReplaceRolePolicies(tenantID, roleID uint, resources []model.SysApi) error {
	casbinRuntime.mu.RLock()
	enforcer := casbinRuntime.enforcer
	casbinRuntime.mu.RUnlock()
	if enforcer == nil {
		return errors.New("casbin enforcer is not initialized")
	}
	if _, err := enforcer.RemoveFilteredPolicy(0, roleSubject(roleID), domain(tenantID)); err != nil {
		return err
	}
	for _, resource := range resources {
		if _, err := enforcer.AddPolicy(roleSubject(roleID), domain(tenantID), resource.Path, resource.Method); err != nil {
			return err
		}
	}
	return nil
}

// RoleAPIPaths 返回一个角色当前拥有的“路径 + 方法”策略。它只读取 Casbin 的事实
// 策略，不根据前端菜单推断权限，供角色配置页精确回显已勾选的 API。
func RoleAPIPaths(tenantID, roleID uint) ([][2]string, error) {
	casbinRuntime.mu.RLock()
	enforcer := casbinRuntime.enforcer
	casbinRuntime.mu.RUnlock()
	if enforcer == nil {
		return nil, errors.New("casbin enforcer is not initialized")
	}
	policies, err := enforcer.GetFilteredPolicy(0, roleSubject(roleID), domain(tenantID))
	if err != nil {
		return nil, err
	}
	paths := make([][2]string, 0, len(policies))
	for _, policy := range policies {
		// p = role, tenant, path, method
		if len(policy) != 4 {
			continue
		}
		paths = append(paths, [2]string{policy[2], policy[3]})
	}
	return paths, nil
}

// ReplaceMembershipRole updates the Casbin grouping policy for one member.
func ReplaceMembershipRole(userID, tenantID, previousRoleID, roleID uint) error {
	casbinRuntime.mu.RLock()
	enforcer := casbinRuntime.enforcer
	casbinRuntime.mu.RUnlock()
	if enforcer == nil {
		return errors.New("casbin enforcer is not initialized")
	}
	if previousRoleID != 0 && previousRoleID != roleID {
		if _, err := enforcer.RemoveGroupingPolicy(subject(userID), roleSubject(previousRoleID), domain(tenantID)); err != nil {
			return err
		}
	}
	_, err := enforcer.AddGroupingPolicy(subject(userID), roleSubject(roleID), domain(tenantID))
	return err
}

// SeedTenantPolicies only establishes the owner role relationship. API
// policies are maintained by SyncSysApis for owner roles; every other role is
// configured explicitly by the frontend role-permission screen.
func SeedTenantPolicies(ownerID, tenantID uint, roles []model.SysRole, ownerRoleID uint) error {
	casbinRuntime.mu.RLock()
	enforcer := casbinRuntime.enforcer
	casbinRuntime.mu.RUnlock()
	if enforcer == nil {
		return errors.New("casbin enforcer is not initialized")
	}

	for _, role := range roles {
		if role.ID == 0 {
			db := global.GVA_DB
			if err := db.Where("tenant_id = ? AND code = ?", tenantID, role.Code).First(&role).Error; err != nil {
				return err
			}
		}
		if role.Code == model.RoleOwner {
			ownerRoleID = role.ID
		}
	}

	_, err := enforcer.AddGroupingPolicy(subject(ownerID), roleSubject(ownerRoleID), domain(tenantID))
	return err
}

// EnsureBuiltinPolicies repairs owner API grants and membership role bindings.
// It never grants APIs to admin, author, reviewer, reader, or custom roles.
func EnsureBuiltinPolicies(ctx context.Context) error {
	db := global.GVA_DB
	casbinRuntime.mu.RLock()
	enforcer := casbinRuntime.enforcer
	casbinRuntime.mu.RUnlock()
	if enforcer == nil {
		return errors.New("casbin enforcer is not initialized")
	}
	var owners []model.SysRole
	if err := db.WithContext(ctx).Where("code = ?", model.RoleOwner).Find(&owners).Error; err != nil {
		return err
	}
	var resources []model.SysApi
	if err := db.WithContext(ctx).Find(&resources).Error; err != nil {
		return err
	}
	if err := ReplaceOwnerAPIs(owners, resources); err != nil {
		return err
	}

	var memberships []model.SysMembership
	if err := db.WithContext(ctx).Where("status = ?", model.UserStatusActive).Find(&memberships).Error; err != nil {
		return err
	}
	for _, membership := range memberships {
		if _, err := enforcer.AddGroupingPolicy(subject(membership.UserID), roleSubject(membership.RoleID), domain(membership.TenantID)); err != nil {
			return err
		}
	}
	return nil
}

// subject 将用户 ID 编码为 Casbin 用户主体标识。
func subject(userID uint) string {
	return fmt.Sprintf("user:%d", userID)
}

// roleSubject 将角色 ID 编码为 Casbin 角色主体标识。
func roleSubject(roleID uint) string {
	return fmt.Sprintf("role:%d", roleID)
}

// domain 将租户 ID 编码为 Casbin 策略域标识。
func domain(tenantID uint) string {
	return fmt.Sprintf("tenant:%d", tenantID)
}
