package response

import "time"

// SysMembershipApplicationSummary is the organization application row for users and reviewers.
type SysMembershipApplicationSummary struct {
	ID               uint      `json:"id"`
	OrganizationID   uint      `json:"organization_id"`
	OrganizationName string    `json:"organization_name"`
	UserID           uint      `json:"user_id"`
	Username         string    `json:"username"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
}
