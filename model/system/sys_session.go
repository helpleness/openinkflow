package system

import (
	"time"

	"gorm.io/gorm"
)

// SysSession 只保存随机会话凭据的哈希，原始凭据仅返回给当前客户端。
type SysSession struct {
	gorm.Model
	UserID     uint      `json:"user_id" gorm:"not null;index"`
	TokenHash  string    `json:"-" gorm:"size:64;not null;uniqueIndex:idx_system_sessions_token_hash"`
	ExpiresAt  time.Time `json:"expires_at" gorm:"not null;index"`
	Kind       string    `json:"kind" gorm:"size:32;not null;default:local"`
	LastSeenAt time.Time `json:"last_seen_at" gorm:"not null;default:CURRENT_TIMESTAMP;index"`
	ClientIP   string    `json:"client_ip" gorm:"size:64"`
	UserAgent  string    `json:"user_agent" gorm:"size:512"`
	DeviceName string    `json:"device_name" gorm:"size:255"`
}

// TableName 返回会话模型对应的数据表名。
func (SysSession) TableName() string { return "sys_sessions" }
