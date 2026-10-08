package response

import "time"

type AIChatConversationView struct {
	ID             uint      `json:"id"`
	OrganizationID uint      `json:"organization_id"`
	Title          string    `json:"title"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type AIChatMessageView struct {
	ID        uint      `json:"id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	ToolName  string    `json:"tool_name,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type AIChatConversationDetail struct {
	Conversation AIChatConversationView `json:"conversation"`
	Messages     []AIChatMessageView    `json:"messages"`
}

type AIChatSendResult struct {
	UserMessage      AIChatMessageView   `json:"user_message"`
	SkillMessage     *AIChatMessageView  `json:"skill_message,omitempty"`
	AssistantMessage AIChatMessageView   `json:"assistant_message"`
	ToolMessages     []AIChatMessageView `json:"tool_messages"`
	Warnings         []string            `json:"warnings"`
}

// AIChatStreamDelta is a single visible assistant text increment sent over
// the chat SSE endpoint.
type AIChatStreamDelta struct {
	Content string `json:"content"`
}

type AISkillView struct {
	ID           uint      `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Instructions string    `json:"instructions"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
