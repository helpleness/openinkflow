package system

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"InkFlow/config"
	"InkFlow/global"
	model "InkFlow/model/system"
	request "InkFlow/model/system/request"
	response "InkFlow/model/system/response"
	"InkFlow/utils/securestore"

	"gorm.io/gorm"
)

// SysModelSettingService 管理当前用户的模型连接元数据与安全加密的密钥。
type SysModelSettingService struct{}

// Get 返回当前用户在当前租户中的模型配置。未保存过时返回空配置，调用方可按需回退到
// 客户端默认 LLM 配置；不会把全局 API Key 暴露给任意用户。
func (service *SysModelSettingService) Get(ctx context.Context, tenantID, userID uint) (response.SysModelSettingView, error) {
	setting, err := service.find(ctx, tenantID, userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return response.SysModelSettingView{}, nil
	}
	if err != nil {
		return response.SysModelSettingView{}, err
	}
	return response.SysModelSettingView{
		BaseURL:      setting.BaseURL,
		HasAPIKey:    hasModelCredential(setting, tenantID, userID, "primary"),
		ModelDefault: setting.ModelDefault,
		OCRBaseURL:   setting.OCRBaseURL,
		HasOCRAPIKey: hasModelCredential(setting, tenantID, userID, "ocr"),
		OCRModel:     setting.OCRModel,
	}, nil
}

// Update 保存当前用户的模型配置。密钥字段留空时不覆盖已有凭据，避免“重新读取 → 保存”
// 意外清除密钥。主模型与 OCR 总结模型都使用 OpenAI Chat Completions 兼容地址。
func (service *SysModelSettingService) Update(ctx context.Context, tenantID, userID uint, input request.SysModelSettingUpdate) (response.SysModelSettingView, error) {
	input.BaseURL = strings.TrimSpace(input.BaseURL)
	input.ModelDefault = strings.TrimSpace(input.ModelDefault)
	input.OCRBaseURL = strings.TrimSpace(input.OCRBaseURL)
	input.OCRModel = strings.TrimSpace(input.OCRModel)
	if err := validateModelBaseURL(input.BaseURL); err != nil {
		return response.SysModelSettingView{}, fmt.Errorf("主模型地址无效: %w", err)
	}
	if err := validateModelBaseURL(input.OCRBaseURL); err != nil {
		return response.SysModelSettingView{}, fmt.Errorf("OCR 语义模型地址无效: %w", err)
	}

	db := global.GVA_DB
	setting, err := service.find(ctx, tenantID, userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		setting = model.SysModelSetting{TenantID: tenantID, UserID: userID}
		if err := db.WithContext(ctx).Create(&setting).Error; err != nil {
			return response.SysModelSettingView{}, err
		}
	} else if err != nil {
		return response.SysModelSettingView{}, err
	}

	updates := map[string]any{
		"base_url":      input.BaseURL,
		"model_default": input.ModelDefault,
		"ocr_base_url":  input.OCRBaseURL,
		"ocr_model":     input.OCRModel,
	}
	if err := db.WithContext(ctx).Model(&setting).Updates(updates).Error; err != nil {
		return response.SysModelSettingView{}, err
	}
	if key := strings.TrimSpace(input.APIKey); key != "" {
		if err := saveModelCredential(ctx, &setting, tenantID, userID, "primary", key); err != nil {
			return response.SysModelSettingView{}, fmt.Errorf("保存主模型密钥: %w", err)
		}
	}
	if key := strings.TrimSpace(input.OCRAPIKey); key != "" {
		if err := saveModelCredential(ctx, &setting, tenantID, userID, "ocr", key); err != nil {
			return response.SysModelSettingView{}, fmt.Errorf("保存 OCR 语义模型密钥: %w", err)
		}
	}
	return service.Get(ctx, tenantID, userID)
}

func (service *SysModelSettingService) find(ctx context.Context, tenantID, userID uint) (model.SysModelSetting, error) {
	var setting model.SysModelSetting
	err := global.GVA_DB.WithContext(ctx).Where("tenant_id = ? AND user_id = ?", tenantID, userID).First(&setting).Error
	return setting, err
}

// ResolveOCRSemanticLLM 返回导入图片时要使用的语义模型。OCR 专用字段为空时会
// 回退到同一租户、同一用户的主模型；凭据优先从系统凭据管理器读取，在服务端会从
// 加密数据库回退读取，且永远不会出现在接口响应中。
func (service *SysModelSettingService) ResolveOCRSemanticLLM(ctx context.Context, tenantID, userID uint) (config.LLM, error) {
	resolved, err := service.ResolvePrimaryLLM(ctx, tenantID, userID)
	if err != nil {
		return config.LLM{}, err
	}
	setting, err := service.find(ctx, tenantID, userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return resolved, nil
	}
	if err != nil {
		return config.LLM{}, err
	}
	if value := strings.TrimSpace(setting.OCRBaseURL); value != "" {
		resolved.BaseUrl = value
	}
	if value := strings.TrimSpace(setting.OCRModel); value != "" {
		resolved.ModelDefault = value
	}
	if key, loadErr := loadModelCredential(setting, tenantID, userID, "ocr"); loadErr != nil {
		return config.LLM{}, fmt.Errorf("读取 OCR 语义模型密钥: %w", loadErr)
	} else if len(key) > 0 {
		resolved.ApiKey = string(key)
	}
	return resolved, nil
}

// ResolvePrimaryLLM returns the caller's primary writing model. Credentials stay
// in secure storage and never enter an API response or a writing-task record.
func (service *SysModelSettingService) ResolvePrimaryLLM(ctx context.Context, tenantID, userID uint) (config.LLM, error) {
	resolved := global.GVA_CONFIG.LLM
	setting, err := service.find(ctx, tenantID, userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return resolved, nil
	}
	if err != nil {
		return config.LLM{}, err
	}
	if value := strings.TrimSpace(setting.BaseURL); value != "" {
		resolved.BaseUrl = value
	}
	if value := strings.TrimSpace(setting.ModelDefault); value != "" {
		resolved.ModelDefault = value
	}
	if key, loadErr := loadModelCredential(setting, tenantID, userID, "primary"); loadErr != nil {
		return config.LLM{}, fmt.Errorf("读取主模型密钥: %w", loadErr)
	} else if len(key) > 0 {
		resolved.ApiKey = string(key)
	}
	return resolved, nil
}

func validateModelBaseURL(rawURL string) error {
	if rawURL == "" {
		return nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return errors.New("必须是 http 或 https 地址")
	}
	return nil
}

func modelCredentialKey(tenantID, userID uint, kind string) string {
	return fmt.Sprintf("InkFlow.System.ModelSetting/%d/%d/%s", tenantID, userID, kind)
}

func saveModelCredential(ctx context.Context, setting *model.SysModelSetting, tenantID, userID uint, kind, value string) error {
	if err := securestore.New().Save(modelCredentialKey(tenantID, userID, kind), []byte(value)); err == nil {
		return nil
	} else if !errors.Is(err, securestore.ErrUnsupported) {
		return err
	}

	encrypted, err := securestore.EncryptWithSecret(global.GVA_CONFIG.Auth.JWTSecret, []byte(value))
	if err != nil {
		return err
	}
	column := "primary_api_key_encrypted"
	if kind == "ocr" {
		column = "ocr_api_key_encrypted"
		setting.OCRAPIKeyEncrypted = encrypted
	} else {
		setting.PrimaryAPIKeyEncrypted = encrypted
	}
	return global.GVA_DB.WithContext(ctx).Model(setting).Update(column, encrypted).Error
}

func loadModelCredential(setting model.SysModelSetting, tenantID, userID uint, kind string) ([]byte, error) {
	value, err := securestore.New().Load(modelCredentialKey(tenantID, userID, kind))
	if err == nil && len(value) > 0 {
		return value, nil
	}
	if err != nil && !errors.Is(err, securestore.ErrNotFound) && !errors.Is(err, securestore.ErrUnsupported) {
		return nil, err
	}

	encrypted := setting.PrimaryAPIKeyEncrypted
	if kind == "ocr" {
		encrypted = setting.OCRAPIKeyEncrypted
	}
	if encrypted == "" {
		return nil, nil
	}
	return securestore.DecryptWithSecret(global.GVA_CONFIG.Auth.JWTSecret, encrypted)
}

func hasModelCredential(setting model.SysModelSetting, tenantID, userID uint, kind string) bool {
	value, err := loadModelCredential(setting, tenantID, userID, kind)
	return err == nil && len(value) > 0
}
