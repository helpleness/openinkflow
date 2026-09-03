package request

// SysMembershipSave creates a member's organization assignment or updates their role.
type SysMembershipSave struct {
	UserID         uint   `json:"user_id"`
	Username       string `json:"username"`
	RoleID         uint   `json:"role_id"`
	OrganizationID uint   `json:"organization_id"`
	// MFAEnrollmentRequired is optional so older callers updating only an organization or
	// role do not accidentally clear an administrator's MFA requirement.
	MFAEnrollmentRequired *bool `json:"mfa_enrollment_required"`
}
