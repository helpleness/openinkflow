package request

// DocumentTemplateCreate creates one controlled-writing template.
type DocumentTemplateCreate struct {
	OrganizationID uint     `json:"organization_id" binding:"required"`
	Name           string   `json:"name" binding:"required"`
	Code           string   `json:"code" binding:"required"`
	Description    string   `json:"description"`
	Category       string   `json:"category"`
	Body           string   `json:"body" binding:"required"`
	Variables      []string `json:"variables"`
	Constraints    []string `json:"constraints"`
	IsEnabled      *bool    `json:"is_enabled"`
}

// DocumentTemplateUpdate updates user-configurable template fields.
type DocumentTemplateUpdate struct {
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Body        string   `json:"body" binding:"required"`
	Variables   []string `json:"variables"`
	Constraints []string `json:"constraints"`
	IsEnabled   bool     `json:"is_enabled"`
}

type DocumentTemplateList struct {
	OrganizationID uint `form:"organization_id" binding:"required"`
}
