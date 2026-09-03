package system

// ServiceGroup follows the GVA service-group convention. Each field maps to a
// single system domain, so callers do not rely on a generic service singleton.
type ServiceGroup struct {
	SysApiService
	SysAuthService
	SysTenantService
	SysOrganizationService
	SysMembershipService
	SysMembershipApplicationService
	SysUserService
	SysRoleService
	SysMenuService
	SysAuditService
	SysModelSettingService
}

// ServiceGroupApp is consumed by API, middleware, router and initialization.
var ServiceGroupApp = new(ServiceGroup)
