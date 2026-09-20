package config

import "time"

const defaultOAuthStateTTL = 10 * time.Minute

var knownOAuthProviders = map[string]OAuthProvider{
	"google": {
		DisplayName: "Google",
		AuthURL:     "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:    "https://oauth2.googleapis.com/token",
		UserInfoURL: "https://openidconnect.googleapis.com/v1/userinfo",
		Scopes:      []string{"openid", "email", "profile"},
		UsePKCE:     true,
		IDField:     "sub",
		EmailField:  "email",
		NameField:   "name",
		AvatarField: "picture",
	},
	"github": {
		DisplayName: "GitHub",
		AuthURL:     "https://github.com/login/oauth/authorize",
		TokenURL:    "https://github.com/login/oauth/access_token",
		UserInfoURL: "https://api.github.com/user",
		Scopes:      []string{"read:user", "user:email"},
		UsePKCE:     false,
		IDField:     "id",
		EmailField:  "email",
		NameField:   "login",
		AvatarField: "avatar_url",
	},
}

// OAuthProvider describes a standard OAuth2 Authorization Code provider.
type OAuthProvider struct {
	Enabled        bool     `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	DisplayName    string   `mapstructure:"display-name" json:"display-name" yaml:"display-name"`
	ClientID       string   `mapstructure:"client-id" json:"client-id" yaml:"client-id"`
	ClientSecret   string   `mapstructure:"client-secret" json:"client-secret" yaml:"client-secret"`
	RedirectURL    string   `mapstructure:"redirect-url" json:"redirect-url" yaml:"redirect-url"`
	AuthURL        string   `mapstructure:"auth-url" json:"auth-url" yaml:"auth-url"`
	TokenURL       string   `mapstructure:"token-url" json:"token-url" yaml:"token-url"`
	UserInfoURL    string   `mapstructure:"user-info-url" json:"user-info-url" yaml:"user-info-url"`
	Scopes         []string `mapstructure:"scopes" json:"scopes" yaml:"scopes"`
	UsePKCE        bool     `mapstructure:"use-pkce" json:"use-pkce" yaml:"use-pkce"`
	IDField        string   `mapstructure:"id-field" json:"id-field" yaml:"id-field"`
	EmailField     string   `mapstructure:"email-field" json:"email-field" yaml:"email-field"`
	NameField      string   `mapstructure:"name-field" json:"name-field" yaml:"name-field"`
	AvatarField    string   `mapstructure:"avatar-field" json:"avatar-field" yaml:"avatar-field"`
	ExtraUserAgent string   `mapstructure:"extra-user-agent" json:"extra-user-agent" yaml:"extra-user-agent"`
}

// StateCookiePrefix is the browser cookie prefix used for OAuth CSRF state.
const StateCookiePrefix = "inkflow_oauth_state_"
