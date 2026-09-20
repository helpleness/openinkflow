package response

import "time"

// SysMembershipSummary is the member directory row returned to management clients.
type SysMembershipSummary struct {
	ID                    uint      `json:"id"`
	TenantID              uint      `json:"tenant_id"`
	OrganizationID        uint      `json:"organization_id"`
	OrganizationName      string    `json:"organization_name"`
	UserID                uint      `json:"user_id"`
	Username              string    `json:"username"`
	RoleID                uint      `json:"role_id"`
	RoleName              string    `json:"role_name"`
	RoleCode              string    `json:"role_code"`
	Status                string    `json:"status"`
	MFAEnrollmentRequired bool      `json:"mfa_enrollment_required"`
	MFAEnabled            bool      `json:"mfa_enabled"`
	CreatedAt             time.Time `json:"created_at"`
}
