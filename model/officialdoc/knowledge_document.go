package officialdoc

import (
	"time"

	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
)

// KnowledgeDocument is one source file imported into an organization knowledge base.
type KnowledgeDocument struct {
	gorm.Model
	TenantID       uint   `json:"tenant_id" gorm:"not null;index;uniqueIndex:idx_knowledge_documents_scope_sha256,priority:1"`
	OrganizationID uint   `json:"organization_id" gorm:"not null;index;uniqueIndex:idx_knowledge_documents_scope_sha256,priority:2"`
	CreatedBy      uint   `json:"created_by" gorm:"not null;index"`
	Name           string `json:"name" gorm:"size:255;not null"`
	OriginalName   string `json:"original_name" gorm:"size:255;not null"`
	ContentType    string `json:"content_type" gorm:"size:128"`
	ObjectKey      string `json:"-" gorm:"type:text;not null;default:''"`
	// SHA256 identifies identical source content within one tenant and organization.
	SHA256        string     `json:"sha256" gorm:"size:64;not null;index;uniqueIndex:idx_knowledge_documents_scope_sha256,priority:3"`
	ChunkCount    int        `json:"chunk_count" gorm:"not null;default:0"`
	Status        string     `json:"status" gorm:"size:32;not null;default:indexing;index"`
	FailureReason string     `json:"failure_reason" gorm:"type:text"`
	IndexedAt     *time.Time `json:"indexed_at"`
}

func (KnowledgeDocument) TableName() string { return "knowledge_documents" }

// KnowledgeChunk stores one chunker output. The relational record remains the source of truth
// for text and access scope; the optional Embedding column is used directly by pgvector.
type KnowledgeChunk struct {
	gorm.Model
	DocumentID     uint             `json:"document_id" gorm:"not null;index"`
	TenantID       uint             `json:"tenant_id" gorm:"not null;index"`
	OrganizationID uint             `json:"organization_id" gorm:"not null;index"`
	ChunkIndex     int              `json:"chunk_index" gorm:"not null"`
	Title          string           `json:"title" gorm:"size:512"`
	ParentTitle    string           `json:"parent_title" gorm:"size:512"`
	Content        string           `json:"content" gorm:"type:text;not null"`
	Metadata       string           `json:"metadata" gorm:"type:text"`
	Embedding      *pgvector.Vector `json:"-" gorm:"type:vector"`
	IndexedAt      *time.Time       `json:"indexed_at"`
}

func (KnowledgeChunk) TableName() string { return "knowledge_chunks" }

type KnowledgeImage struct {
	gorm.Model
	DocumentID uint   `json:"document_id" gorm:"not null;index"`
	Name       string `json:"name" gorm:"size:255;not null"`
	MIME       string `json:"mime" gorm:"size:128"`
	ObjectKey  string `json:"-" gorm:"type:text;not null;default:''"`
	// SHA256 lets the importer deduplicate identical embedded images in one document.
	SHA256        string `json:"sha256" gorm:"size:64;not null;index"`
	Kind          string `json:"kind" gorm:"size:32;not null;default:unknown"`
	ExtractedText string `json:"extracted_text" gorm:"type:text"`
	Semantic      string `json:"semantic" gorm:"type:text"`
}

func (KnowledgeImage) TableName() string { return "knowledge_images" }
