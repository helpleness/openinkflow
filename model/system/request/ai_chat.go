package request

// AIChatConversationCreate starts a user-scoped chat in the selected organization.
type AIChatConversationCreate struct {
	OrganizationID uint   `json:"organization_id" binding:"required"`
	Title          string `json:"title"`
}

// AIChatSendMessage sends one natural-language message to a conversation.
type AIChatSendMessage struct {
	Content     string             `json:"content"`
	Attachments []AIChatAttachment `json:"attachments"`
	// UseSkills defaults to true. It is deliberately a per-turn option so a
	// user can exclude all enabled prompt Skills without changing their saved
	// Skill configuration.
	UseSkills *bool `json:"use_skills"`
	// SkillIDs distinguishes automatic matching (nil) from an explicit manual
	// selection (including an empty JSON array). Only enabled Skills owned by
	// the current user may be selected.
	SkillIDs *[]uint `json:"skill_ids"`
}

// AIChatAttachment carries a small, inline user attachment for one chat turn.
// The server only accepts image and plain-text formats; binary office files and
// audio/video are deliberately rejected instead of being silently ignored.
type AIChatAttachment struct {
	Name       string `json:"name"`
	MediaType  string `json:"media_type"`
	DataBase64 string `json:"data_base64"`
}

type AISkillCreate struct {
	Name         string `json:"name" binding:"required"`
	Description  string `json:"description"`
	Instructions string `json:"instructions" binding:"required"`
	Enabled      *bool  `json:"enabled"`
}

type AISkillUpdate struct {
	Name         *string `json:"name"`
	Description  *string `json:"description"`
	Instructions *string `json:"instructions"`
	Enabled      *bool   `json:"enabled"`
}
