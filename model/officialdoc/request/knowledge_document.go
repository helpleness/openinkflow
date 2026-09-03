package request

// KnowledgeDocumentSearch is the scoped hybrid-retrieval request.
type KnowledgeDocumentSearch struct {
	OrganizationID uint   `json:"organization_id" binding:"required"`
	Query          string `json:"query" binding:"required"`
	Limit          int    `json:"limit"`
}

// KnowledgeDocumentList is the scoped source-document listing request.
type KnowledgeDocumentList struct {
	OrganizationID uint `form:"organization_id" binding:"required"`
}
