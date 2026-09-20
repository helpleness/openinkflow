package initialize

import (
	"path/filepath"
	"testing"

	system "InkFlow/model/system"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMigrateLegacyMFAColumns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "legacy-mfa.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	createSQL := `CREATE TABLE sys_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME,
		username VARCHAR(64) NOT NULL,
		mfa_secret_encrypted TEXT,
		mfa_required BOOLEAN NOT NULL DEFAULT FALSE,
		mfa_enabled BOOLEAN NOT NULL DEFAULT FALSE,
		mfa_pending_token_hash VARCHAR(64),
		mfa_pending_stage VARCHAR(32),
		mfa_pending_expires_at DATETIME
	)`
	if err := db.Exec(createSQL).Error; err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	insertSQL := `INSERT INTO sys_users (
		username, mfa_secret_encrypted, mfa_required, mfa_enabled,
		mfa_pending_token_hash, mfa_pending_stage, mfa_pending_expires_at
	) VALUES ('legacy', 'secret', TRUE, FALSE, 'hash', 'mfa_enrollment', CURRENT_TIMESTAMP)`
	if err := db.Exec(insertSQL).Error; err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}

	if err := migrateLegacyMFAColumns(db); err != nil {
		t.Fatalf("migrate MFA columns: %v", err)
	}

	var user system.SysUser
	if err := db.Where("username = ?", "legacy").First(&user).Error; err != nil {
		t.Fatalf("load migrated row: %v", err)
	}
	if user.MFA.TOTPSecretCiphertext != "secret" || !user.MFA.EnrollmentRequired || user.MFA.Enabled {
		t.Fatalf("MFA data was not preserved: %#v", user.MFA)
	}
	if user.MFA.LoginStepTokenHash != "hash" || user.MFA.LoginStepKind != "mfa_enrollment" || user.MFA.LoginStepExpiresAt == nil {
		t.Fatalf("login-step data was not preserved: %#v", user.MFA)
	}
}
