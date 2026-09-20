package system

import (
	v1 "InkFlow/api/v1/system"
	"InkFlow/middleware"

	"github.com/gin-gonic/gin"
)

// SysAuthRouter binds authentication and session APIs.
type SysAuthRouter struct{}

func (router *SysAuthRouter) InitSysAuthRouter(Router, PublicRouter *gin.RouterGroup) {
	sysAuthApi := v1.ApiGroupApp.SysAuthApi

	authRouter := PublicRouter.Group("/auth")
	authPrivateRouter := Router.Group("/auth")
	// MFA settings and device-session management affect an authenticated
	// account. They therefore require both an explicit tenant context and the
	// role's Casbin API permission. /me and /logout deliberately remain
	// session-only: the client needs /me before it has selected a tenant, and a
	// user must always be able to terminate their own session.
	personalCenterRouter := Router.Group("/auth").Use(middleware.RequireTenant(), middleware.SystemAuthorize())
	{
		authRouter.GET("/captcha", sysAuthApi.Captcha)
		authRouter.POST("/local/register", sysAuthApi.RegisterLocal)
		authRouter.POST("/local/login", sysAuthApi.LoginLocal)
		authRouter.POST("/remote/register", sysAuthApi.RegisterRemote)
		authRouter.POST("/remote/login", sysAuthApi.LoginRemote)
		authRouter.POST("/mfa/pending/setup", sysAuthApi.SetupPendingMFA)
		authRouter.POST("/mfa/pending/complete", sysAuthApi.CompletePendingMFA)
		authRouter.GET("/oauth/providers", sysAuthApi.GetOAuthProviders)
		authRouter.GET("/oauth/:provider/login", sysAuthApi.OAuthLogin)
		authRouter.GET("/oauth/:provider/callback", sysAuthApi.OAuthCallback)
	}
	{
		authPrivateRouter.GET("/me", sysAuthApi.Me)
		authPrivateRouter.POST("/logout", sysAuthApi.Logout)
	}
	{
		personalCenterRouter.POST("/mfa/setup", sysAuthApi.SetupMFA)
		personalCenterRouter.POST("/mfa/enable", sysAuthApi.EnableMFA)
		personalCenterRouter.POST("/mfa/disable", sysAuthApi.DisableMFA)
		personalCenterRouter.GET("/sessions", sysAuthApi.ListSessions)
		personalCenterRouter.DELETE("/sessions/:session_id", sysAuthApi.RevokeSession)
		personalCenterRouter.POST("/sessions/revoke-others", sysAuthApi.RevokeOtherSessions)
	}
}
