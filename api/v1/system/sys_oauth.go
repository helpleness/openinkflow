package system

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"InkFlow/config"
	"InkFlow/global"
	"InkFlow/model/common/response"
	systemservice "InkFlow/service/system"
	strutil "InkFlow/utils"

	"github.com/gin-gonic/gin"
)

// GetOAuthProviders returns enabled external login entries for the frontend.
func (api *SysAuthApi) GetOAuthProviders(c *gin.Context) {
	auth := global.GVA_CONFIG.Auth
	providers := make([]gin.H, 0, len(auth.OAuthProviders))
	for name, provider := range auth.OAuthProviders {
		if !provider.Enabled {
			continue
		}
		providers = append(providers, gin.H{
			"name":         name,
			"display_name": strutil.FirstNonEmpty(provider.DisplayName, name),
		})
	}
	response.OkWithData(gin.H{"providers": providers}, c)
}

// OAuthLogin redirects the browser to the configured authorization endpoint.
func (api *SysAuthApi) OAuthLogin(c *gin.Context) {
	providerName := c.Param("provider")
	auth := global.GVA_CONFIG.Auth

	callbackURL := oauthProviderCallbackURL(providerName, auth)
	authURL, stateToken, err := systemservice.ServiceGroupApp.SysAuthService.OAuthLoginURL(providerName, callbackURL)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}

	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(config.StateCookiePrefix+providerName, stateToken, int(oauthStateTTLSeconds(auth)), "/", "", auth.SessionCookieSecure, true)
	c.Redirect(http.StatusFound, authURL)
}

// OAuthCallback completes the external login and establishes the same SysSession
// used by local and remote password authentication.
func (api *SysAuthApi) OAuthCallback(c *gin.Context) {
	providerName := c.Param("provider")
	auth := global.GVA_CONFIG.Auth
	stateCookieName := config.StateCookiePrefix + providerName

	stateCookie, _ := c.Cookie(stateCookieName)
	stateQuery := c.Query("state")
	api.clearOAuthCookie(c, stateCookieName)

	if stateCookie == "" || stateQuery == "" || stateCookie != stateQuery {
		api.redirectOAuthError(c, auth, errors.New("invalid oauth state"))
		return
	}

	service := systemservice.ServiceGroupApp.SysAuthService
	result, registered, err := service.OAuthCallback(providerName, c.Query("code"), stateQuery)
	if err != nil {
		api.redirectOAuthError(c, auth, err)
		return
	}
	if err := service.AttachSessionMetadata(c.Request.Context(), result.SessionToken, c.ClientIP(), c.GetHeader("User-Agent")); err != nil {
		_ = service.Logout(c.Request.Context(), result.SessionToken)
		api.redirectOAuthError(c, auth, errors.New("创建安全会话失败"))
		return
	}

	writeSessionCookie(c, result.SessionToken)
	result.SessionToken = ""
	_ = systemservice.ServiceGroupApp.SysAuditService.RecordAudit(c.Request.Context(), systemservice.AuditEntry{
		UserID:     result.User.ID,
		Action:     "authenticate",
		Resource:   c.FullPath(),
		Method:     c.Request.Method,
		Path:       c.Request.URL.Path,
		Result:     "success",
		StatusCode: http.StatusOK,
		ClientIP:   c.ClientIP(),
	})
	redirectParams := map[string]string{"provider": providerName}
	if registered {
		redirectParams["oauth_registered"] = "1"
	}
	api.redirectToFrontend(c, auth, auth.PostLoginRedirect, redirectParams)
}

func (api *SysAuthApi) clearOAuthCookie(c *gin.Context, name string) {
	auth := global.GVA_CONFIG.Auth
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(name, "", -1, "/", "", auth.SessionCookieSecure, true)
}

func (api *SysAuthApi) redirectToFrontend(c *gin.Context, auth config.Auth, redirectPath string, params map[string]string) {
	base := strings.TrimSuffix(auth.FrontendBaseURL, "/")
	if base == "" {
		response.OkWithDetailed(gin.H{"redirect": redirectPath}, "登录成功", c)
		return
	}

	target, err := url.Parse(base + strutil.FirstNonEmpty(redirectPath, "/"))
	if err != nil {
		response.FailWithMessage("redirect target is invalid", c)
		return
	}
	query := target.Query()
	for key, value := range params {
		if value != "" {
			query.Set(key, value)
		}
	}
	target.RawQuery = query.Encode()
	c.Redirect(http.StatusFound, target.String())
}

func (api *SysAuthApi) redirectOAuthError(c *gin.Context, auth config.Auth, err error) {
	message := "OAuth 登录失败"
	if err != nil {
		message = err.Error()
	}
	api.redirectToFrontend(c, auth, strutil.FirstNonEmpty(auth.PostErrorRedirect, "/login"), map[string]string{"error": message})
}

func oauthProviderCallbackURL(providerName string, auth config.Auth) string {
	if provider, ok := auth.OAuthProviders[providerName]; ok && provider.RedirectURL != "" {
		return strings.TrimSuffix(provider.RedirectURL, "/")
	}
	return strings.TrimSuffix(auth.PublicBaseURL, "/") + "/auth/oauth/" + providerName + "/callback"
}

func oauthStateTTLSeconds(auth config.Auth) int64 {
	seconds := int64(auth.StateTTL.Seconds())
	if seconds <= 0 {
		return 10 * 60
	}
	return seconds
}
