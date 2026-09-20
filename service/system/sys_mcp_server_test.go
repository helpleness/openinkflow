package system

import (
	"context"
	"testing"
	"time"

	"InkFlow/global"
	"InkFlow/internal/ai/mcpclient"
	model "InkFlow/model/system"
	request "InkFlow/model/system/request"
	"InkFlow/utils/securestore"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestSysMCPServerServicePersistsAndResolvesRemoteConfiguration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open SQLite: %v", err)
	}
	if err := db.AutoMigrate(&model.SysMCPServer{}); err != nil {
		t.Fatalf("migrate MCP server table: %v", err)
	}
	previousDB := global.GVA_DB
	previousJWTSecret := global.GVA_CONFIG.Auth.JWTSecret
	global.GVA_DB = db
	global.GVA_CONFIG.Auth.JWTSecret = "mcp-server-test-secret"
	t.Cleanup(func() {
		global.GVA_DB = previousDB
		global.GVA_CONFIG.Auth.JWTSecret = previousJWTSecret
	})

	service := SysMCPServerService{}
	enabled := true
	initialToken := "initial-token"
	created, err := service.Create(context.Background(), 7, 11, request.SysMCPServerCreate{
		Name:           "calculator",
		EndpointURL:    "https://mcp.example.com/v1/mcp",
		BearerToken:    &initialToken,
		TimeoutSeconds: 15,
		Enabled:        &enabled,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	t.Cleanup(func() {
		_ = securestore.New().Delete(mcpBearerTokenKey(7, 11, created.ID))
	})
	if created.ID == 0 || !created.HasBearerToken || created.TimeoutSeconds != 15 || created.Transport != mcpTransportStreamableHTTP {
		t.Fatalf("Create() = %#v", created)
	}

	items, err := service.List(context.Background(), 7, 11)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 1 || items[0].Name != "calculator" || items[0].EndpointURL != "https://mcp.example.com/v1/mcp" {
		t.Fatalf("List() = %#v", items)
	}
	otherUserItems, err := service.List(context.Background(), 7, 12)
	if err != nil {
		t.Fatalf("List(other user) error = %v", err)
	}
	if len(otherUserItems) != 0 {
		t.Fatalf("List(other user) = %#v, want no records", otherUserItems)
	}

	endpoint := "https://mcp2.example.com/mcp"
	updatedToken := "updated-token"
	timeout := 30
	updated, err := service.Update(context.Background(), 7, 11, created.ID, request.SysMCPServerUpdate{
		EndpointURL:    &endpoint,
		BearerToken:    &updatedToken,
		TimeoutSeconds: &timeout,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.EndpointURL != endpoint || !updated.HasBearerToken || updated.TimeoutSeconds != timeout {
		t.Fatalf("Update() = %#v", updated)
	}

	resolved, err := service.Resolve(context.Background(), 7, 11, created.ID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	wantConfig := mcpclient.Config{
		Endpoint:    endpoint,
		BearerToken: updatedToken,
		Timeout:     30 * time.Second,
	}
	if resolved != wantConfig {
		t.Fatalf("Resolve() = %#v, want %#v", resolved, wantConfig)
	}

	if _, err := service.Create(context.Background(), 7, 11, request.SysMCPServerCreate{
		Name:        "unsafe",
		EndpointURL: "http://127.0.0.1:8080/mcp",
	}); err == nil {
		t.Fatal("Create() allowed an unsafe local HTTP endpoint")
	}

	disabled := false
	if _, err := service.Update(context.Background(), 7, 11, created.ID, request.SysMCPServerUpdate{Enabled: &disabled}); err != nil {
		t.Fatalf("disable MCP server: %v", err)
	}
	if _, err := service.Resolve(context.Background(), 7, 11, created.ID); err == nil {
		t.Fatal("Resolve() with disabled server succeeded, want an error")
	}

	if err := service.Delete(context.Background(), 7, 11, created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	items, err = service.List(context.Background(), 7, 11)
	if err != nil {
		t.Fatalf("List() after delete error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("List() after delete = %#v, want no records", items)
	}
}
