package response

// SysUserDirectoryItem is an owner-visible user directory row.
type SysUserDirectoryItem struct {
	ID             uint   `json:"id"`
	Username       string `json:"username"`
	Status         string `json:"status"`
	OrganizationID uint   `json:"organization_id"`
	Organization   string `json:"organization_name"`
	RoleName       string `json:"role_name"`
}
