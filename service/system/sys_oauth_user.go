package system

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"InkFlow/config"
	"InkFlow/global"
	model "InkFlow/model/system"
	response "InkFlow/model/system/response"
	strutil "InkFlow/utils"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
)

const (
	stateIssuer        = "inkflow-oauth-state"
	defaultStateTTL    = 10 * time.Minute
	defaultHTTPTimeout = 30 * time.Second
)

// ErrOAuthProviderDisabled 表示请求的 OAuth2 Provider 未启用或未配置。
var ErrOAuthProviderDisabled = errors.New("oauth provider is disabled or not configured")

type oauthStateClaims struct {
	Provider    string `json:"provider"`
	RedirectURI string `json:"redirect_uri"`
	Verifier    string `json:"verifier,omitempty"`
	jwt.RegisteredClaims
}

// OAuthLoginURL 生成 OAuth2 授权地址，并返回需要写入浏览器的一次性 State。
func (s *SysAuthService) OAuthLoginURL(providerName, redirectURI string) (string, string, error) {
	provider, err := s.oauthProvider(providerName)
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(s.oauthConfigAuth().JWTSecret) == "" {
		return "", "", errors.New("auth.jwt-secret is required")
	}

	stateTTL := s.oauthConfigAuth().StateTTL
	if stateTTL <= 0 {
		stateTTL = defaultStateTTL
	}
	now := time.Now()
	stateID, err := strutil.RandomStateID()
	if err != nil {
		return "", "", fmt.Errorf("generate oauth state id failed: %w", err)
	}
	claims := oauthStateClaims{
		Provider:    providerName,
		RedirectURI: redirectURI,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    stateIssuer,
			Subject:   "oauth-login",
			ID:        stateID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(stateTTL)),
		},
	}

	var authOptions []oauth2.AuthCodeOption
	if provider.UsePKCE {
		verifier := oauth2.GenerateVerifier()
		claims.Verifier = verifier
		authOptions = append(authOptions, oauth2.S256ChallengeOption(verifier))
	}

	stateToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.oauthConfigAuth().JWTSecret))
	if err != nil {
		return "", "", fmt.Errorf("sign oauth state failed: %w", err)
	}

	authURL := s.oauth2Config(provider, redirectURI).AuthCodeURL(stateToken, authOptions...)
	return authURL, stateToken, nil
}

// OAuthCallback 用授权码换取用户信息，创建或更新系统用户，并创建普通系统会话。
func (s *SysAuthService) OAuthCallback(providerName, code, stateToken string) (*response.SysAuthResult, bool, error) {
	provider, err := s.oauthProvider(providerName)
	if err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(code) == "" {
		return nil, false, errors.New("authorization code is empty")
	}

	claims, err := s.parseOAuthState(stateToken)
	if err != nil {
		return nil, false, fmt.Errorf("invalid oauth state: %w", err)
	}
	if claims.Provider != providerName {
		return nil, false, errors.New("oauth state provider mismatch")
	}

	redirectURI := s.oauthRedirectURI(providerName, provider)
	if claims.RedirectURI != redirectURI {
		return nil, false, errors.New("oauth redirect uri mismatch")
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultHTTPTimeout)
	defer cancel()

	oauthConfig := s.oauth2Config(provider, redirectURI)
	var exchangeOptions []oauth2.AuthCodeOption
	if claims.Verifier != "" {
		exchangeOptions = append(exchangeOptions, oauth2.VerifierOption(claims.Verifier))
	}
	token, err := oauthConfig.Exchange(ctx, code, exchangeOptions...)
	if err != nil {
		return nil, false, fmt.Errorf("exchange oauth authorization code failed: %w", err)
	}

	profile, err := fetchOAuthUserInfo(ctx, oauthConfig.Client(ctx, token), provider)
	if err != nil {
		return nil, false, err
	}

	user, registered, err := s.upsertOAuthUser(ctx, providerName, provider, profile)
	if err != nil {
		return nil, false, err
	}
	sessionToken, err := s.createSession(ctx, user.ID, "oauth")
	if err != nil {
		return nil, false, err
	}
	return &response.SysAuthResult{User: user, SessionToken: sessionToken, AuthStage: authStageAuthenticated}, registered, nil
}

func (s *SysAuthService) oauthConfigAuth() *config.Auth {
	return &global.GVA_CONFIG.Auth
}

func (s *SysAuthService) oauthProvider(name string) (*config.OAuthProvider, error) {
	if name == "" {
		return nil, ErrOAuthProviderDisabled
	}
	provider, ok := s.oauthConfigAuth().OAuthProviders[name]
	if !ok || !provider.Enabled {
		return nil, ErrOAuthProviderDisabled
	}
	if provider.ClientID == "" || provider.ClientSecret == "" || provider.AuthURL == "" || provider.TokenURL == "" {
		return nil, fmt.Errorf("oauth provider %q is incomplete: client-id, client-secret, auth-url and token-url are required", name)
	}
	return &provider, nil
}

func (s *SysAuthService) oauth2Config(provider *config.OAuthProvider, redirectURI string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     provider.ClientID,
		ClientSecret: provider.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  provider.AuthURL,
			TokenURL: provider.TokenURL,
		},
		RedirectURL: redirectURI,
		Scopes:      provider.Scopes,
	}
}

func (s *SysAuthService) oauthRedirectURI(name string, provider *config.OAuthProvider) string {
	if provider.RedirectURL != "" {
		return strings.TrimSuffix(provider.RedirectURL, "/")
	}
	base := strings.TrimSuffix(s.oauthConfigAuth().PublicBaseURL, "/")
	return fmt.Sprintf("%s/auth/oauth/%s/callback", base, name)
}

func (s *SysAuthService) parseOAuthState(tokenString string) (*oauthStateClaims, error) {
	claims := &oauthStateClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		return []byte(s.oauthConfigAuth().JWTSecret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer(stateIssuer))
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("invalid state token")
	}
	return claims, nil
}

// upsertOAuthUser keeps the provider identity in its own table, but the resulting
// SysUser is the only identity used by sessions, tenants, roles and permissions.
func (s *SysAuthService) upsertOAuthUser(ctx context.Context, providerName string, provider *config.OAuthProvider, profile map[string]interface{}) (*model.SysUser, bool, error) {
	if global.GVA_DB == nil {
		return nil, false, gorm.ErrInvalidDB
	}

	idField := strutil.FirstNonEmpty(provider.IDField, "sub", "id", "user_id")
	providerUID, _ := strutil.StringFromMapPath(profile, idField)
	providerUID = strutil.NormalizeText(providerUID)
	if providerUID == "" {
		return nil, false, fmt.Errorf("oauth user info does not contain field %q", idField)
	}

	emailField := strutil.FirstNonEmpty(provider.EmailField, "email")
	nameField := strutil.FirstNonEmpty(provider.NameField, "name")
	avatarField := strutil.FirstNonEmpty(provider.AvatarField, "picture", "avatar_url")

	email, _ := strutil.StringFromMapPath(profile, emailField)
	name, _ := strutil.StringFromMapPath(profile, nameField)
	if name == "" {
		name = strutil.FirstNonEmpty(provider.DisplayName, providerName+"-"+providerUID)
	}
	avatarURL, _ := strutil.StringFromMapPath(profile, avatarField)
	now := time.Now()

	var user model.SysUser
	registered := false
	err := global.GVA_DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var account model.SysOAuthUser
		err := tx.Where("provider = ? AND provider_uid = ?", providerName, providerUID).First(&account).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			registered = true
			username := s.availableOAuthUsername(ctx, tx, email, name, providerName, providerUID)
			user = model.SysUser{Username: username, Status: model.UserStatusActive}
			if err := tx.WithContext(ctx).Create(&user).Error; err != nil {
				return fmt.Errorf("create oauth system user failed: %w", err)
			}
			account = model.SysOAuthUser{
				UserID: user.ID, Provider: providerName, ProviderUID: providerUID,
				Name: name, Email: email, AvatarURL: avatarURL, LastLoginAt: &now,
			}
			if err := tx.WithContext(ctx).Create(&account).Error; err != nil {
				return fmt.Errorf("create oauth identity failed: %w", err)
			}
			return nil
		case err != nil:
			return err
		}

		if err := tx.WithContext(ctx).First(&user, account.UserID).Error; err != nil {
			return err
		}
		if user.Status != model.UserStatusActive {
			return errors.New("OAuth 账号已被禁用")
		}
		account.Name, account.Email, account.AvatarURL, account.LastLoginAt = name, email, avatarURL, &now
		if err := tx.WithContext(ctx).Save(&account).Error; err != nil {
			return fmt.Errorf("update oauth identity failed: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return &user, registered, nil
}

// availableOAuthUsername never links by email: an existing account must not be
// taken over just because an OAuth provider returns the same address. If every
// preferred value is occupied, the provider-specific fallback remains unique.
func (s *SysAuthService) availableOAuthUsername(ctx context.Context, db *gorm.DB, email, name, providerName, providerUID string) string {
	candidates := []string{strutil.NormalizeText(email), strutil.NormalizeText(name)}
	fallback := strings.TrimSpace(providerName + "-" + providerUID)
	for _, candidate := range candidates {
		if len(candidate) < 3 || len(candidate) > 64 {
			continue
		}
		var count int64
		if err := db.WithContext(ctx).Unscoped().Model(&model.SysUser{}).Where("username = ?", candidate).Count(&count).Error; err == nil && count == 0 {
			return candidate
		}
	}
	base := strutil.PrefixRunes(fallback, 64)
	var count int64
	if err := db.WithContext(ctx).Unscoped().Model(&model.SysUser{}).Where("username = ?", base).Count(&count).Error; err == nil && count == 0 {
		return base
	}
	return strutil.PrefixRunes(fmt.Sprintf("%s-%d", base, time.Now().UnixNano()), 64)
}

func fetchOAuthUserInfo(ctx context.Context, client *http.Client, provider *config.OAuthProvider) (map[string]interface{}, error) {
	if provider.UserInfoURL == "" {
		return nil, errors.New("auth provider user-info-url is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, provider.UserInfoURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", strutil.FirstNonEmpty(provider.ExtraUserAgent, "InkFlow"))

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch oauth user info failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read oauth user info failed: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("fetch oauth user info failed: status %d, body %s", resp.StatusCode, strutil.Truncate(string(body), 512))
	}

	var profile map[string]interface{}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&profile); err != nil {
		return nil, fmt.Errorf("decode oauth user info failed: %w", err)
	}
	return profile, nil
}
