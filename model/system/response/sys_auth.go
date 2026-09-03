package response

import model "InkFlow/model/system"

// SysAuthResult is the authentication response returned after a session is created.
type SysAuthResult struct {
	User         *model.SysUser `json:"user"`
	SessionToken string         `json:"session_token"`
	// AuthStage is authenticated, mfa_verify or mfa_enrollment. PendingToken
	// is intentionally not a session and is valid only for the MFA endpoints.
	AuthStage    string `json:"auth_stage"`
	PendingToken string `json:"pending_token"`
}

type SysCaptcha struct {
	CaptchaID string `json:"captcha_id"`
	ImageData string `json:"image_data"`
	ExpiresAt string `json:"expires_at"`
}

type SysMFASetup struct {
	Secret     string `json:"secret"`
	OTPAuthURL string `json:"otpauth_url"`
}

type SysSessionView struct {
	ID         uint   `json:"id"`
	Kind       string `json:"kind"`
	ClientIP   string `json:"client_ip"`
	DeviceName string `json:"device_name"`
	CreatedAt  string `json:"created_at"`
	LastSeenAt string `json:"last_seen_at"`
	ExpiresAt  string `json:"expires_at"`
	Current    bool   `json:"current"`
}
