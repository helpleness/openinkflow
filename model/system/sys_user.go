package system

import (
	"time"

	"gorm.io/gorm"
)

// SysUserMFA groups the durable state for time-based one-time password (TOTP)
// multi-factor authentication. Keeping it embedded makes ownership obvious and
// prevents generic flags such as "pending" from being mistaken for sessions.
type SysUserMFA struct {
	// TOTPSecretCiphertext stores the encrypted authenticator secret. An empty
	// value means that no authenticator has been bound (or enrollment has not
	// been started).
	TOTPSecretCiphertext string `json:"-" gorm:"type:text"`
	// EnrollmentRequired is an administrator/policy flag: the account must bind
	// and verify TOTP at its next password login. It does not mean TOTP is
	// usable yet.
	EnrollmentRequired bool `json:"mfa_enrollment_required" gorm:"not null;default:false"`
	// Enabled becomes true only after a TOTP code has been verified with the
	// bound authenticator. It is the state used during later logins.
	Enabled bool `json:"mfa_enabled" gorm:"not null;default:false"`
	// LoginStepTokenHash is a one-shot, short-lived challenge created after the
	// password/CAPTCHA step. It is not a session and cannot create one until
	// LoginStepKind has been completed.
	LoginStepTokenHash string `json:"-" gorm:"size:64"`
	// LoginStepKind identifies the next expected action: verify an already
	// enabled authenticator or finish initial enrollment.
	LoginStepKind string `json:"-" gorm:"size:32"`
	// LoginStepExpiresAt bounds the lifetime of the login step challenge.
	LoginStepExpiresAt *time.Time `json:"-" gorm:"index"`
}

// SysUser is a local identity independent of the concrete business domain.
// Remote OAuth/identity mappings do not store third-party passwords here.
type SysUser struct {
	gorm.Model
	Username          string     `json:"username" gorm:"size:64;not null;uniqueIndex:idx_system_users_username"`
	LocalPasswordHash string     `json:"-" gorm:"type:text"`
	RemoteUserID      uint       `json:"remote_user_id" gorm:"index"`
	AuthDomain        string     `json:"auth_domain" gorm:"size:255;index"`
	RemoteProfile     string     `json:"remote_profile,omitempty" gorm:"type:text"`
	Status            string     `json:"status" gorm:"size:32;not null;default:active;index"`
	FailedLoginCount  int        `json:"-" gorm:"not null;default:0"`
	LockedUntil       *time.Time `json:"-" gorm:"index"`
	PasswordChangedAt *time.Time `json:"-"`
	MFA               SysUserMFA `gorm:"embedded;embeddedPrefix:mfa_"`
}

// TableName returns the table name used by the system user model.
func (SysUser) TableName() string { return "sys_users" }
