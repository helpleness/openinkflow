package system

import "gorm.io/gorm"

// SysModelSetting 保存一个用户在一个租户内的 OpenAI 兼容模型连接配置。
//
// API Key 优先保存在系统凭据管理器中。没有系统安全存储的服务端部署会使用
// auth.jwt-secret 加密后写入数据库；接口与审计记录始终不会包含明文密钥。
type SysModelSetting struct {
	gorm.Model
	TenantID uint `json:"tenant_id" gorm:"not null;uniqueIndex:idx_sys_model_settings_owner,priority:1"`
	UserID   uint `json:"user_id" gorm:"not null;uniqueIndex:idx_sys_model_settings_owner,priority:2"`

	BaseURL      string `json:"base_url" gorm:"size:512"`
	ModelDefault string `json:"model_default" gorm:"size:255"`
	// PrimaryAPIKeyEncrypted is only populated on platforms without an OS credential store.
	PrimaryAPIKeyEncrypted string `json:"-" gorm:"type:text"`

	// OCR* 是图片 OCR 文本的可选语义总结模型。三个字段全部为空时复用主模型配置。
	OCRBaseURL         string `json:"ocr_base_url" gorm:"size:512"`
	OCRModel           string `json:"ocr_model" gorm:"size:255"`
	OCRAPIKeyEncrypted string `json:"-" gorm:"type:text"`
}

// TableName 返回系统模型配置表名。
func (SysModelSetting) TableName() string { return "sys_model_settings" }
