package response

import "time"

// SysTenantApplicationSummary is shared by applicants and workspace reviewers.
type SysTenantApplicationSummary struct {
	ID         uint      `json:"id"`
	TenantID   uint      `json:"tenant_id"`
	TenantName string    `json:"tenant_name"`
	TenantCode string    `json:"tenant_code"`
	UserID     uint      `json:"user_id"`
	Username   string    `json:"username"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}
