package request

// SysTenantApplicationCreate submits a request to join a public workspace.
type SysTenantApplicationCreate struct {
	TenantID uint `json:"tenant_id" binding:"required"`
}

// SysTenantApplicationReview approves or rejects a workspace request.
type SysTenantApplicationReview struct {
	Approve bool `json:"approve"`
}

// SysTenantSettingsUpdate controls whether new users may request to join this
// workspace and which existing role they receive after approval.
type SysTenantSettingsUpdate struct {
	IsVisible     bool `json:"is_visible"`
	DefaultRoleID uint `json:"default_role_id"`
}
