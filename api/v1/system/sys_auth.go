package system

import (
	"net/http"
	"strings"
	"time"

	"InkFlow/global"
	commonResponse "InkFlow/model/common/response"
	request "InkFlow/model/system/request"
	response "InkFlow/model/system/response"
	systemService "InkFlow/service/system"
	"InkFlow/utils/ginctx"

	"github.com/gin-gonic/gin"
)

// SysAuthApi handles authentication and session HTTP requests.
type SysAuthApi struct{}

// RegisterLocal 注册本地账号并创建登录会话。
func (api *SysAuthApi) RegisterLocal(c *gin.Context) {
	authenticate(c, func(req request.SysAuthCredentials) (*response.SysAuthResult, error) {
		return systemService.ServiceGroupApp.SysAuthService.RegisterLocal(c.Request.Context(), req.Username, req.Password)
	})
}

// LoginLocal 使用本地账号密码完成登录。
func (api *SysAuthApi) LoginLocal(c *gin.Context) {
	authenticate(c, func(req request.SysAuthCredentials) (*response.SysAuthResult, error) {
		return systemService.ServiceGroupApp.SysAuthService.BeginLocalLogin(c.Request.Context(), req.Username, req.Password)
	})
}

// RegisterRemote 通过远端认证服务注册账号并创建本地会话。
func (api *SysAuthApi) RegisterRemote(c *gin.Context) {
	authenticate(c, func(req request.SysAuthCredentials) (*response.SysAuthResult, error) {
		return systemService.ServiceGroupApp.SysAuthService.AuthenticateRemote(c.Request.Context(), "register", req.Username, req.Password, "")
	})
}

// LoginRemote 通过远端认证服务验证账号并创建本地会话。
func (api *SysAuthApi) LoginRemote(c *gin.Context) {
	authenticate(c, func(req request.SysAuthCredentials) (*response.SysAuthResult, error) {
		return systemService.ServiceGroupApp.SysAuthService.AuthenticateRemote(c.Request.Context(), "login", req.Username, req.Password, "")
	})
}

// Me 返回当前已认证用户的信息。
func (api *SysAuthApi) Me(c *gin.Context) {
	commonResponse.OkWithData(c.MustGet("system_user"), c)
}

// Logout 使当前会话令牌失效。
func (api *SysAuthApi) Logout(c *gin.Context) {
	_ = systemService.ServiceGroupApp.SysAuthService.Logout(c.Request.Context(), ginctx.SessionToken(c))
	clearSessionCookie(c)
	commonResponse.OkWithData(gin.H{}, c)
}

// Captcha creates a short-lived, one-time image challenge for login and
// registration. The answer is never included in the database or API response.
func (api *SysAuthApi) Captcha(c *gin.Context) {
	result, err := systemService.ServiceGroupApp.SysAuthService.NewCaptcha(c.Request.Context())
	if err != nil {
		commonResponse.ResultWithStatus(http.StatusInternalServerError, http.StatusInternalServerError, nil, "暂时无法生成图片验证码", c)
		return
	}
	commonResponse.OkWithData(result, c)
}

// SetupMFA creates a pending TOTP secret. It becomes active only after the
// caller proves possession through EnableMFA.
func (api *SysAuthApi) SetupMFA(c *gin.Context) {
	result, err := systemService.ServiceGroupApp.SysAuthService.SetupMFA(c.Request.Context(), ginctx.CurrentUserID(c))
	if err != nil {
		commonResponse.BadRequest(err.Error(), c)
		return
	}
	commonResponse.OkWithData(result, c)
}

// SetupPendingMFA exposes an enrollment secret only after primary credentials
// and CAPTCHA have been proved; it deliberately does not create a session.
func (api *SysAuthApi) SetupPendingMFA(c *gin.Context) {
	var req request.SysMFAPendingSetup
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("登录验证标识无效", c)
		return
	}
	result, err := systemService.ServiceGroupApp.SysAuthService.SetupPendingMFA(c.Request.Context(), req.PendingToken)
	if err != nil {
		commonResponse.Unauthorized(err.Error(), c)
		return
	}
	commonResponse.OkWithData(result, c)
}

// CompletePendingMFA turns a short-lived post-password login proof into a
// normal session only after a TOTP verification or required enrollment.
func (api *SysAuthApi) CompletePendingMFA(c *gin.Context) {
	var req request.SysMFAPendingComplete
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("请输入 6 位动态验证码", c)
		return
	}
	service := systemService.ServiceGroupApp.SysAuthService
	if !service.AllowLoginAttempt(c.ClientIP()) {
		commonResponse.ResultWithStatus(http.StatusTooManyRequests, http.StatusTooManyRequests, nil, "登录验证请求过于频繁，请稍后再试", c)
		return
	}
	result, err := service.CompletePendingMFA(c.Request.Context(), req.PendingToken, req.Code)
	if err != nil {
		commonResponse.Unauthorized(err.Error(), c)
		return
	}
	finishAuthentication(c, result)
}

func (api *SysAuthApi) EnableMFA(c *gin.Context) {
	var req request.SysMFAEnable
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("动态验证码不能为空", c)
		return
	}
	if err := systemService.ServiceGroupApp.SysAuthService.EnableMFA(c.Request.Context(), ginctx.CurrentUserID(c), req.Code); err != nil {
		commonResponse.BadRequest(err.Error(), c)
		return
	}
	commonResponse.OkWithMessage("多重验证已启用", c)
}

func (api *SysAuthApi) DisableMFA(c *gin.Context) {
	var req request.SysMFADisable
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("请输入密码和动态验证码", c)
		return
	}
	if err := systemService.ServiceGroupApp.SysAuthService.DisableMFA(c.Request.Context(), ginctx.CurrentUserID(c), req.Password, req.Code); err != nil {
		commonResponse.BadRequest(err.Error(), c)
		return
	}
	clearSessionCookie(c)
	commonResponse.OkWithMessage("多重验证已关闭；所有设备需要重新登录", c)
}

func (api *SysAuthApi) ListSessions(c *gin.Context) {
	items, err := systemService.ServiceGroupApp.SysAuthService.ListSessions(c.Request.Context(), ginctx.CurrentUserID(c), ginctx.SessionToken(c))
	if err != nil {
		commonResponse.BadRequest(err.Error(), c)
		return
	}
	commonResponse.OkWithData(items, c)
}

func (api *SysAuthApi) RevokeSession(c *gin.Context) {
	var uri struct {
		SessionID uint `uri:"session_id" binding:"required"`
	}
	if err := c.ShouldBindUri(&uri); err != nil {
		commonResponse.BadRequest("会话标识无效", c)
		return
	}
	current := false
	if sessions, err := systemService.ServiceGroupApp.SysAuthService.ListSessions(c.Request.Context(), ginctx.CurrentUserID(c), ginctx.SessionToken(c)); err == nil {
		for _, session := range sessions {
			if session.ID == uri.SessionID {
				current = session.Current
				break
			}
		}
	}
	if err := systemService.ServiceGroupApp.SysAuthService.RevokeSession(c.Request.Context(), ginctx.CurrentUserID(c), uri.SessionID); err != nil {
		commonResponse.BadRequest("会话不存在或无权操作", c)
		return
	}
	if current {
		clearSessionCookie(c)
	}
	commonResponse.OkWithData(gin.H{"current": current}, c)
}

func (api *SysAuthApi) RevokeOtherSessions(c *gin.Context) {
	if err := systemService.ServiceGroupApp.SysAuthService.RevokeOtherSessions(c.Request.Context(), ginctx.CurrentUserID(c), ginctx.SessionToken(c)); err != nil {
		commonResponse.BadRequest(err.Error(), c)
		return
	}
	commonResponse.OkWithMessage("其他设备会话已撤销", c)
}

// authenticate 统一处理认证请求的参数校验、审计记录和响应输出。
func authenticate(c *gin.Context, action func(request.SysAuthCredentials) (*response.SysAuthResult, error)) {
	var req request.SysAuthCredentials
	if err := c.ShouldBindJSON(&req); err != nil {
		commonResponse.BadRequest("请求参数无效", c)
		return
	}
	service := systemService.ServiceGroupApp.SysAuthService
	if !service.AllowLoginAttempt(c.ClientIP()) {
		commonResponse.ResultWithStatus(http.StatusTooManyRequests, http.StatusTooManyRequests, nil, "登录请求过于频繁，请稍后再试", c)
		return
	}
	if err := service.VerifyCaptcha(c.Request.Context(), req.CaptchaID, req.CaptchaAnswer); err != nil {
		commonResponse.Unauthorized(err.Error(), c)
		return
	}

	result, err := action(req)
	if err != nil {
		commonResponse.Unauthorized(err.Error(), c)
		return
	}
	if strings.TrimSpace(result.PendingToken) != "" {
		// Password verification has succeeded, but this is not a login yet: no
		// cookie is set until the independent MFA page is completed.
		commonResponse.OkWithData(result, c)
		return
	}
	finishAuthentication(c, result)
}

func finishAuthentication(c *gin.Context, result *response.SysAuthResult) {
	if result == nil || result.User == nil || strings.TrimSpace(result.SessionToken) == "" {
		commonResponse.ResultWithStatus(http.StatusInternalServerError, http.StatusInternalServerError, nil, "创建安全会话失败", c)
		return
	}
	service := systemService.ServiceGroupApp.SysAuthService
	if err := service.AttachSessionMetadata(c.Request.Context(), result.SessionToken, c.ClientIP(), c.GetHeader("User-Agent")); err != nil {
		_ = service.Logout(c.Request.Context(), result.SessionToken)
		commonResponse.ResultWithStatus(http.StatusInternalServerError, http.StatusInternalServerError, nil, "创建安全会话失败", c)
		return
	}
	writeSessionCookie(c, result.SessionToken)
	// Browser code authenticates solely through the HttpOnly cookie. The
	// desktop shell identifies itself explicitly and retains its in-memory
	// loopback handoff token.
	if !isDesktopAuthRequest(c) {
		result.SessionToken = ""
	}
	_ = systemService.ServiceGroupApp.SysAuditService.RecordAudit(c.Request.Context(), systemService.AuditEntry{
		UserID:     result.User.ID,
		Action:     "authenticate",
		Resource:   c.FullPath(),
		Method:     c.Request.Method,
		Path:       c.Request.URL.Path,
		Result:     "success",
		StatusCode: 200,
		ClientIP:   c.ClientIP(),
	})
	commonResponse.OkWithData(result, c)
}

func isDesktopAuthRequest(c *gin.Context) bool {
	return strings.TrimSpace(c.GetHeader("X-InkFlow-Desktop-Client")) == "1"
}

func sessionCookieName() string {
	if value := strings.TrimSpace(global.GVA_CONFIG.Auth.SessionCookieName); value != "" {
		return value
	}
	return "inkflow_session"
}

func writeSessionCookie(c *gin.Context, token string) {
	c.SetSameSite(sessionCookieSameSite())
	c.SetCookie(sessionCookieName(), token, int(sessionCookieTTL().Seconds()), "/", "", global.GVA_CONFIG.Auth.SessionCookieSecure, true)
}

func clearSessionCookie(c *gin.Context) {
	c.SetSameSite(sessionCookieSameSite())
	c.SetCookie(sessionCookieName(), "", -1, "/", "", global.GVA_CONFIG.Auth.SessionCookieSecure, true)
}

func sessionCookieTTL() time.Duration {
	if hours := global.GVA_CONFIG.Auth.SessionTTLHours; hours > 0 {
		return time.Duration(hours) * time.Hour
	}
	return 24 * time.Hour
}

func sessionCookieSameSite() http.SameSite {
	switch strings.ToLower(strings.TrimSpace(global.GVA_CONFIG.Auth.SessionCookieSameSite)) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}
