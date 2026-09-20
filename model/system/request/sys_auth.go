package request

// SysAuthCredentials is the request body for local and remote authentication.
type SysAuthCredentials struct {
	Username      string `json:"username" binding:"required"`
	Password      string `json:"password" binding:"required"`
	CaptchaID     string `json:"captcha_id" binding:"required"`
	CaptchaAnswer string `json:"captcha_answer" binding:"required"`
}

type SysMFAEnable struct {
	Code string `json:"code" binding:"required"`
}

type SysMFADisable struct {
	Password string `json:"password" binding:"required"`
	Code     string `json:"code" binding:"required"`
}

// SysMFAPendingComplete proves a user has completed the password and CAPTCHA
// stage, but has not received a normal browser or desktop session yet.
type SysMFAPendingComplete struct {
	PendingToken string `json:"pending_token" binding:"required"`
	Code         string `json:"code" binding:"required"`
}

type SysMFAPendingSetup struct {
	PendingToken string `json:"pending_token" binding:"required"`
}
