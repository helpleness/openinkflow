package config

import (
	"net/url"
	"strings"
	"time"
)

// BootstrapAuthBaseURL 只用于尚未登录过的新安装包发起首次注册或登录。
// 登录成功后，客户端只使用远端 user.auth_domain 同步到本地 users 表中的
// 域名；因此域名迁移不要求终端用户改设置。旧认证服务在过渡期返回新的
// auth_domain 即可把已有客户端逐步导向新地址。
const BootstrapAuthBaseURL = "https://doc.inkflowai.top"

type Auth struct {
	// JWTSecret is also the deployment secret used to encrypt user-saved model
	// API keys before they are stored in the database. It must be unique in
	// production and must not be committed with a real value.
	JWTSecret string `mapstructure:"jwt-secret" json:"jwt-secret" yaml:"jwt-secret"`
	// RemoteBaseURL 为空时使用 BootstrapAuthBaseURL；企业部署可在配置中覆盖。
	RemoteBaseURL string `mapstructure:"remote-base-url" json:"remote-base-url" yaml:"remote-base-url"`
	// RemoteTimeoutSeconds 限制账号请求等待时间。令牌仍有效时，网络不可用不会阻止离线使用。
	RemoteTimeoutSeconds int `mapstructure:"remote-timeout-seconds" json:"remote-timeout-seconds" yaml:"remote-timeout-seconds"`
	// Cookie settings apply to browser deployments. The desktop shell retains
	// its loopback session handoff and never writes an auth token to web storage.
	SessionCookieName     string `mapstructure:"session-cookie-name" json:"session-cookie-name" yaml:"session-cookie-name"`
	SessionCookieSecure   bool   `mapstructure:"session-cookie-secure" json:"session-cookie-secure" yaml:"session-cookie-secure"`
	SessionCookieSameSite string `mapstructure:"session-cookie-same-site" json:"session-cookie-same-site" yaml:"session-cookie-same-site"`
	SessionTTLHours       int    `mapstructure:"session-ttl-hours" json:"session-ttl-hours" yaml:"session-ttl-hours"`
	LoginMaxFailures      int    `mapstructure:"login-max-failures" json:"login-max-failures" yaml:"login-max-failures"`
	LoginLockMinutes      int    `mapstructure:"login-lock-minutes" json:"login-lock-minutes" yaml:"login-lock-minutes"`
	LoginIPLimit          int    `mapstructure:"login-ip-limit" json:"login-ip-limit" yaml:"login-ip-limit"`
	LoginIPWindowSeconds  int    `mapstructure:"login-ip-window-seconds" json:"login-ip-window-seconds" yaml:"login-ip-window-seconds"`
	CaptchaTTLSeconds     int    `mapstructure:"captcha-ttl-seconds" json:"captcha-ttl-seconds" yaml:"captcha-ttl-seconds"`
	// MFAEnrollmentRequired requires every local account to bind TOTP after its next
	// successful password verification. Existing bindings still require a code.
	MFAEnrollmentRequired bool `mapstructure:"mfa-enrollment-required" json:"mfa-enrollment-required" yaml:"mfa-enrollment-required"`

	// OAuth2 Authorization Code 登录配置。
	PublicBaseURL     string                   `mapstructure:"public-base-url" json:"public-base-url" yaml:"public-base-url"`
	FrontendBaseURL   string                   `mapstructure:"frontend-base-url" json:"frontend-base-url" yaml:"frontend-base-url"`
	AllowedOrigins    []string                 `mapstructure:"allowed-origins" json:"allowed-origins" yaml:"allowed-origins"`
	PostLoginRedirect string                   `mapstructure:"post-login-redirect" json:"post-login-redirect" yaml:"post-login-redirect"`
	PostErrorRedirect string                   `mapstructure:"post-error-redirect" json:"post-error-redirect" yaml:"post-error-redirect"`
	StateTTL          time.Duration            `mapstructure:"state-ttl" json:"state-ttl" yaml:"state-ttl"`
	OAuthProviders    map[string]OAuthProvider `mapstructure:"oauth-providers" json:"oauth-providers" yaml:"oauth-providers"`
}

// NormalizeRemoteAuthBaseURL 将配置值规范化为用于身份隔离和凭据命名的
// 域名根地址。空或无效配置返回空字符串，由调用方拒绝发起认证请求。
func NormalizeRemoteAuthBaseURL(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return strings.TrimRight(parsed.Scheme+"://"+strings.ToLower(parsed.Host)+strings.TrimRight(parsed.Path, "/"), "/")
}

// NormalizeOAuthProviders fills known-provider defaults and login redirect
// defaults. The YAML file only needs the values that vary per deployment:
// enabled, client-id and client-secret. Custom providers may still override
// every protocol field explicitly.
func (a *Auth) NormalizeOAuthProviders() {
	if a.PublicBaseURL == "" {
		a.PublicBaseURL = "http://127.0.0.1:8888"
	}
	if a.FrontendBaseURL == "" {
		a.FrontendBaseURL = "http://localhost:5173"
	}
	if len(a.AllowedOrigins) == 0 {
		a.AllowedOrigins = []string{"http://localhost:5173", "http://127.0.0.1:5173"}
	}
	if a.PostLoginRedirect == "" {
		a.PostLoginRedirect = "/"
	}
	if a.PostErrorRedirect == "" {
		a.PostErrorRedirect = "/login"
	}
	if a.StateTTL <= 0 {
		a.StateTTL = defaultOAuthStateTTL
	}
	if a.OAuthProviders == nil {
		a.OAuthProviders = make(map[string]OAuthProvider)
	}
	for name, provider := range a.OAuthProviders {
		a.OAuthProviders[name] = normalizeOAuthProvider(name, provider)
	}
}

func normalizeOAuthProvider(name string, provider OAuthProvider) OAuthProvider {
	known, ok := knownOAuthProviders[name]
	if !ok {
		return provider
	}
	if provider.DisplayName == "" {
		provider.DisplayName = known.DisplayName
	}
	if provider.AuthURL == "" {
		provider.AuthURL = known.AuthURL
	}
	if provider.TokenURL == "" {
		provider.TokenURL = known.TokenURL
	}
	if provider.UserInfoURL == "" {
		provider.UserInfoURL = known.UserInfoURL
	}
	if len(provider.Scopes) == 0 {
		provider.Scopes = append([]string(nil), known.Scopes...)
	}
	provider.UsePKCE = known.UsePKCE
	if provider.IDField == "" {
		provider.IDField = known.IDField
	}
	if provider.EmailField == "" {
		provider.EmailField = known.EmailField
	}
	if provider.NameField == "" {
		provider.NameField = known.NameField
	}
	if provider.AvatarField == "" {
		provider.AvatarField = known.AvatarField
	}
	return provider
}
