package request

// SysModelSettingUpdate 是当前用户保存模型连接配置的请求体。
// 留空 api_key 与 ocr_api_key 表示保留已有密钥；页面永远不会收到旧密钥明文。
type SysModelSettingUpdate struct {
	BaseURL      string `json:"base_url"`
	APIKey       string `json:"api_key"`
	ModelDefault string `json:"model_default"`

	OCRBaseURL string `json:"ocr_base_url"`
	OCRAPIKey  string `json:"ocr_api_key"`
	OCRModel   string `json:"ocr_model"`
}
