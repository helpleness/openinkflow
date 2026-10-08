package system

import (
	"time"

	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
)

// SysAIChatConversation is one user's conversation inside an organization.
// Conversations are deliberately user-scoped: a chat can use the user's model
// credentials, enabled MCP connections, and enabled prompt skills without
// exposing either the messages or configuration to other members.
type SysAIChatConversation struct {
	gorm.Model
	TenantID       uint   `json:"tenant_id" gorm:"not null;index"`
	OrganizationID uint   `json:"organization_id" gorm:"not null;index"`
	UserID         uint   `json:"user_id" gorm:"not null;index"`
	Title          string `json:"title" gorm:"size:255;not null"`
}

func (SysAIChatConversation) TableName() string { return "sys_ai_chat_conversations" }

// SysAIChatMessage persists the human-readable chat transcript. Tool result
// rows are kept separately from the prompt history so an old MCP call never
// becomes an unbound tool response in a later model request.
type SysAIChatMessage struct {
	gorm.Model
	TenantID       uint   `json:"tenant_id" gorm:"not null;index"`
	ConversationID uint   `json:"conversation_id" gorm:"not null;index"`
	Role           string `json:"role" gorm:"size:16;not null;index"`
	Content        string `json:"content" gorm:"type:text;not null"`
	ToolName       string `json:"tool_name" gorm:"size:255"`
}

func (SysAIChatMessage) TableName() string { return "sys_ai_chat_messages" }

// SysAIChatTurnMemory is one completed user-question/assistant-answer pair.
// It is deliberately separate from the chat transcript: the transcript is
// retained for display, while this table is the retrievable unit used to recall
// older conversation context without separating an answer from its question.
type SysAIChatTurnMemory struct {
	gorm.Model
	TenantID           uint             `json:"tenant_id" gorm:"not null;index"`
	ConversationID     uint             `json:"conversation_id" gorm:"not null;index"`
	UserMessageID      uint             `json:"user_message_id" gorm:"not null;uniqueIndex:idx_ai_chat_turn_memories_user_message"`
	AssistantMessageID uint             `json:"assistant_message_id" gorm:"not null;uniqueIndex:idx_ai_chat_turn_memories_assistant_message"`
	Question           string           `json:"question" gorm:"type:text;not null"`
	Answer             string           `json:"answer" gorm:"type:text;not null"`
	Embedding          *pgvector.Vector `json:"-" gorm:"type:vector"`
	IndexedAt          *time.Time       `json:"indexed_at"`
}

func (SysAIChatTurnMemory) TableName() string { return "sys_ai_chat_turn_memories" }

// SysAISkill is a user-authored, prompt-only capability. Unlike MCP tools a
// skill cannot execute code or access a network endpoint. Enabled means it is
// eligible for a turn's automatic match or manual selection; only the Skills
// selected for that turn are included in the system prompt.
type SysAISkill struct {
	gorm.Model
	TenantID     uint   `json:"tenant_id" gorm:"not null;uniqueIndex:idx_sys_ai_skills_owner_name,priority:1"`
	UserID       uint   `json:"user_id" gorm:"not null;uniqueIndex:idx_sys_ai_skills_owner_name,priority:2"`
	Name         string `json:"name" gorm:"size:128;not null;uniqueIndex:idx_sys_ai_skills_owner_name,priority:3"`
	Description  string `json:"description" gorm:"size:512"`
	Instructions string `json:"instructions" gorm:"type:text;not null"`
	Enabled      bool   `json:"enabled" gorm:"not null;default:true;index"`
}

func (SysAISkill) TableName() string { return "sys_ai_skills" }
