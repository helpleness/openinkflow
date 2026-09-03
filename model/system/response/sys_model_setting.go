package response

// SysModelSettingView 是模型配置页面的安全响应。密钥仅以是否已保存的布尔值表示。
type SysModelSettingView struct {
	BaseURL      string `json:"base_url"`
	HasAPIKey    bool   `json:"has_api_key"`
	ModelDefault string `json:"model_default"`

	OCRBaseURL   string `json:"ocr_base_url"`
	HasOCRAPIKey bool   `json:"has_ocr_api_key"`
	OCRModel     string `json:"ocr_model"`
}
