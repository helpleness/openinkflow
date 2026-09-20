package request

// SysApiSearch is the normalized query parameter model for the SysApi directory.
type SysApiSearch struct {
	APIGroup string `form:"api_group" json:"api_group"`
	Keyword  string `form:"keyword" json:"keyword"`
	Path     string `form:"path" json:"path"`
	Method   string `form:"method" json:"method"`
	IsPublic *bool  `form:"is_public" json:"is_public"`
}
