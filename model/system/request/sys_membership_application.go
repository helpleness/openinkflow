package request

// SysMembershipApplicationCreate submits a request to join a public organization.
type SysMembershipApplicationCreate struct {
	OrganizationID uint `json:"organization_id" binding:"required"`
}

// SysMembershipApplicationReview approves or rejects an organization application.
type SysMembershipApplicationReview struct {
	Approve bool `json:"approve"`
}
