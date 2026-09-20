package request

// SysTenantCreate is the request body for creating a tenant.
type SysTenantCreate struct {
	Name string `json:"name" binding:"required"`
	Code string `json:"code"`
}
