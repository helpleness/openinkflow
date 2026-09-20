package system

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestSysUserMFAEmbeddedFields(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "sys_users.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := db.AutoMigrate(&SysUser{}); err != nil {
		t.Fatalf("migrate sys_users: %v", err)
	}

	expiresAt := time.Now().Add(10 * time.Minute)
	user := SysUser{
		Username: "mfa-fields",
		Status:   UserStatusActive,
		MFA: SysUserMFA{
			TOTPSecretCiphertext: "ciphertext",
			EnrollmentRequired:   true,
			Enabled:              false,
			LoginStepTokenHash:   "token-hash",
			LoginStepKind:        "mfa_enrollment",
			LoginStepExpiresAt:   &expiresAt,
		},
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create sys_user: %v", err)
	}

	var persisted SysUser
	if err := db.First(&persisted, user.ID).Error; err != nil {
		t.Fatalf("load sys_user: %v", err)
	}
	if persisted.MFA.TOTPSecretCiphertext != "ciphertext" || !persisted.MFA.EnrollmentRequired || persisted.MFA.Enabled {
		t.Fatalf("unexpected MFA state: %#v", persisted.MFA)
	}
	if persisted.MFA.LoginStepTokenHash != "token-hash" || persisted.MFA.LoginStepKind != "mfa_enrollment" || persisted.MFA.LoginStepExpiresAt == nil {
		t.Fatalf("unexpected login step state: %#v", persisted.MFA)
	}
}
