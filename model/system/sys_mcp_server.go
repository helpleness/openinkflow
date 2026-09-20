package system

import "gorm.io/gorm"

// SysMCPServer stores one user's remote MCP endpoint in a tenant. The optional
// bearer token is stored in the operating-system credential manager when
// available, otherwise encrypted in AuthTokenEncrypted; it is never
// serialized in API responses.
type SysMCPServer struct {
	gorm.Model
	TenantID uint   `json:"tenant_id" gorm:"not null;uniqueIndex:idx_sys_mcp_servers_owner_name,priority:1"`
	UserID   uint   `json:"user_id" gorm:"not null;uniqueIndex:idx_sys_mcp_servers_owner_name,priority:2"`
	Name     string `json:"name" gorm:"size:128;not null;uniqueIndex:idx_sys_mcp_servers_owner_name,priority:3"`

	// Streamable HTTP is the only supported transport. It is explicit in the
	// schema so a future, separately-reviewed transport can migrate safely.
	Transport      string `json:"transport" gorm:"size:32;not null;default:streamable_http"`
	EndpointURL    string `json:"endpoint_url" gorm:"type:text"`
	TimeoutSeconds int    `json:"timeout_seconds" gorm:"not null;default:60"`
	Enabled        bool   `json:"enabled" gorm:"not null;default:true;index"`

	AuthTokenEncrypted string `json:"-" gorm:"type:text"`
}

// TableName returns the persisted MCP-server configuration table name.
func (SysMCPServer) TableName() string { return "sys_mcp_servers" }
