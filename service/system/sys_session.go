package system

import (
	"InkFlow/global"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	model "InkFlow/model/system"
	response "InkFlow/model/system/response"

	"gorm.io/gorm"
)

const defaultSessionTTL = 24 * time.Hour

// Logout 删除指定令牌的会话，使其立即失效。
func (s *SysAuthService) Logout(ctx context.Context, token string) error {
	db := global.GVA_DB
	if strings.TrimSpace(token) == "" {
		return nil
	}
	return db.WithContext(ctx).Where("token_hash = ?", hashToken(token)).Delete(&model.SysSession{}).Error
}

// createSession 为用户生成随机令牌并持久化其哈希会话记录。
func (s *SysAuthService) createSession(ctx context.Context, userID uint, kind string) (string, error) {
	return s.createSessionWithDB(ctx, global.GVA_DB, userID, kind)
}

// createSessionWithDB keeps a session creation in the same transaction as a
// completed MFA challenge, so a pending login cannot be consumed without a
// durable session being created.
func (s *SysAuthService) createSessionWithDB(ctx context.Context, db *gorm.DB, userID uint, kind string) (string, error) {
	if db == nil {
		return "", fmt.Errorf("session database is unavailable")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	now := time.Now()
	session := model.SysSession{UserID: userID, TokenHash: hashToken(token), ExpiresAt: now.Add(configuredSessionTTL()), LastSeenAt: now, Kind: kind}
	if err := db.WithContext(ctx).Create(&session).Error; err != nil {
		return "", err
	}
	return token, nil
}

// AttachSessionMetadata records the device information for the just-created
// session. It intentionally accepts the raw token only inside the service; the
// database never stores the raw token.
func (s *SysAuthService) AttachSessionMetadata(ctx context.Context, token, clientIP, userAgent string) error {
	if strings.TrimSpace(token) == "" {
		return nil
	}
	return global.GVA_DB.WithContext(ctx).Model(&model.SysSession{}).Where("token_hash = ?", hashToken(token)).Updates(map[string]any{
		"client_ip":    truncateSessionValue(clientIP, 64),
		"user_agent":   truncateSessionValue(userAgent, 512),
		"device_name":  deviceName(userAgent),
		"last_seen_at": time.Now(),
	}).Error
}

// ListSessions exposes only metadata for the authenticated user's own
// sessions; hashes and raw credentials never leave the service.
func (s *SysAuthService) ListSessions(ctx context.Context, userID uint, currentToken string) ([]response.SysSessionView, error) {
	var sessions []model.SysSession
	if err := global.GVA_DB.WithContext(ctx).Where("user_id = ? AND expires_at > ?", userID, time.Now()).Order("last_seen_at DESC").Find(&sessions).Error; err != nil {
		return nil, err
	}
	currentHash := hashToken(currentToken)
	items := make([]response.SysSessionView, 0, len(sessions))
	for _, session := range sessions {
		items = append(items, response.SysSessionView{
			ID: session.ID, Kind: session.Kind, ClientIP: session.ClientIP, DeviceName: session.DeviceName,
			CreatedAt: session.CreatedAt.UTC().Format(time.RFC3339), LastSeenAt: session.LastSeenAt.UTC().Format(time.RFC3339),
			ExpiresAt: session.ExpiresAt.UTC().Format(time.RFC3339), Current: subtleEqual(session.TokenHash, currentHash),
		})
	}
	return items, nil
}

// RevokeSession deletes one session only after proving that it belongs to the
// current user, preventing cross-account device management.
func (s *SysAuthService) RevokeSession(ctx context.Context, userID, sessionID uint) error {
	result := global.GVA_DB.WithContext(ctx).Where("id = ? AND user_id = ?", sessionID, userID).Delete(&model.SysSession{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// RevokeOtherSessions makes the current device the sole remaining session.
func (s *SysAuthService) RevokeOtherSessions(ctx context.Context, userID uint, currentToken string) error {
	return global.GVA_DB.WithContext(ctx).Where("user_id = ? AND token_hash <> ?", userID, hashToken(currentToken)).Delete(&model.SysSession{}).Error
}

func configuredSessionTTL() time.Duration {
	if value := global.GVA_CONFIG.Auth.SessionTTLHours; value > 0 {
		return time.Duration(value) * time.Hour
	}
	return defaultSessionTTL
}

func deviceName(userAgent string) string {
	value := strings.TrimSpace(userAgent)
	if value == "" {
		return "未知设备"
	}
	return truncateSessionValue(value, 255)
}

func truncateSessionValue(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	return value[:max]
}

func subtleEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	matched := byte(0)
	for index := range left {
		matched |= left[index] ^ right[index]
	}
	return matched == 0
}

// hashToken 计算会话令牌的 SHA-256 哈希值。
func hashToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}
