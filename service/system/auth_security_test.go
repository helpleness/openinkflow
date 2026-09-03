package system

import (
	"context"
	"testing"
	"time"

	"InkFlow/global"
	model "InkFlow/model/system"

	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func TestCaptchaIsOneTimeAndRateLimited(t *testing.T) {
	db := setupAuthSecurityDB(t, "captcha")
	now := time.Now().Add(time.Minute)
	challenge := model.SysAuthChallenge{
		ChallengeHash: authDigest("challenge-1"),
		AnswerHash:    authDigest("ABC234"),
		ExpiresAt:     now,
	}
	if err := db.Create(&challenge).Error; err != nil {
		t.Fatal(err)
	}
	service := ServiceGroupApp.SysAuthService
	if err := service.VerifyCaptcha(context.Background(), "challenge-1", "abc234"); err != nil {
		t.Fatalf("VerifyCaptcha() error = %v", err)
	}
	if err := service.VerifyCaptcha(context.Background(), "challenge-1", "ABC234"); err == nil {
		t.Fatal("used CAPTCHA unexpectedly remained valid")
	}
	global.GVA_CONFIG.Auth.LoginIPLimit = 2
	global.GVA_CONFIG.Auth.LoginIPWindowSeconds = 60
	if !service.AllowLoginAttempt("198.51.100.1") || !service.AllowLoginAttempt("198.51.100.1") || service.AllowLoginAttempt("198.51.100.1") {
		t.Fatal("login IP rate limit did not reject the third request")
	}
}

func TestMFAAndPasswordPolicy(t *testing.T) {
	db := setupAuthSecurityDB(t, "mfa")
	if err := validateLocalPassword("writer", "short"); err == nil {
		t.Fatal("weak password unexpectedly passed validation")
	}
	if err := validateLocalPassword("writer", "Abcdef1!"); err != nil {
		t.Fatalf("valid eight-character password rejected: %v", err)
	}
	if err := validateLocalPassword("writer", "CorrectHorse-Battery9"); err != nil {
		t.Fatalf("strong password rejected: %v", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("CorrectHorse-Battery9"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	user := model.SysUser{Username: "mfa-writer", LocalPasswordHash: string(hash), Status: model.UserStatusActive}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	service := ServiceGroupApp.SysAuthService
	setup, err := service.SetupMFA(context.Background(), user.ID)
	if err != nil || setup.Secret == "" {
		t.Fatalf("SetupMFA() = %#v, %v", setup, err)
	}
	if err := service.EnableMFA(context.Background(), user.ID, totpCode(setup.Secret, time.Now())); err != nil {
		t.Fatalf("EnableMFA() error = %v", err)
	}
	pending, err := service.BeginLocalLogin(context.Background(), user.Username, "CorrectHorse-Battery9")
	if err != nil {
		t.Fatalf("BeginLocalLogin() error = %v", err)
	}
	if pending.AuthStage != authStageMFAVerify || pending.PendingToken == "" || pending.SessionToken != "" {
		t.Fatalf("expected a pending MFA step, got %#v", pending)
	}
	var sessions int64
	if err := db.Model(&model.SysSession{}).Where("user_id = ?", user.ID).Count(&sessions).Error; err != nil || sessions != 0 {
		t.Fatalf("session created before MFA completion: %d, %v", sessions, err)
	}
	login, err := service.CompletePendingMFA(context.Background(), pending.PendingToken, totpCode(setup.Secret, time.Now()))
	if err != nil || login.SessionToken == "" {
		t.Fatalf("MFA login = %#v, %v", login, err)
	}
}

func TestPerUserMFARequirementEnrolsAfterPasswordStep(t *testing.T) {
	db := setupAuthSecurityDB(t, "mfa-enrollment")
	hash, err := bcrypt.GenerateFromPassword([]byte("CorrectHorse-Battery9"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	user := model.SysUser{Username: "enroll-writer", LocalPasswordHash: string(hash), Status: model.UserStatusActive, MFA: model.SysUserMFA{EnrollmentRequired: true}}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	service := ServiceGroupApp.SysAuthService
	pending, err := service.BeginLocalLogin(context.Background(), user.Username, "CorrectHorse-Battery9")
	if err != nil || pending.AuthStage != authStageMFAEnrollment || pending.PendingToken == "" {
		t.Fatalf("enrollment step = %#v, %v", pending, err)
	}
	setup, err := service.SetupPendingMFA(context.Background(), pending.PendingToken)
	if err != nil || setup.Secret == "" {
		t.Fatalf("SetupPendingMFA() = %#v, %v", setup, err)
	}
	login, err := service.CompletePendingMFA(context.Background(), pending.PendingToken, totpCode(setup.Secret, time.Now()))
	if err != nil || login.SessionToken == "" {
		t.Fatalf("CompletePendingMFA() = %#v, %v", login, err)
	}
	var persisted model.SysUser
	if err := db.First(&persisted, user.ID).Error; err != nil || !persisted.MFA.Enabled {
		t.Fatalf("MFA enrollment was not persisted: %#v, %v", persisted, err)
	}
}

func TestFailedPasswordLocksAccountAndSessionsAreScoped(t *testing.T) {
	db := setupAuthSecurityDB(t, "lock")
	global.GVA_CONFIG.Auth.LoginMaxFailures = 2
	global.GVA_CONFIG.Auth.LoginLockMinutes = 1
	hash, err := bcrypt.GenerateFromPassword([]byte("CorrectHorse-Battery9"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	user := model.SysUser{Username: "locked-writer", LocalPasswordHash: string(hash), Status: model.UserStatusActive}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	service := ServiceGroupApp.SysAuthService
	for range 2 {
		if _, err := service.LoginLocalWithMFA(context.Background(), user.Username, "wrong-password", ""); err == nil {
			t.Fatal("wrong password unexpectedly authenticated")
		}
	}
	if _, err := service.LoginLocalWithMFA(context.Background(), user.Username, "CorrectHorse-Battery9", ""); err == nil {
		t.Fatal("locked account unexpectedly authenticated")
	}
	var persisted model.SysUser
	if err := db.First(&persisted, user.ID).Error; err != nil || persisted.LockedUntil == nil {
		t.Fatalf("lock state = %#v, %v", persisted, err)
	}
}

func setupAuthSecurityDB(t *testing.T, name string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:auth-security-"+name+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.SysUser{}, &model.SysSession{}, &model.SysAuthChallenge{}); err != nil {
		t.Fatal(err)
	}
	previousDB, previousConfig := global.GVA_DB, global.GVA_CONFIG
	global.GVA_DB = db
	global.GVA_CONFIG.Auth.JWTSecret = "test-auth-secret-with-sufficient-entropy"
	global.GVA_CONFIG.Auth.SessionTTLHours = 24
	global.GVA_CONFIG.Auth.LoginMaxFailures = 5
	global.GVA_CONFIG.Auth.LoginLockMinutes = 15
	global.GVA_CONFIG.Auth.LoginIPLimit = 30
	global.GVA_CONFIG.Auth.LoginIPWindowSeconds = 600
	global.GVA_CONFIG.Auth.MFAEnrollmentRequired = false
	t.Cleanup(func() {
		global.GVA_DB = previousDB
		global.GVA_CONFIG = previousConfig
	})
	return db
}
