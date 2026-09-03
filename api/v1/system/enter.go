package system

// ApiGroup follows the GVA API-group convention. Each field belongs to one
// domain API file; no generic API receiver is shared across the system domain.
type ApiGroup struct {
	SysApiApi                   SysApiApi
	SysAuthApi                  SysAuthApi
	SysTenantApi                SysTenantApi
	SysOrganizationApi          SysOrganizationApi
	SysMembershipApi            SysMembershipApi
	SysMembershipApplicationApi SysMembershipApplicationApi
	SysUserApi                  SysUserApi
	SysRoleApi                  SysRoleApi
	SysMenuApi                  SysMenuApi
	SysAuditApi                 SysAuditApi
	SysModelSettingApi          SysModelSettingApi
	SysInferenceApi             SysInferenceApi
}

// ApiGroupApp is consumed by the matching router modules.
var ApiGroupApp = new(ApiGroup)
