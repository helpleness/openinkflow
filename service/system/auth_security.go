package system

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"html"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"InkFlow/global"
	model "InkFlow/model/system"
	response "InkFlow/model/system/response"
	"InkFlow/utils/securestore"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	errCaptchaInvalid  = fmt.Errorf("图片验证码无效或已过期")
	errMFACodeRequired = fmt.Errorf("请输入动态验证码完成多重验证")
)

const (
	authStageAuthenticated  = "authenticated"
	authStageMFAVerify      = "mfa_verify"
	authStageMFAEnrollment  = "mfa_enrollment"
	pendingMFATokenLifetime = 10 * time.Minute
)

type loginIPWindow struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
}

var authenticationAttempts = loginIPWindow{attempts: make(map[string][]time.Time)}

func (service *SysAuthService) NewCaptcha(ctx context.Context) (response.SysCaptcha, error) {
	id, err := randomAuthValue(24)
	if err != nil {
		return response.SysCaptcha{}, err
	}
	answer, err := randomCaptchaAnswer(6)
	if err != nil {
		return response.SysCaptcha{}, err
	}
	now := time.Now()
	expiresAt := now.Add(captchaTTL())
	challenge := model.SysAuthChallenge{ChallengeHash: authDigest(id), AnswerHash: authDigest(strings.ToUpper(answer)), ExpiresAt: expiresAt}
	db := global.GVA_DB.WithContext(ctx)
	if err := db.Create(&challenge).Error; err != nil {
		return response.SysCaptcha{}, err
	}
	// Best-effort cleanup keeps a public endpoint from accumulating short-lived
	// challenges. A failure here must not make the new challenge unusable.
	_ = db.Where("expires_at < ? OR used_at IS NOT NULL", now.Add(-time.Hour)).Delete(&model.SysAuthChallenge{}).Error
	return response.SysCaptcha{CaptchaID: id, ImageData: captchaImage(answer), ExpiresAt: expiresAt.UTC().Format(time.RFC3339)}, nil
}

func (service *SysAuthService) VerifyCaptcha(ctx context.Context, id, answer string) error {
	id, answer = strings.TrimSpace(id), strings.ToUpper(strings.TrimSpace(answer))
	if id == "" || answer == "" {
		return errCaptchaInvalid
	}
	now := time.Now()
	return global.GVA_DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var challenge model.SysAuthChallenge
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("challenge_hash = ? AND expires_at > ? AND used_at IS NULL", authDigest(id), now).First(&challenge).Error; err != nil {
			return errCaptchaInvalid
		}
		// A CAPTCHA is one-shot even when the answer is wrong, preventing online
		// brute force against a six-character challenge.
		if err := tx.Model(&challenge).Update("used_at", now).Error; err != nil {
			return err
		}
		if subtle.ConstantTimeCompare([]byte(challenge.AnswerHash), []byte(authDigest(answer))) != 1 {
			return errCaptchaInvalid
		}
		return nil
	})
}

func (service *SysAuthService) AllowLoginAttempt(clientIP string) bool {
	clientIP = strings.TrimSpace(clientIP)
	if clientIP == "" {
		clientIP = "unknown"
	}
	now := time.Now()
	window := loginIPWindowDuration()
	limit := loginIPLimit()
	authenticationAttempts.mu.Lock()
	defer authenticationAttempts.mu.Unlock()
	values := authenticationAttempts.attempts[clientIP]
	start := 0
	for start < len(values) && now.Sub(values[start]) > window {
		start++
	}
	values = append(values[start:], now)
	authenticationAttempts.attempts[clientIP] = values
	return len(values) <= limit
}

func (service *SysAuthService) SetupMFA(ctx context.Context, userID uint) (response.SysMFASetup, error) {
	var user model.SysUser
	if err := global.GVA_DB.WithContext(ctx).First(&user, userID).Error; err != nil {
		return response.SysMFASetup{}, err
	}
	return service.createMFASetup(ctx, &user)
}

func (service *SysAuthService) createMFASetup(ctx context.Context, user *model.SysUser) (response.SysMFASetup, error) {
	if user == nil || user.ID == 0 {
		return response.SysMFASetup{}, fmt.Errorf("用户不存在")
	}
	if user.MFA.Enabled {
		return response.SysMFASetup{}, fmt.Errorf("多重验证已启用；如需重新绑定，请先完成关闭验证")
	}
	secret, err := generateTOTPSecret()
	if err != nil {
		return response.SysMFASetup{}, err
	}
	encrypted, err := securestore.EncryptWithSecret(global.GVA_CONFIG.Auth.JWTSecret, []byte(secret))
	if err != nil {
		return response.SysMFASetup{}, err
	}
	if err := global.GVA_DB.WithContext(ctx).Model(user).Updates(map[string]any{"mfa_totp_secret_ciphertext": encrypted, "mfa_enabled": false}).Error; err != nil {
		return response.SysMFASetup{}, err
	}
	issuer := "InkFlow"
	uri := "otpauth://totp/" + url.PathEscape(issuer+":"+user.Username) + "?secret=" + url.QueryEscape(secret) + "&issuer=" + url.QueryEscape(issuer) + "&algorithm=SHA1&digits=6&period=30"
	return response.SysMFASetup{Secret: secret, OTPAuthURL: uri}, nil
}

func (service *SysAuthService) EnableMFA(ctx context.Context, userID uint, code string) error {
	var user model.SysUser
	if err := global.GVA_DB.WithContext(ctx).First(&user, userID).Error; err != nil {
		return err
	}
	if strings.TrimSpace(user.MFA.TOTPSecretCiphertext) == "" {
		return fmt.Errorf("请先创建多重验证密钥")
	}
	if err := verifyUserTOTP(user, code, time.Now()); err != nil {
		return errMFACodeRequired
	}
	return global.GVA_DB.WithContext(ctx).Model(&user).Update("mfa_enabled", true).Error
}

func (service *SysAuthService) DisableMFA(ctx context.Context, userID uint, password, code string) error {
	var user model.SysUser
	if err := global.GVA_DB.WithContext(ctx).First(&user, userID).Error; err != nil {
		return err
	}
	if !user.MFA.Enabled || user.LocalPasswordHash == "" || bcrypt.CompareHashAndPassword([]byte(user.LocalPasswordHash), []byte(password)) != nil || verifyUserTOTP(user, code, time.Now()) != nil {
		return fmt.Errorf("密码或动态验证码无效")
	}
	if err := global.GVA_DB.WithContext(ctx).Model(&user).Updates(map[string]any{"mfa_enabled": false, "mfa_totp_secret_ciphertext": ""}).Error; err != nil {
		return err
	}
	// A sensitive authentication-factor change invalidates every session. The
	// caller is prompted to sign in again, ensuring a stolen active cookie is
	// not silently kept after MFA is removed.
	return global.GVA_DB.WithContext(ctx).Where("user_id = ?", user.ID).Delete(&model.SysSession{}).Error
}

// BeginLocalLogin verifies the primary credentials only. If the account needs
// MFA, it returns a short-lived pending token rather than a usable session.
func (service *SysAuthService) BeginLocalLogin(ctx context.Context, username, password string) (*response.SysAuthResult, error) {
	db := global.GVA_DB
	var user model.SysUser
	if err := db.WithContext(ctx).Where("username = ?", strings.TrimSpace(username)).First(&user).Error; err != nil {
		return nil, fmt.Errorf("用户名或密码错误")
	}
	now := time.Now()
	if user.LockedUntil != nil && user.LockedUntil.After(now) {
		return nil, fmt.Errorf("账号已因连续登录失败锁定，请于 %s 后重试", user.LockedUntil.Local().Format("15:04"))
	}
	if user.Status != model.UserStatusActive || user.LocalPasswordHash == "" || bcrypt.CompareHashAndPassword([]byte(user.LocalPasswordHash), []byte(password)) != nil {
		service.recordFailedLogin(ctx, &user, now)
		return nil, fmt.Errorf("用户名或密码错误")
	}
	if err := db.WithContext(ctx).Model(&user).Updates(map[string]any{"failed_login_count": 0, "locked_until": nil}).Error; err != nil {
		return nil, err
	}
	return service.completePrimaryAuthentication(ctx, &user)
}

func (service *SysAuthService) completePrimaryAuthentication(ctx context.Context, user *model.SysUser) (*response.SysAuthResult, error) {
	if user == nil || user.ID == 0 {
		return nil, fmt.Errorf("用户不存在")
	}
	if user.MFA.Enabled {
		pendingToken, err := service.createPendingMFA(ctx, user.ID, authStageMFAVerify)
		if err != nil {
			return nil, err
		}
		return &response.SysAuthResult{AuthStage: authStageMFAVerify, PendingToken: pendingToken}, nil
	}
	// An account that has already started MFA enrollment must finish it on a
	// separate post-password screen as well. MFA can be required for this
	// account by its membership administrator, or for every account by config.
	if user.MFA.EnrollmentRequired || global.GVA_CONFIG.Auth.MFAEnrollmentRequired || strings.TrimSpace(user.MFA.TOTPSecretCiphertext) != "" {
		pendingToken, err := service.createPendingMFA(ctx, user.ID, authStageMFAEnrollment)
		if err != nil {
			return nil, err
		}
		return &response.SysAuthResult{AuthStage: authStageMFAEnrollment, PendingToken: pendingToken}, nil
	}
	token, err := service.createSession(ctx, user.ID, "local")
	if err != nil {
		return nil, err
	}
	return &response.SysAuthResult{User: user, SessionToken: token, AuthStage: authStageAuthenticated}, nil
}

// LoginLocalWithMFA remains available to non-HTTP callers during migration.
// The web API deliberately uses BeginLocalLogin and the pending MFA endpoints.
func (service *SysAuthService) LoginLocalWithMFA(ctx context.Context, username, password, code string) (*response.SysAuthResult, error) {
	result, err := service.BeginLocalLogin(ctx, username, password)
	if err != nil || result.AuthStage == authStageAuthenticated {
		return result, err
	}
	if result.AuthStage != authStageMFAVerify || strings.TrimSpace(code) == "" {
		return nil, errMFACodeRequired
	}
	return service.CompletePendingMFA(ctx, result.PendingToken, code)
}

// SetupPendingMFA creates a TOTP secret after the password and CAPTCHA stage
// has succeeded, but before a normal application session exists.
func (service *SysAuthService) SetupPendingMFA(ctx context.Context, pendingToken string) (response.SysMFASetup, error) {
	user, err := service.pendingMFAUser(ctx, pendingToken, authStageMFAEnrollment)
	if err != nil {
		return response.SysMFASetup{}, err
	}
	return service.createMFASetup(ctx, user)
}

// CompletePendingMFA validates one post-password MFA stage and creates the
// first real session atomically with clearing the pending state on sys_users.
func (service *SysAuthService) CompletePendingMFA(ctx context.Context, pendingToken, code string) (*response.SysAuthResult, error) {
	pendingToken = strings.TrimSpace(pendingToken)
	if pendingToken == "" || strings.TrimSpace(code) == "" {
		return nil, errMFACodeRequired
	}
	var result response.SysAuthResult
	err := global.GVA_DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		var user model.SysUser
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("mfa_login_step_token_hash = ? AND mfa_login_step_expires_at > ?", authDigest(pendingToken), now).First(&user).Error; err != nil {
			return fmt.Errorf("登录验证已过期，请重新输入账号和密码")
		}
		if user.Status != model.UserStatusActive {
			return fmt.Errorf("账号不可用")
		}
		switch user.MFA.LoginStepKind {
		case authStageMFAVerify:
			if !user.MFA.Enabled || verifyUserTOTP(user, code, now) != nil {
				return errMFACodeRequired
			}
		case authStageMFAEnrollment:
			if user.MFA.Enabled || verifyUserTOTP(user, code, now) != nil {
				return errMFACodeRequired
			}
			if err := tx.Model(&user).Update("mfa_enabled", true).Error; err != nil {
				return err
			}
			user.MFA.Enabled = true
		default:
			return fmt.Errorf("无效的登录验证状态")
		}
		token, err := service.createSessionWithDB(ctx, tx, user.ID, "local")
		if err != nil {
			return err
		}
		if err := tx.Model(&user).Updates(map[string]any{"mfa_login_step_token_hash": "", "mfa_login_step_kind": "", "mfa_login_step_expires_at": nil}).Error; err != nil {
			return err
		}
		result = response.SysAuthResult{User: &user, SessionToken: token, AuthStage: authStageAuthenticated}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (service *SysAuthService) createPendingMFA(ctx context.Context, userID uint, stage string) (string, error) {
	if stage != authStageMFAVerify && stage != authStageMFAEnrollment {
		return "", fmt.Errorf("无效的登录验证状态")
	}
	token, err := randomAuthValue(32)
	if err != nil {
		return "", err
	}
	expiresAt := time.Now().Add(pendingMFATokenLifetime)
	updates := map[string]any{"mfa_login_step_token_hash": authDigest(token), "mfa_login_step_kind": stage, "mfa_login_step_expires_at": expiresAt}
	if err := global.GVA_DB.WithContext(ctx).Model(&model.SysUser{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
		return "", err
	}
	return token, nil
}

func (service *SysAuthService) pendingMFAUser(ctx context.Context, pendingToken, stage string) (*model.SysUser, error) {
	pendingToken = strings.TrimSpace(pendingToken)
	if pendingToken == "" {
		return nil, fmt.Errorf("登录验证已过期，请重新输入账号和密码")
	}
	var user model.SysUser
	if err := global.GVA_DB.WithContext(ctx).Where("mfa_login_step_token_hash = ? AND mfa_login_step_kind = ? AND mfa_login_step_expires_at > ?", authDigest(pendingToken), stage, time.Now()).First(&user).Error; err != nil {
		return nil, fmt.Errorf("登录验证已过期，请重新输入账号和密码")
	}
	if user.Status != model.UserStatusActive {
		return nil, fmt.Errorf("账号不可用")
	}
	return &user, nil
}

func (service *SysAuthService) recordFailedLogin(ctx context.Context, user *model.SysUser, now time.Time) {
	if user == nil || user.ID == 0 {
		return
	}
	count := user.FailedLoginCount + 1
	updates := map[string]any{"failed_login_count": count}
	if count >= loginMaxFailures() {
		updates["locked_until"] = now.Add(loginLockDuration())
	}
	_ = global.GVA_DB.WithContext(ctx).Model(user).Updates(updates).Error
}

func validateLocalPassword(username, password string) error {
	if len([]rune(password)) < 8 {
		return fmt.Errorf("密码至少需要 8 个字符")
	}
	if strings.Contains(strings.ToLower(password), strings.ToLower(strings.TrimSpace(username))) {
		return fmt.Errorf("密码不能包含用户名")
	}
	classes := 0
	var lower, upper, digit, symbol bool
	for _, value := range password {
		switch {
		case unicode.IsLower(value):
			lower = true
		case unicode.IsUpper(value):
			upper = true
		case unicode.IsDigit(value):
			digit = true
		case !unicode.IsSpace(value):
			symbol = true
		}
	}
	for _, yes := range []bool{lower, upper, digit, symbol} {
		if yes {
			classes++
		}
	}
	if classes < 3 && !(len([]rune(password)) >= 16 && classes >= 2) {
		return fmt.Errorf("密码应包含大写、小写、数字、符号中的至少三类；16 位以上长口令可使用两类")
	}
	return nil
}

func verifyUserTOTP(user model.SysUser, code string, now time.Time) error {
	if strings.TrimSpace(user.MFA.TOTPSecretCiphertext) == "" {
		return fmt.Errorf("MFA 密钥不存在")
	}
	secret, err := securestore.DecryptWithSecret(global.GVA_CONFIG.Auth.JWTSecret, user.MFA.TOTPSecretCiphertext)
	if err != nil {
		return err
	}
	if !verifyTOTP(string(secret), code, now) {
		return fmt.Errorf("动态验证码无效")
	}
	return nil
}

func generateTOTPSecret() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}

func verifyTOTP(secret, code string, now time.Time) bool {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false
	}
	for offset := -1; offset <= 1; offset++ {
		if subtle.ConstantTimeCompare([]byte(totpCode(secret, now.Add(time.Duration(offset)*30*time.Second))), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

func totpCode(secret string, at time.Time) string {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return ""
	}
	buffer := make([]byte, 8)
	binary.BigEndian.PutUint64(buffer, uint64(at.Unix()/30))
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(buffer)
	digest := mac.Sum(nil)
	offset := digest[len(digest)-1] & 0x0f
	value := binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1000000)
}

func randomAuthValue(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func randomCaptchaAnswer(size int) (string, error) {
	const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	for index := range raw {
		raw[index] = alphabet[int(raw[index])%len(alphabet)]
	}
	return string(raw), nil
}

func captchaImage(answer string) string {
	// The answer is generated internally from a restricted alphabet and escaped
	// anyway; no caller-provided content is interpolated into the SVG.
	escaped := html.EscapeString(answer)
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="176" height="56" viewBox="0 0 176 56"><rect width="176" height="56" rx="8" fill="#edf7f0"/><path d="M8 41 L48 12 M64 48 L101 9 M122 46 L166 16" stroke="#89b79d" stroke-width="2" opacity=".55"/><path d="M11 17 C44 50 94 2 166 38" fill="none" stroke="#c99070" stroke-width="1.5" opacity=".65"/><text x="17" y="37" fill="#184f3e" font-family="monospace" font-size="27" font-weight="700" letter-spacing="5">` + escaped + `</text></svg>`
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
}

func authDigest(value string) string {
	key := []byte(global.GVA_CONFIG.Auth.JWTSecret)
	if len(key) == 0 {
		key = []byte("inkflow-auth-development-fallback")
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

func captchaTTL() time.Duration {
	if value := global.GVA_CONFIG.Auth.CaptchaTTLSeconds; value > 0 {
		return time.Duration(value) * time.Second
	}
	return 5 * time.Minute
}
func loginMaxFailures() int {
	if value := global.GVA_CONFIG.Auth.LoginMaxFailures; value > 0 {
		return value
	}
	return 5
}
func loginLockDuration() time.Duration {
	if value := global.GVA_CONFIG.Auth.LoginLockMinutes; value > 0 {
		return time.Duration(value) * time.Minute
	}
	return 15 * time.Minute
}
func loginIPLimit() int {
	if value := global.GVA_CONFIG.Auth.LoginIPLimit; value > 0 {
		return value
	}
	return 30
}
func loginIPWindowDuration() time.Duration {
	if value := global.GVA_CONFIG.Auth.LoginIPWindowSeconds; value > 0 {
		return time.Duration(value) * time.Second
	}
	return 10 * time.Minute
}
