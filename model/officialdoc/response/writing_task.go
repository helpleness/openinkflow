package response

import "time"

type DocumentVersionView struct {
	ID        uint                `json:"id"`
	Version   int                 `json:"version"`
	Stage     string              `json:"stage"`
	Content   string              `json:"content"`
	Model     string              `json:"model,omitempty"`
	CreatedAt time.Time           `json:"created_at"`
	Evidence  []KnowledgeEvidence `json:"evidence,omitempty"`
}

type DocumentDiffSegment struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type DocumentVersionDiff struct {
	BaseVersionID   uint                  `json:"base_version_id"`
	TargetVersionID uint                  `json:"target_version_id"`
	Segments        []DocumentDiffSegment `json:"segments"`
}

type DocumentValidationFinding struct {
	Rule     string `json:"rule"`
	Category string `json:"category"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Line     int    `json:"line,omitempty"`
	Excerpt  string `json:"excerpt,omitempty"`
}

type DocumentValidationResult struct {
	VersionID uint                        `json:"version_id"`
	Passed    bool                        `json:"passed"`
	Findings  []DocumentValidationFinding `json:"findings"`
}

type DocumentReviewCommentView struct {
	ID          uint       `json:"id"`
	TaskID      uint       `json:"task_id"`
	VersionID   uint       `json:"version_id"`
	CreatedBy   uint       `json:"created_by"`
	ParentID    uint       `json:"parent_id,omitempty"`
	AnchorStart int        `json:"anchor_start,omitempty"`
	AnchorEnd   int        `json:"anchor_end,omitempty"`
	Quote       string     `json:"quote,omitempty"`
	Content     string     `json:"content"`
	Status      string     `json:"status"`
	ResolvedBy  uint       `json:"resolved_by,omitempty"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type WritingTaskView struct {
	ID               uint                  `json:"id"`
	OrganizationID   uint                  `json:"organization_id"`
	TemplateID       uint                  `json:"template_id"`
	Title            string                `json:"title"`
	Requirement      string                `json:"requirement"`
	Constraints      []string              `json:"constraints"`
	Status           string                `json:"status"`
	CurrentVersionID uint                  `json:"current_version_id"`
	CreatedAt        time.Time             `json:"created_at"`
	UpdatedAt        time.Time             `json:"updated_at"`
	Versions         []DocumentVersionView `json:"versions,omitempty"`
}

type WritingRunMessageView struct {
	ID        uint      `json:"id"`
	Round     int       `json:"round"`
	Role      string    `json:"role"`
	ToolName  string    `json:"tool_name,omitempty"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type WritingRunTraceView struct {
	ID            uint      `json:"id"`
	Round         int       `json:"round"`
	ToolName      string    `json:"tool_name"`
	Kind          string    `json:"kind"`
	Input         string    `json:"input,omitempty"`
	OutputSummary string    `json:"output_summary,omitempty"`
	Status        string    `json:"status"`
	Error         string    `json:"error,omitempty"`
	ElapsedMS     int64     `json:"elapsed_ms"`
	CreatedAt     time.Time `json:"created_at"`
}

// WritingRunView makes a long-running MCP workflow observable and resumable.
// It returns compact conversation and tool ledgers, never hidden reasoning or
// model credentials.
type WritingRunView struct {
	ID            uint                    `json:"id"`
	TaskID        uint                    `json:"task_id"`
	Stage         string                  `json:"stage"`
	EvidenceQuery string                  `json:"evidence_query"`
	EvidenceLimit int                     `json:"evidence_limit"`
	Status        string                  `json:"status"`
	CurrentStep   string                  `json:"current_step"`
	FailureReason string                  `json:"failure_reason,omitempty"`
	ModelName     string                  `json:"model_name,omitempty"`
	VersionID     uint                    `json:"version_id,omitempty"`
	ResumeCount   int                     `json:"resume_count"`
	StartedAt     *time.Time              `json:"started_at,omitempty"`
	PausedAt      *time.Time              `json:"paused_at,omitempty"`
	CompletedAt   *time.Time              `json:"completed_at,omitempty"`
	CreatedAt     time.Time               `json:"created_at"`
	UpdatedAt     time.Time               `json:"updated_at"`
	Messages      []WritingRunMessageView `json:"messages,omitempty"`
	Traces        []WritingRunTraceView   `json:"traces,omitempty"`
	Evidence      []KnowledgeEvidence     `json:"evidence,omitempty"`
}
