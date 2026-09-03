package initialize

import (
	"fmt"
	"strings"

	"InkFlow/global"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// openPostgres 建立 PostgreSQL 连接、启用 pgvector 扩展并配置连接池。
func openPostgres(gormConfig *gorm.Config) (*gorm.DB, error) {
	p := global.GVA_CONFIG.Pgsql
	if p.Dbname == "" {
		return nil, fmt.Errorf("PostgreSQL database name is empty")
	}
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: p.Dsn(), PreferSimpleProtocol: false}), gormConfig)
	if err != nil {
		return nil, fmt.Errorf("connect PostgreSQL failed: %w", err)
	}
	// 向量字段和距离运算符依赖 pgvector，建表前必须保证扩展可用。
	if err := db.Exec("CREATE EXTENSION IF NOT EXISTS vector").Error; err != nil {
		return nil, fmt.Errorf("create pgvector extension failed: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get PostgreSQL connection: %w", err)
	}
	sqlDB.SetMaxIdleConns(p.MaxIdleConns)
	sqlDB.SetMaxOpenConns(p.MaxOpenConns)
	return db, nil
}

// preparePostgresSchema 只处理 model 无法表达的 pgvector 维度兼容问题。
// 普通字段、默认值和常规索引均由 AutoMigrate 根据 model tag 管理。
func preparePostgresSchema(db *gorm.DB) {
	migrateEmbeddingDimension(db)
}

// finishPostgresSchema 在 AutoMigrate 建表后创建 pgvector 专属 HNSW 索引。
func finishPostgresSchema(db *gorm.DB) {
	for _, table := range postgresVectorTables() {
		ensurePostgresHNSWIndex(db, table.Table, "idx_"+table.Table+"_embedding", table.EmbeddingColumn)
	}
}

// migrateEmbeddingDimension 处理已有 PostgreSQL 表的向量维度不兼容情况。
// 维度不匹配时必须先删除 HNSW 索引；旧向量无法无损转换，因此会置空重建。
func migrateEmbeddingDimension(db *gorm.DB) {
	for _, table := range postgresVectorTables() {
		stmt := embeddingDimensionSQL(table.Table, "idx_"+table.Table+"_embedding", table.EmbeddingColumn)
		if err := db.Exec(stmt).Error; err != nil {
			panic(fmt.Errorf("migrate embedding dimension failed: %w", err))
		}
	}
}

// embeddingDimensionSQL 生成幂等的向量维度迁移 SQL；目标表不存在时不会执行变更。
func embeddingDimensionSQL(table, index, vectorColumn string) string {
	return fmt.Sprintf(`
DO $$
BEGIN
	IF to_regclass('%[1]s') IS NOT NULL
		AND EXISTS (
			SELECT 1
			FROM pg_attribute
			WHERE attrelid = to_regclass('%[1]s')
				AND attname = '%[3]s'
				AND NOT attisdropped
				AND format_type(atttypid, atttypmod) <> 'vector(1024)'
		) THEN
		DROP INDEX IF EXISTS %[2]s;
		ALTER TABLE %[1]s ALTER COLUMN %[3]s TYPE vector(1024) USING NULL::vector(1024);
	END IF;
END $$;`, table, index, vectorColumn)
}

// ensurePostgresHNSWIndex 创建或按配置更新余弦距离 HNSW 索引。
func ensurePostgresHNSWIndex(db *gorm.DB, table, index, vectorColumn string) {
	m := global.GVA_CONFIG.RAG.HNSWM
	if m <= 0 {
		m = 32
	}
	efConstruction := global.GVA_CONFIG.RAG.HNSWEFConstruction
	if efConstruction <= 0 {
		efConstruction = 256
	}
	var exists bool
	if err := db.Raw(`SELECT to_regclass(?) IS NOT NULL`, index).Scan(&exists).Error; err != nil {
		panic(fmt.Errorf("inspect hnsw index %s failed: %w", index, err))
	}
	// 首次启动直接创建索引；已有索引则继续检查参数是否与配置一致。
	if !exists {
		sql := fmt.Sprintf(
			`CREATE INDEX %s ON %s USING hnsw (%s vector_cosine_ops) WITH (m = %d, ef_construction = %d)`,
			index, table, vectorColumn, m, efConstruction,
		)
		if err := db.Exec(sql).Error; err != nil {
			panic(fmt.Errorf("create hnsw index %s failed: %w", index, err))
		}
		return
	}

	var reloptions string
	if err := db.Raw(`SELECT COALESCE(array_to_string(reloptions, ','), '') FROM pg_class WHERE oid = to_regclass(?)`, index).Scan(&reloptions).Error; err != nil {
		panic(fmt.Errorf("inspect hnsw index options %s failed: %w", index, err))
	}
	wantM := fmt.Sprintf("m=%d", m)
	wantEF := fmt.Sprintf("ef_construction=%d", efConstruction)
	if strings.Contains(reloptions, wantM) && strings.Contains(reloptions, wantEF) {
		return
	}
	// 参数变化后需要重建索引，新的构建参数才会作用于现有数据。
	if err := db.Exec(fmt.Sprintf(`ALTER INDEX %s SET (m = %d, ef_construction = %d)`, index, m, efConstruction)).Error; err != nil {
		panic(fmt.Errorf("alter hnsw index %s failed: %w", index, err))
	}
	if err := db.Exec(fmt.Sprintf(`REINDEX INDEX %s`, index)).Error; err != nil {
		panic(fmt.Errorf("reindex hnsw index %s failed: %w", index, err))
	}
}
