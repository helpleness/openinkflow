package system

import (
	"time"

	"gorm.io/gorm"
)

// SysAuthChallenge persists only hashes of CAPTCHA identifiers and answers so
// browser verification works across restarts and multiple server instances.
type SysAuthChallenge struct {
	gorm.Model
	ChallengeHash string     `json:"-" gorm:"size:64;not null;uniqueIndex:idx_sys_auth_challenges_hash"`
	AnswerHash    string     `json:"-" gorm:"size:64;not null"`
	Purpose       string     `json:"-" gorm:"size:32;not null;default:login"`
	ExpiresAt     time.Time  `json:"-" gorm:"not null;index"`
	UsedAt        *time.Time `json:"-" gorm:"index"`
}

func (SysAuthChallenge) TableName() string { return "sys_auth_challenges" }
