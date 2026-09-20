package initialize

import (
	"fmt"
	"path/filepath"
	"strings"

	"InkFlow/global"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// openSQLite 打开客户端本地 SQLite 数据库，并配置适合桌面单实例运行的连接参数。
func openSQLite(gormConfig *gorm.Config) (*gorm.DB, error) {
	path := strings.TrimSpace(global.GVA_CONFIG.System.DbPath)
	if path == "" {
		path = "inkflow.db"
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve SQLite path: %w", err)
	}
	db, err := gorm.Open(sqlite.Open(absPath), gormConfig)
	if err != nil {
		return nil, fmt.Errorf("connect SQLite failed: %w", err)
	}
	// WAL 提升读写并发；外键约束保证数据完整性；busy_timeout 降低短暂锁冲突导致的失败。
	for _, pragma := range []string{"PRAGMA journal_mode=WAL", "PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000"} {
		if err := db.Exec(pragma).Error; err != nil {
			return nil, fmt.Errorf("configure SQLite failed: %w", err)
		}
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get SQLite connection: %w", err)
	}
	// 单连接可以规避 SQLite 多连接写入时的锁竞争，同时仍可复用 WAL 的读取能力。
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	return db, nil
}
