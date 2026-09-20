package response

// SysRoleMenuAccess is the current user's tenant menu and organization scope.
type SysRoleMenuAccess struct {
	Menus          []string `json:"menus"`
	OrganizationID uint     `json:"organization_id"`
	RoleCode       string   `json:"role_code"`
	RoleID         uint     `json:"role_id"`
}
