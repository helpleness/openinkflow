package officialdoc

import (
	"time"

	"gorm.io/gorm"
)

// WritingRun persists one MCP-orchestrated controlled-writing execution. A run
// is separate from WritingTask so the task can retain many interrupted and
// completed attempts without mutating the immutable document versions.
type WritingRun struct {
	gorm.Model
	TaskID         uint       `json:"task_id" gorm:"not null;index"`
	TenantID       uint       `json:"tenant_id" gorm:"not null;index"`
	OrganizationID uint       `json:"organization_id" gorm:"not null;index"`
	StartedBy      uint       `json:"started_by" gorm:"not null;index"`
	Stage          string     `json:"stage" gorm:"size:32;not null;index"`
	EvidenceQuery  string     `json:"evidence_query" gorm:"type:text"`
	EvidenceLimit  int        `json:"evidence_limit" gorm:"not null;default:6"`
	Status         string     `json:"status" gorm:"size:32;not null;default:queued;index"`
	CurrentStep    string     `json:"current_step" gorm:"size:64;not null;default:retrieve_evidence"`
	FailureReason  string     `json:"failure_reason" gorm:"type:text"`
	GeneratedBody  string     `json:"-" gorm:"type:text"`
	ModelName      string     `json:"model_name" gorm:"size:255"`
	VersionID      uint       `json:"version_id" gorm:"index"`
	ResumeCount    int        `json:"resume_count" gorm:"not null;default:0"`
	StartedAt      *time.Time `json:"started_at"`
	PausedAt       *time.Time `json:"paused_at"`
	CompletedAt    *time.Time `json:"completed_at"`
}

func (WritingRun) TableName() string { return "writing_runs" }

// WritingRunMessage persists the compact multi-round dialogue ledger. It is an
// audit trail for what the user requested, which MCP step was requested, and
// the result returned by that step; it never stores hidden model reasoning.
type WritingRunMessage struct {
	gorm.Model
	RunID    uint   `json:"run_id" gorm:"not null;index"`
	Round    int    `json:"round" gorm:"not null"`
	Role     string `json:"role" gorm:"size:32;not null;index"`
	ToolName string `json:"tool_name" gorm:"size:128;index"`
	Content  string `json:"content" gorm:"type:text;not null"`
}

func (WritingRunMessage) TableName() string { return "writing_run_messages" }

// WritingRunToolTrace is the durable counterpart of toolchain.Trace. Its
// fields make a paused or failed run diagnosable without retaining secret model
// credentials or mutable in-memory state.
type WritingRunToolTrace struct {
	gorm.Model
	RunID         uint   `json:"run_id" gorm:"not null;index"`
	Round         int    `json:"round" gorm:"not null"`
	ToolName      string `json:"tool_name" gorm:"size:128;not null;index"`
	Kind          string `json:"kind" gorm:"size:32;not null"`
	Input         string `json:"input" gorm:"type:text"`
	OutputSummary string `json:"output_summary" gorm:"type:text"`
	Status        string `json:"status" gorm:"size:32;not null;index"`
	Error         string `json:"error" gorm:"type:text"`
	ElapsedMS     int64  `json:"elapsed_ms" gorm:"not null;default:0"`
}

func (WritingRunToolTrace) TableName() string { return "writing_run_tool_traces" }

// WritingRunEvidence stores the retrieval snapshot before a version is
// committed. It lets a paused run resume without re-querying a changed source.
type WritingRunEvidence struct {
	gorm.Model
	RunID           uint    `json:"run_id" gorm:"not null;index"`
	DocumentID      uint    `json:"document_id" gorm:"not null;index"`
	ChunkID         uint    `json:"chunk_id" gorm:"not null;index"`
	Rank            int     `json:"rank" gorm:"not null"`
	Score           float64 `json:"score" gorm:"not null"`
	DocumentName    string  `json:"document_name" gorm:"size:255"`
	ChunkTitle      string  `json:"chunk_title" gorm:"size:512"`
	ContentSnapshot string  `json:"content_snapshot" gorm:"type:text;not null"`
}

func (WritingRunEvidence) TableName() string { return "writing_run_evidences" }
