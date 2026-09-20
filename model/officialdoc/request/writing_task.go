package request

// WritingTaskCreate starts a controlled-writing task from a template.
type WritingTaskCreate struct {
	OrganizationID uint     `json:"organization_id" binding:"required"`
	TemplateID     uint     `json:"template_id" binding:"required"`
	Title          string   `json:"title" binding:"required"`
	Requirement    string   `json:"requirement" binding:"required"`
	Constraints    []string `json:"constraints"`
}

type WritingTaskList struct {
	OrganizationID uint `form:"organization_id" binding:"required"`
}

// WritingRunCreate starts one MCP-orchestrated run. It deliberately contains
// no user, tenant or organization identifiers: the server derives them from
// the authenticated task and request context before any MCP tool is exposed.
type WritingRunCreate struct {
	Stage         string `json:"stage" binding:"required"`
	EvidenceQuery string `json:"evidence_query"`
	EvidenceLimit int    `json:"evidence_limit"`
}

// DocumentVersionCreate stores an explicit user revision without calling a model.
type DocumentVersionCreate struct {
	Stage   string `json:"stage"`
	Content string `json:"content" binding:"required"`
}

type DocumentReviewCommentCreate struct {
	Content     string `json:"content" binding:"required"`
	ParentID    uint   `json:"parent_id"`
	AnchorStart int    `json:"anchor_start"`
	AnchorEnd   int    `json:"anchor_end"`
	Quote       string `json:"quote"`
}

type DocumentReviewCommentResolve struct {
	Resolved bool `json:"resolved"`
}
