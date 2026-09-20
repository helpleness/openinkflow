package officialdoc

import "gorm.io/gorm"

// DocumentTemplate is an organization-scoped controlled-writing template.
// Variables and Constraints are JSON arrays so the UI can evolve without schema churn.
type DocumentTemplate struct {
	gorm.Model
	TenantID       uint   `json:"tenant_id" gorm:"not null;index;uniqueIndex:idx_document_templates_scope_code,priority:1"`
	OrganizationID uint   `json:"organization_id" gorm:"not null;index;uniqueIndex:idx_document_templates_scope_code,priority:2"`
	CreatedBy      uint   `json:"created_by" gorm:"not null;index"`
	Name           string `json:"name" gorm:"size:255;not null"`
	Code           string `json:"code" gorm:"size:128;not null;uniqueIndex:idx_document_templates_scope_code,priority:3"`
	Description    string `json:"description" gorm:"type:text"`
	Category       string `json:"category" gorm:"size:128;index"`
	Body           string `json:"body" gorm:"type:text;not null"`
	Variables      string `json:"variables" gorm:"type:text"`
	Constraints    string `json:"constraints" gorm:"type:text"`
	IsEnabled      bool   `json:"is_enabled" gorm:"not null;default:true;index"`
}

func (DocumentTemplate) TableName() string { return "document_templates" }
