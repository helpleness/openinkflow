package system

import (
	"InkFlow/global"
	strutil "InkFlow/utils"
	casbinUtils "InkFlow/utils/casbin"
	"context"
	"encoding/json"
	"errors"
	"strings"

	model "InkFlow/model/system"
	response "InkFlow/model/system/response"
)

// SysRoleService owns role and permission operations.
type SysRoleService struct{}

type apiRouteKey struct {
	path   string
	method string
}

// ListRoles 返回指定租户的全部角色。
func (s *SysRoleService) ListRoles(ctx context.Context, tenantID uint) ([]model.SysRole, error) {
	db := global.GVA_DB

	var roles []model.SysRole
	err := db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("is_builtin DESC, name").
		Find(&roles).Error
	if err != nil {
		return nil, err
	}

	// API 勾选状态来自 Casbin 而不是角色表。将策略映射回 SysApi.ID 后返回，前端
	// 再次保存菜单时就不会意外清空这个角色原本拥有的 API 权限。
	var resources []model.SysApi
	if err := db.WithContext(ctx).Find(&resources).Error; err != nil {
		return nil, err
	}
	apiIDsByRoute := make(map[apiRouteKey]uint, len(resources))
	for _, resource := range resources {
		apiIDsByRoute[apiRouteKey{path: resource.Path, method: resource.Method}] = resource.ID
	}
	for index := range roles {
		paths, err := casbinUtils.RoleAPIPaths(tenantID, roles[index].ID)
		if err != nil {
			return nil, err
		}
		roles[index].APIIDs = make([]uint, 0, len(paths))
		for _, pair := range paths {
			if apiID, ok := apiIDsByRoute[apiRouteKey{path: pair[0], method: pair[1]}]; ok {
				roles[index].APIIDs = append(roles[index].APIIDs, apiID)
			}
		}
	}
	return roles, nil
}

// CreateRole 在租户中创建自定义角色并配置其菜单和 API 权限。
func (s *SysRoleService) CreateRole(ctx context.Context, tenantID uint, name, code, description string, menuKeys []string, apiIDs []uint) (*model.SysRole, error) {
	db := global.GVA_DB
	name = strutil.NormalizeText(name)
	code = strings.ToLower(strutil.NormalizeText(code))
	description = strutil.NormalizeText(description)
	if name == "" || !tenantCodePattern.MatchString(code) {
		return nil, errors.New("角色名称不能为空，角色代码格式不正确")
	}
	if code == model.RoleOwner {
		return nil, errors.New("所有者角色由系统自动维护，不能手动创建")
	}
	role := &model.SysRole{TenantID: tenantID, Name: name, Code: code, Description: description, MenuKeys: encodeMenuKeys(menuKeys)}
	if err := db.WithContext(ctx).Create(role).Error; err != nil {
		return nil, err
	}
	if err := s.ConfigureRolePermissions(ctx, tenantID, role.ID, menuKeys, apiIDs); err != nil {
		return nil, err
	}
	return role, nil
}

// ConfigureRolePermissions 更新角色的菜单可见范围和 API 访问策略。
func (s *SysRoleService) ConfigureRolePermissions(ctx context.Context, tenantID, roleID uint, menuKeys []string, apiIDs []uint) error {
	db := global.GVA_DB
	var role model.SysRole
	if err := db.WithContext(ctx).Where("id = ? AND tenant_id = ?", roleID, tenantID).First(&role).Error; err != nil {
		return errors.New("角色不存在或不属于当前组织")
	}
	if err := db.WithContext(ctx).Model(&role).Update("menu_keys", encodeMenuKeys(menuKeys)).Error; err != nil {
		return err
	}
	if role.Code == model.RoleOwner {
		resources, err := s.ListOwnerAPIs(ctx)
		if err != nil {
			return err
		}
		return casbinUtils.ReplaceRolePolicies(tenantID, role.ID, resources)
	}

	var resources []model.SysApi
	if len(apiIDs) > 0 {
		if err := db.WithContext(ctx).Where("id IN ?", apiIDs).Find(&resources).Error; err != nil {
			return err
		}
		if len(resources) != len(uniqueIDs(apiIDs)) {
			return errors.New("存在无效的 API 权限项")
		}
	}

	return casbinUtils.ReplaceRolePolicies(tenantID, role.ID, resources)
}

// MenusForUser 返回用户在指定租户中可见的菜单键。
func (s *SysRoleService) MenusForUser(ctx context.Context, tenantID, userID uint) ([]string, error) {
	access, err := s.AccessForUser(ctx, tenantID, userID)
	return access.Menus, err
}

// AccessForUser 返回用户的菜单访问范围和所属组织信息。
func (s *SysRoleService) AccessForUser(ctx context.Context, tenantID, userID uint) (response.SysRoleMenuAccess, error) {
	db := global.GVA_DB
	var membership model.SysMembership
	if err := db.WithContext(ctx).Where("tenant_id = ? AND user_id = ? AND status = ?", tenantID, userID, model.UserStatusActive).First(&membership).Error; err != nil {
		return response.SysRoleMenuAccess{Menus: []string{"workspace"}}, nil
	}
	var role model.SysRole
	if err := db.WithContext(ctx).First(&role, membership.RoleID).Error; err != nil {
		return response.SysRoleMenuAccess{Menus: []string{"workspace"}, OrganizationID: membership.OrganizationID}, nil
	}
	return response.SysRoleMenuAccess{
		Menus:          decodeMenuKeys(role.MenuKeys, role.Code),
		OrganizationID: membership.OrganizationID,
		RoleCode:       role.Code,
		RoleID:         role.ID,
	}, nil
}

// ListOwnerAPIs returns the complete current API registry. Owner APIs are not
// chosen by a form submission: every registered application API is maintained
// as a Casbin policy for the owner role.
func (s *SysRoleService) ListOwnerAPIs(ctx context.Context) ([]model.SysApi, error) {
	db := global.GVA_DB
	var resources []model.SysApi
	err := db.WithContext(ctx).Order("api_group, sort, path, method").Find(&resources).Error
	return resources, err
}

// encodeMenuKeys 将菜单键规范化后编码为 JSON 字符串。
func encodeMenuKeys(keys []string) string {
	normalized := normalizeMenuKeys(keys)
	encoded, _ := json.Marshal(normalized)
	return string(encoded)
}

// decodeMenuKeys parses menu keys configured by the frontend.
func decodeMenuKeys(raw, _ string) []string {
	var keys []string
	if json.Unmarshal([]byte(raw), &keys) == nil && len(keys) > 0 {
		return normalizeMenuKeys(keys)
	}
	return []string{}
}

// normalizeMenuKeys only deduplicates frontend-owned menu keys.
func normalizeMenuKeys(keys []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strutil.NormalizeText(key)
		if _, exists := seen[key]; !exists {
			seen[key] = struct{}{}
			result = append(result, key)
		}
	}
	return result
}

// uniqueIDs 过滤零值并移除 ID 列表中的重复项。
func uniqueIDs(ids []uint) []uint {
	seen := map[uint]struct{}{}
	result := make([]uint, 0, len(ids))
	for _, id := range ids {
		if _, exists := seen[id]; id != 0 && !exists {
			seen[id] = struct{}{}
			result = append(result, id)
		}
	}
	return result
}

// builtinRoles creates only the system-owned owner role. All other roles are
// created and configured explicitly from the frontend.
func builtinRoles(tenantID uint) []model.SysRole {
	return []model.SysRole{
		{TenantID: tenantID, Name: "所有者", Code: model.RoleOwner, Description: "组织完全控制权限", MenuKeys: encodeMenuKeys(nil), IsBuiltin: true},
	}
}

// findRole 在角色列表中按角色代码查找角色。
func findRole(roles []model.SysRole, code string) *model.SysRole {
	for index := range roles {
		if roles[index].Code == code {
			return &roles[index]
		}
	}
	return nil
}
