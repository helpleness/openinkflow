package system

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"InkFlow/global"
	"InkFlow/internal/ai/mcpclient"
	securehttp "InkFlow/internal/http"
	model "InkFlow/model/system"
	request "InkFlow/model/system/request"
	response "InkFlow/model/system/response"
	"InkFlow/utils/securestore"
)

const (
	mcpTransportStreamableHTTP = "streamable_http"
	defaultMCPTimeoutSeconds   = 60
	maxMCPTimeoutSeconds       = 600
)

// SysMCPServerService persists and resolves a user's remote Streamable HTTP
// MCP connections. It has no facility for launching a local command.
type SysMCPServerService struct{}

func (s *SysMCPServerService) List(ctx context.Context, tenantID, userID uint) ([]response.SysMCPServerView, error) {
	var servers []model.SysMCPServer
	if err := global.GVA_DB.WithContext(ctx).
		Where("tenant_id = ? AND user_id = ?", tenantID, userID).
		Order("name ASC, id ASC").
		Find(&servers).Error; err != nil {
		return nil, err
	}

	views := make([]response.SysMCPServerView, 0, len(servers))
	for _, server := range servers {
		view, err := s.view(server, tenantID, userID)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *SysMCPServerService) Create(ctx context.Context, tenantID, userID uint, input request.SysMCPServerCreate) (response.SysMCPServerView, error) {
	if tenantID == 0 || userID == 0 {
		return response.SysMCPServerView{}, errors.New("工作空间或用户无效")
	}
	server := model.SysMCPServer{
		TenantID:  tenantID,
		UserID:    userID,
		Transport: mcpTransportStreamableHTTP,
		Enabled:   true,
	}
	if input.Enabled != nil {
		server.Enabled = *input.Enabled
	}
	if err := setMCPServerFields(&server, input.Name, input.EndpointURL, input.TimeoutSeconds); err != nil {
		return response.SysMCPServerView{}, err
	}
	if err := global.GVA_DB.WithContext(ctx).Create(&server).Error; err != nil {
		return response.SysMCPServerView{}, err
	}
	if input.BearerToken != nil {
		if err := saveMCPBearerToken(ctx, &server, *input.BearerToken); err != nil {
			return response.SysMCPServerView{}, fmt.Errorf("保存 MCP Bearer Token: %w", err)
		}
	}
	return s.view(server, tenantID, userID)
}

func (s *SysMCPServerService) Update(ctx context.Context, tenantID, userID, serverID uint, input request.SysMCPServerUpdate) (response.SysMCPServerView, error) {
	server, err := s.find(ctx, tenantID, userID, serverID)
	if err != nil {
		return response.SysMCPServerView{}, err
	}
	if input.Name != nil {
		server.Name = strings.TrimSpace(*input.Name)
	}
	if input.EndpointURL != nil {
		server.EndpointURL = strings.TrimSpace(*input.EndpointURL)
	}
	if input.TimeoutSeconds != nil {
		timeout, err := normalizeMCPTimeout(*input.TimeoutSeconds)
		if err != nil {
			return response.SysMCPServerView{}, err
		}
		server.TimeoutSeconds = timeout
	}
	if input.Enabled != nil {
		server.Enabled = *input.Enabled
	}
	if err := validateMCPServer(server); err != nil {
		return response.SysMCPServerView{}, err
	}
	if err := global.GVA_DB.WithContext(ctx).Save(&server).Error; err != nil {
		return response.SysMCPServerView{}, err
	}
	if input.BearerToken != nil {
		if err := saveMCPBearerToken(ctx, &server, *input.BearerToken); err != nil {
			return response.SysMCPServerView{}, fmt.Errorf("保存 MCP Bearer Token: %w", err)
		}
	}
	return s.view(server, tenantID, userID)
}

// Delete permanently removes the remote endpoint and its saved bearer token.
func (s *SysMCPServerService) Delete(ctx context.Context, tenantID, userID, serverID uint) error {
	server, err := s.find(ctx, tenantID, userID, serverID)
	if err != nil {
		return err
	}
	if err := clearMCPBearerToken(ctx, &server); err != nil {
		return fmt.Errorf("删除 MCP Bearer Token: %w", err)
	}
	return global.GVA_DB.WithContext(ctx).Unscoped().Delete(&server).Error
}

// Resolve converts one enabled database record into a remote-only runtime
// configuration. No database field is ever treated as an executable path.
func (s *SysMCPServerService) Resolve(ctx context.Context, tenantID, userID, serverID uint) (mcpclient.Config, error) {
	server, err := s.find(ctx, tenantID, userID, serverID)
	if err != nil {
		return mcpclient.Config{}, err
	}
	if !server.Enabled {
		return mcpclient.Config{}, errors.New("MCP 服务已禁用")
	}
	if err := validateMCPServer(server); err != nil {
		return mcpclient.Config{}, err
	}
	bearerToken, err := loadMCPBearerToken(server, tenantID, userID)
	if err != nil {
		return mcpclient.Config{}, fmt.Errorf("读取 MCP Bearer Token: %w", err)
	}
	return mcpclient.Config{
		Endpoint:    server.EndpointURL,
		BearerToken: bearerToken,
		Timeout:     time.Duration(server.TimeoutSeconds) * time.Second,
	}, nil
}

// Connect resolves and connects to an explicitly selected remote MCP endpoint.
func (s *SysMCPServerService) Connect(ctx context.Context, tenantID, userID, serverID uint) (*mcpclient.Client, error) {
	cfg, err := s.Resolve(ctx, tenantID, userID, serverID)
	if err != nil {
		return nil, err
	}
	return mcpclient.Connect(ctx, cfg)
}

func (s *SysMCPServerService) find(ctx context.Context, tenantID, userID, serverID uint) (model.SysMCPServer, error) {
	if serverID == 0 {
		return model.SysMCPServer{}, errors.New("MCP 服务编号无效")
	}
	var server model.SysMCPServer
	err := global.GVA_DB.WithContext(ctx).
		Where("id = ? AND tenant_id = ? AND user_id = ?", serverID, tenantID, userID).
		First(&server).Error
	return server, err
}

func (s *SysMCPServerService) view(server model.SysMCPServer, tenantID, userID uint) (response.SysMCPServerView, error) {
	bearerToken, err := loadMCPBearerToken(server, tenantID, userID)
	if err != nil {
		return response.SysMCPServerView{}, fmt.Errorf("读取 MCP 服务 %q 的 Bearer Token: %w", server.Name, err)
	}
	return response.SysMCPServerView{
		ID:             server.ID,
		Name:           server.Name,
		Transport:      server.Transport,
		EndpointURL:    server.EndpointURL,
		HasBearerToken: bearerToken != "",
		TimeoutSeconds: server.TimeoutSeconds,
		Enabled:        server.Enabled,
		CreatedAt:      server.CreatedAt,
		UpdatedAt:      server.UpdatedAt,
	}, nil
}

func setMCPServerFields(server *model.SysMCPServer, name, endpointURL string, timeout int) error {
	server.Name = strings.TrimSpace(name)
	server.EndpointURL = strings.TrimSpace(endpointURL)
	normalizedTimeout, err := normalizeMCPTimeout(timeout)
	if err != nil {
		return err
	}
	server.TimeoutSeconds = normalizedTimeout
	return validateMCPServer(*server)
}

func normalizeMCPTimeout(timeout int) (int, error) {
	if timeout == 0 {
		return defaultMCPTimeoutSeconds, nil
	}
	if timeout < 1 || timeout > maxMCPTimeoutSeconds {
		return 0, fmt.Errorf("MCP 超时必须在 1 到 %d 秒之间", maxMCPTimeoutSeconds)
	}
	return timeout, nil
}

func validateMCPServer(server model.SysMCPServer) error {
	if server.Name == "" {
		return errors.New("MCP 服务名称不能为空")
	}
	if len(server.Name) > 128 {
		return errors.New("MCP 服务名称不能超过 128 个字符")
	}
	if server.Transport != mcpTransportStreamableHTTP {
		return fmt.Errorf("不支持的 MCP transport %q", server.Transport)
	}
	if _, err := securehttp.ParsePublicHTTPSURL(server.EndpointURL); err != nil {
		return err
	}
	if _, err := normalizeMCPTimeout(server.TimeoutSeconds); err != nil {
		return err
	}
	return nil
}

func saveMCPBearerToken(ctx context.Context, server *model.SysMCPServer, token string) error {
	token, err := securehttp.NormalizeBearerToken(token)
	if err != nil {
		return err
	}
	if token == "" {
		return clearMCPBearerToken(ctx, server)
	}
	payload := []byte(token)
	store := securestore.New()
	if err := store.Save(mcpBearerTokenKey(server.TenantID, server.UserID, server.ID), payload); err == nil {
		server.AuthTokenEncrypted = ""
		return global.GVA_DB.WithContext(ctx).Model(server).Update("auth_token_encrypted", "").Error
	} else if !errors.Is(err, securestore.ErrUnsupported) {
		return err
	}

	encrypted, err := securestore.EncryptWithSecret(global.GVA_CONFIG.Auth.JWTSecret, payload)
	if err != nil {
		return err
	}
	server.AuthTokenEncrypted = encrypted
	return global.GVA_DB.WithContext(ctx).Model(server).Update("auth_token_encrypted", encrypted).Error
}

func clearMCPBearerToken(ctx context.Context, server *model.SysMCPServer) error {
	if err := securestore.New().Delete(mcpBearerTokenKey(server.TenantID, server.UserID, server.ID)); err != nil && !errors.Is(err, securestore.ErrUnsupported) {
		return err
	}
	server.AuthTokenEncrypted = ""
	return global.GVA_DB.WithContext(ctx).Model(server).Update("auth_token_encrypted", "").Error
}

func loadMCPBearerToken(server model.SysMCPServer, tenantID, userID uint) (string, error) {
	value, err := securestore.New().Load(mcpBearerTokenKey(tenantID, userID, server.ID))
	if err == nil && len(value) > 0 {
		return validatedBearerToken(string(value))
	}
	if err != nil && !errors.Is(err, securestore.ErrNotFound) && !errors.Is(err, securestore.ErrUnsupported) {
		return "", err
	}
	if server.AuthTokenEncrypted == "" {
		return "", nil
	}
	payload, err := securestore.DecryptWithSecret(global.GVA_CONFIG.Auth.JWTSecret, server.AuthTokenEncrypted)
	if err != nil {
		return "", err
	}
	return validatedBearerToken(string(payload))
}

func validatedBearerToken(token string) (string, error) {
	return securehttp.NormalizeBearerToken(token)
}

func mcpBearerTokenKey(tenantID, userID, serverID uint) string {
	return fmt.Sprintf("InkFlow.System.MCPServer/%d/%d/%d/bearer-token", tenantID, userID, serverID)
}
