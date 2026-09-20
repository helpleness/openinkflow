package initialize

import (
	"context"
	"fmt"
	"strings"

	"InkFlow/global"
	officialdoc "InkFlow/model/officialdoc"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Gorm 是存储系统的总初始化入口。
// 它只负责编排初始化顺序；数据库连接、表结构和索引分别由对应后端文件负责。
func Gorm() *gorm.DB {
	gormConfig := &gorm.Config{Logger: logger.Default.LogMode(logger.Info)}
	dbType := normalizedDBType(global.GVA_CONFIG.System.DbType)

	var (
		db  *gorm.DB
		err error
	)
	if dbType == "sqlite" {
		db, err = openSQLite(gormConfig)
	} else {
		db, err = openPostgres(gormConfig)
	}
	if err != nil {
		panic(err)
	}
	// 先注册业务表，确保后续的全文索引和向量索引有可用的数据源。
	global.GVA_DB = db
	if dbType == "postgres" {
		if err := ensurePostgresVectorExtension(db); err != nil {
			panic(fmt.Errorf("initialize pgvector extension: %w", err))
		}
	}
	RegisterTables(db)

	// FTS5 是 SQLite 客户端的本地全文检索能力，PostgreSQL 模式不启用该实例。
	if dbType == "sqlite" {
		global.GVA_LEXICAL_STORE, err = initializeSQLiteFTS5(context.Background(), db)
		if err != nil {
			panic(fmt.Errorf("initialize SQLite FTS5 failed: %w", err))
		}
	} else {
		global.GVA_LEXICAL_STORE = nil
	}

	// 向量存储可以独立于关系数据库选择，例如 SQLite + USearch。
	global.GVA_VECTOR_STORE, err = initializeVectorStore(context.Background(), db, dbType)
	if err != nil {
		panic(fmt.Errorf("initialize vector store failed: %w", err))
	}
	return db
}

// normalizedDBType 将配置中的数据库别名归一化，避免后续分支重复判断多种写法。
func normalizedDBType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "sqlite", "sqlite3":
		return "sqlite"
	case "pgsql", "postgres", "postgresql":
		return "postgres"
	default:
		panic(fmt.Errorf("unsupported database type %q", value))
	}
}

// RegisterTables 是领域模型的数据库迁移入口。
func RegisterTables(db *gorm.DB) {
	if db == nil {
		panic("register tables: database is nil")
	}
	initializeSystemModule(db)
	if err := db.AutoMigrate(
		&officialdoc.KnowledgeDocument{}, &officialdoc.KnowledgeChunk{}, &officialdoc.KnowledgeImage{},
		&officialdoc.DocumentTemplate{}, &officialdoc.WritingTask{}, &officialdoc.DocumentVersion{}, &officialdoc.DocumentReviewComment{}, &officialdoc.WritingEvidence{},
		&officialdoc.WritingRun{}, &officialdoc.WritingRunMessage{}, &officialdoc.WritingRunToolTrace{}, &officialdoc.WritingRunEvidence{},
	); err != nil {
		panic(fmt.Errorf("migrate knowledge document schema: %w", err))
	}
	fmt.Println("system and knowledge document schema ready")
}
