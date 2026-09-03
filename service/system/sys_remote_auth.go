package system

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"InkFlow/config"
	"InkFlow/global"
	"InkFlow/model/system"
	response "InkFlow/model/system/response"
	"InkFlow/utils/securestore"

	"gorm.io/gorm"
)

// SysAuthService owns local and remote authentication operations.
type SysAuthService struct{}

type remoteTokenResponse struct {
	Token        string          `json:"token"`
	AccessToken  string          `json:"access_token"`
	RefreshToken string          `json:"refresh_token"`
	ExpiresIn    int64           `json:"expires_in"`
	AuthDomain   string          `json:"auth_domain"`
	User         json.RawMessage `json:"user"`
}

type remoteUser struct {
	ID       json.RawMessage `json:"id"`
	Username string          `json:"username"`
	Status   string          `json:"status"`
}

type remoteCredential struct {
	Domain       string    `json:"domain"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// AuthenticateRemote 将凭据转发给远端认证服务，并为认证成功的用户创建本地会话。
// 本地数据库不保存远端密码或访问令牌，令牌仅尝试保存至操作系统安全凭据存储。
func (s *SysAuthService) AuthenticateRemote(ctx context.Context, action, username, password, tenantName string) (*response.SysAuthResult, error) {
	if action != "login" && action != "register" {
		return nil, errors.New("unsupported remote authentication action")
	}
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, errors.New("用户名和密码不能为空")
	}
	domain := remoteAuthBaseURL()
	if domain == "" {
		return nil, errors.New("未配置远端认证服务")
	}
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, domain+"/auth/"+action, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	timeout := time.Duration(remoteTimeoutSeconds()) * time.Second
	client := &http.Client{Timeout: timeout}
	httpResponse, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("远端认证服务不可用: %w", err)
	}
	defer httpResponse.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(httpResponse.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		return nil, fmt.Errorf("远端认证失败: %s", httpResponse.Status)
	}
	token, user, err := parseRemoteResponse(raw, domain, username)
	if err != nil {
		return nil, err
	}
	localUser, isNew, err := s.syncRemoteUser(ctx, user, token)
	if err != nil {
		return nil, err
	}
	if isNew {
		if _, err := ServiceGroupApp.SysTenantService.CreateTenant(ctx, localUser.ID, tenantName, ""); err != nil {
			return nil, err
		}
	}
	if err := saveRemoteCredential(localUser.ID, token); err != nil && !errors.Is(err, securestore.ErrUnsupported) {
		return nil, err
	}
	sessionToken, err := s.createSession(ctx, localUser.ID, "remote")
	if err != nil {
		return nil, err
	}
	return &response.SysAuthResult{User: localUser, SessionToken: sessionToken}, nil
}

// syncRemoteUser 同步远端身份资料并创建或更新本地用户映射。
func (s *SysAuthService) syncRemoteUser(ctx context.Context, identity remoteUser, credential remoteCredential) (*system.SysUser, bool, error) {
	db := global.GVA_DB
	remoteID := parseRemoteID(identity.ID)
	var user system.SysUser
	query := db.WithContext(ctx)
	if remoteID > 0 {
		query = query.Where("remote_user_id = ? AND auth_domain = ?", remoteID, credential.Domain)
	} else {
		query = query.Where("username = ?", identity.Username)
	}
	err := query.First(&user).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	status := strings.TrimSpace(identity.Status)
	if status == "" {
		status = system.UserStatusActive
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		user = system.SysUser{Username: identity.Username, RemoteUserID: remoteID, AuthDomain: credential.Domain, Status: status}
		if err := db.WithContext(ctx).Create(&user).Error; err != nil {
			return nil, false, err
		}
		return &user, true, nil
	}
	user.Username, user.RemoteUserID, user.AuthDomain, user.Status = identity.Username, remoteID, credential.Domain, status
	if err := db.WithContext(ctx).Save(&user).Error; err != nil {
		return nil, false, err
	}
	return &user, false, nil
}

// parseRemoteResponse 解析远端认证响应中的凭据和用户身份信息。
func parseRemoteResponse(raw []byte, defaultDomain, fallbackUsername string) (remoteCredential, remoteUser, error) {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(raw, &envelope)
	payload := raw
	if len(envelope.Data) > 0 && string(envelope.Data) != "null" {
		payload = envelope.Data
	}
	var response remoteTokenResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		return remoteCredential{}, remoteUser{}, err
	}
	accessToken := response.AccessToken
	if accessToken == "" {
		accessToken = response.Token
	}
	if accessToken == "" {
		return remoteCredential{}, remoteUser{}, errors.New("远端认证响应未包含访问令牌")
	}
	domain := config.NormalizeRemoteAuthBaseURL(response.AuthDomain)
	if domain == "" {
		domain = defaultDomain
	}
	identity := remoteUser{Username: fallbackUsername, Status: system.UserStatusActive}
	if len(response.User) > 0 && string(response.User) != "null" {
		if err := json.Unmarshal(response.User, &identity); err != nil {
			return remoteCredential{}, remoteUser{}, err
		}
		if identity.Username == "" {
			identity.Username = fallbackUsername
		}
	}
	return remoteCredential{Domain: domain, AccessToken: accessToken, RefreshToken: response.RefreshToken, ExpiresAt: time.Now().Add(time.Duration(response.ExpiresIn) * time.Second)}, identity, nil
}

// parseRemoteID 将远端响应中的用户标识解析为无符号整数。
func parseRemoteID(raw json.RawMessage) uint {
	var number uint
	if json.Unmarshal(raw, &number) == nil {
		return number
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		parsed, _ := strconv.ParseUint(value, 10, 64)
		return uint(parsed)
	}
	return 0
}

// saveRemoteCredential 将远端访问令牌尝试保存到系统安全凭据存储。
func saveRemoteCredential(userID uint, credential remoteCredential) error {
	raw, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	return securestore.New().Save(fmt.Sprintf("InkFlow.System.RemoteAuth/%d", userID), raw)
}

// remoteAuthBaseURL 返回已配置的远端认证服务地址。
func remoteAuthBaseURL() string {
	configured := config.NormalizeRemoteAuthBaseURL(global.GVA_CONFIG.Auth.RemoteBaseURL)
	if configured != "" {
		return configured
	}
	return config.NormalizeRemoteAuthBaseURL(config.BootstrapAuthBaseURL)
}

// remoteTimeoutSeconds 返回远端认证请求的超时时间。
func remoteTimeoutSeconds() int {
	value := global.GVA_CONFIG.Auth.RemoteTimeoutSeconds
	if value <= 0 {
		return 15
	}
	return value
}
