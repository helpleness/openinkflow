package system

// RouterGroup follows the GVA router-group convention: initialization owns
// assembly, while each embedded router owns only its domain's path bindings.
type RouterGroup struct {
	SysApiRouter
	SysAuthRouter
	SysTenantRouter
	SysOrganizationRouter
	SysMembershipRouter
	SysMembershipApplicationRouter
	SysRoleRouter
	SysMenuRouter
	SysAuditRouter
	SysModelSettingRouter
	SysInferenceRouter
}

// RouterGroupApp is the system router module exported to initialize/router.go.
var RouterGroupApp = new(RouterGroup)
