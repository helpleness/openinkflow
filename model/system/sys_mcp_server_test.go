package system

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSysMCPServerSchemaScopesNamesToTenantAndUser(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open SQLite: %v", err)
	}
	if err := db.AutoMigrate(&SysMCPServer{}); err != nil {
		t.Fatalf("migrate MCP server table: %v", err)
	}
	if !db.Migrator().HasTable(&SysMCPServer{}) {
		t.Fatal("sys_mcp_servers table was not created")
	}
	for _, column := range []string{"command", "arguments_json", "working_directory", "environment_encrypted"} {
		if db.Migrator().HasColumn(&SysMCPServer{}, column) {
			t.Fatalf("remote MCP schema unexpectedly has legacy local-process column %q", column)
		}
	}

	first := SysMCPServer{TenantID: 1, UserID: 2, Name: "filesystem", Transport: "streamable_http", EndpointURL: "https://mcp.example.com", TimeoutSeconds: 60, Enabled: true}
	if err := db.Create(&first).Error; err != nil {
		t.Fatalf("create first MCP server: %v", err)
	}
	duplicate := SysMCPServer{TenantID: 1, UserID: 2, Name: "filesystem", Transport: "streamable_http", EndpointURL: "https://another.example.com", TimeoutSeconds: 60, Enabled: true}
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate tenant/user/name MCP server was created")
	}
	otherUser := SysMCPServer{TenantID: 1, UserID: 3, Name: "filesystem", Transport: "streamable_http", EndpointURL: "https://mcp.example.com", TimeoutSeconds: 60, Enabled: true}
	if err := db.Create(&otherUser).Error; err != nil {
		t.Fatalf("create same-name server for another user: %v", err)
	}
}
