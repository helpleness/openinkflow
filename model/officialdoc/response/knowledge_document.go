package response

import "time"

type KnowledgeDocumentView struct {
	ID             uint       `json:"id"`
	OrganizationID uint       `json:"organization_id"`
	Name           string     `json:"name"`
	OriginalName   string     `json:"original_name"`
	ContentType    string     `json:"content_type"`
	ChunkCount     int        `json:"chunk_count"`
	Status         string     `json:"status"`
	FailureReason  string     `json:"failure_reason,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	IndexedAt      *time.Time `json:"indexed_at,omitempty"`
}

// KnowledgeEvidence is a source chunk returned by search and stored by generation.
type KnowledgeEvidence struct {
	DocumentID   uint    `json:"document_id"`
	DocumentName string  `json:"document_name"`
	ChunkID      uint    `json:"chunk_id"`
	ChunkIndex   int     `json:"chunk_index"`
	Title        string  `json:"title"`
	ParentTitle  string  `json:"parent_title"`
	Content      string  `json:"content"`
	Score        float64 `json:"score"`
	VectorRank   int     `json:"vector_rank,omitempty"`
	LexicalRank  int     `json:"lexical_rank,omitempty"`
}

type KnowledgeSearchResult struct {
	Items    []KnowledgeEvidence `json:"items"`
	Warnings []string            `json:"warnings,omitempty"`
}

type KnowledgeChunkView struct {
	ID          uint   `json:"id"`
	ChunkIndex  int    `json:"chunk_index"`
	Title       string `json:"title"`
	ParentTitle string `json:"parent_title"`
	Content     string `json:"content"`
	Metadata    string `json:"metadata"`
}

type KnowledgeDocumentDetail struct {
	Document KnowledgeDocumentView `json:"document"`
	Chunks   []KnowledgeChunkView  `json:"chunks"`
}

// KnowledgeDocumentDownload contains only a temporary signed address. Object
// keys and storage credentials remain server-side.
type KnowledgeDocumentDownload struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}
