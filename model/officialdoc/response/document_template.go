package response

import "time"

type DocumentTemplateView struct {
	ID             uint      `json:"id"`
	OrganizationID uint      `json:"organization_id"`
	Name           string    `json:"name"`
	Code           string    `json:"code"`
	Description    string    `json:"description"`
	Category       string    `json:"category"`
	Body           string    `json:"body"`
	Variables      []string  `json:"variables"`
	Constraints    []string  `json:"constraints"`
	IsEnabled      bool      `json:"is_enabled"`
	UpdatedAt      time.Time `json:"updated_at"`
}
