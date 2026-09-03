package response

import model "InkFlow/model/system"

// SysTenantBootstrap is the initial tenant, root organization and owner role.
type SysTenantBootstrap struct {
	Tenant       *model.SysTenant       `json:"tenant"`
	Organization *model.SysOrganization `json:"organization"`
	OwnerRole    *model.SysRole         `json:"owner_role"`
}
