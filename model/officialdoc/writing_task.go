package officialdoc

import (
	"time"

	"gorm.io/gorm"
)

// WritingTask is the controlled generation aggregate. It owns a stable history
// of outlines, drafts and their exact evidence snapshots.
type WritingTask struct {
	gorm.Model
	TenantID         uint   `json:"tenant_id" gorm:"not null;index"`
	OrganizationID   uint   `json:"organization_id" gorm:"not null;index"`
	TemplateID       uint   `json:"template_id" gorm:"not null;index"`
	CreatedBy        uint   `json:"created_by" gorm:"not null;index"`
	Title            string `json:"title" gorm:"size:255;not null"`
	Requirement      string `json:"requirement" gorm:"type:text;not null"`
	Constraints      string `json:"constraints" gorm:"type:text"`
	Status           string `json:"status" gorm:"size:32;not null;default:draft;index"`
	CurrentVersionID uint   `json:"current_version_id" gorm:"index"`
}

func (WritingTask) TableName() string { return "writing_tasks" }

// DocumentVersion is an immutable outline, draft or manually saved revision.
type DocumentVersion struct {
	gorm.Model
	TaskID         uint   `json:"task_id" gorm:"not null;index;uniqueIndex:idx_document_versions_task_number,priority:1"`
	TenantID       uint   `json:"tenant_id" gorm:"not null;index"`
	OrganizationID uint   `json:"organization_id" gorm:"not null;index"`
	CreatedBy      uint   `json:"created_by" gorm:"not null;index"`
	Version        int    `json:"version" gorm:"not null;uniqueIndex:idx_document_versions_task_number,priority:2"`
	Stage          string `json:"stage" gorm:"size:32;not null;index"`
	Content        string `json:"content" gorm:"type:text;not null"`
	Prompt         string `json:"prompt" gorm:"type:text"`
	ModelName      string `json:"model" gorm:"column:model_name;size:255"`
}

func (DocumentVersion) TableName() string { return "document_versions" }

// DocumentReviewComment is an auditable, version-bound review note. A comment
// is never attached to a mutable editor buffer, so later versions cannot alter
// the reviewer record it was made against.
type DocumentReviewComment struct {
	gorm.Model
	TaskID         uint       `json:"task_id" gorm:"not null;index"`
	VersionID      uint       `json:"version_id" gorm:"not null;index"`
	TenantID       uint       `json:"tenant_id" gorm:"not null;index"`
	OrganizationID uint       `json:"organization_id" gorm:"not null;index"`
	CreatedBy      uint       `json:"created_by" gorm:"not null;index"`
	ParentID       uint       `json:"parent_id,omitempty" gorm:"index"`
	AnchorStart    int        `json:"anchor_start,omitempty"`
	AnchorEnd      int        `json:"anchor_end,omitempty"`
	Quote          string     `json:"quote,omitempty" gorm:"type:text"`
	Content        string     `json:"content" gorm:"type:text;not null"`
	Status         string     `json:"status" gorm:"size:16;not null;default:open;index"`
	ResolvedBy     uint       `json:"resolved_by,omitempty" gorm:"index"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
}

func (DocumentReviewComment) TableName() string { return "document_review_comments" }

// WritingEvidence persists the chunk snapshot used for a specific generated version.
// Keeping the text snapshot makes historical versions auditable after source updates/deletion.
type WritingEvidence struct {
	gorm.Model
	TaskID          uint    `json:"task_id" gorm:"not null;index"`
	VersionID       uint    `json:"version_id" gorm:"not null;index"`
	DocumentID      uint    `json:"document_id" gorm:"not null;index"`
	ChunkID         uint    `json:"chunk_id" gorm:"not null;index"`
	Rank            int     `json:"rank" gorm:"not null"`
	Score           float64 `json:"score" gorm:"not null"`
	DocumentName    string  `json:"document_name" gorm:"size:255"`
	ChunkTitle      string  `json:"chunk_title" gorm:"size:512"`
	ContentSnapshot string  `json:"content_snapshot" gorm:"type:text;not null"`
}

func (WritingEvidence) TableName() string { return "writing_evidences" }
