package request

// SysRoleCreate creates a custom role and its initial permissions.
type SysRoleCreate struct {
	Name        string   `json:"name" binding:"required"`
	Code        string   `json:"code" binding:"required"`
	Description string   `json:"description"`
	MenuKeys    []string `json:"menu_keys"`
	APIIDs      []uint   `json:"api_ids"`
}

// SysRolePermissionsUpdate replaces a role's menus and API permissions.
type SysRolePermissionsUpdate struct {
	MenuKeys []string `json:"menu_keys"`
	APIIDs   []uint   `json:"api_ids"`
}
