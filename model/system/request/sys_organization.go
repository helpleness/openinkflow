package request

// SysOrganizationCreate is the request body for creating an organization.
type SysOrganizationCreate struct {
	Name     string `json:"name" binding:"required"`
	Code     string `json:"code" binding:"required"`
	ParentID uint   `json:"parent_id"`
}

// SysOrganizationVisibilityUpdate updates whether an organization is publicly discoverable.
type SysOrganizationVisibilityUpdate struct {
	IsVisible bool `json:"is_visible"`
}
