package request

// SysMCPServerCreate creates one remote Streamable HTTP MCP configuration.
// BearerToken is a write-only secret and is never returned.
type SysMCPServerCreate struct {
	Name           string  `json:"name" binding:"required"`
	EndpointURL    string  `json:"endpoint_url" binding:"required"`
	BearerToken    *string `json:"bearer_token"`
	TimeoutSeconds int     `json:"timeout_seconds"`
	Enabled        *bool   `json:"enabled"`
}

// SysMCPServerUpdate updates only supplied fields. A non-nil bearer_token
// replaces the saved token; use an empty string to clear it.
type SysMCPServerUpdate struct {
	Name           *string `json:"name"`
	EndpointURL    *string `json:"endpoint_url"`
	BearerToken    *string `json:"bearer_token"`
	TimeoutSeconds *int    `json:"timeout_seconds"`
	Enabled        *bool   `json:"enabled"`
}
