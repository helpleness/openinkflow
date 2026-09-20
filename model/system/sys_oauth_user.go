package system

import (
	"time"

	"gorm.io/gorm"
)

// SysOAuthUser maps a SysUser to one external OAuth2 identity. Authentication
// remains owned by the provider, while application authorization, sessions and
// user state always use the normal SysUser record.
type SysOAuthUser struct {
	gorm.Model
	UserID      uint       `json:"user_id" gorm:"not null;index"`
	Provider    string     `json:"provider" gorm:"size:64;not null;uniqueIndex:idx_sys_oauth_users_provider_uid"`
	ProviderUID string     `json:"provider_uid" gorm:"size:191;not null;uniqueIndex:idx_sys_oauth_users_provider_uid"`
	Name        string     `json:"name" gorm:"size:191"`
	Email       string     `json:"email" gorm:"size:191;index"`
	AvatarURL   string     `json:"avatar_url" gorm:"size:512"`
	LastLoginAt *time.Time `json:"last_login_at"`
}

// TableName returns the table name used for linked OAuth identities.
func (SysOAuthUser) TableName() string { return "sys_oauth_users" }
