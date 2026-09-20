package initialize

import (
	"context"
	"fmt"
	"strings"

	"InkFlow/global"
	"InkFlow/utils/vectorstore"

	"gorm.io/gorm"
)

// initializeVectorStore 根据配置选择向量后端，并返回统一的查询接口。
// 关系数据库类型与向量数据库类型可以解耦，但 pgvector 必须依赖 PostgreSQL。
func initializeVectorStore(ctx context.Context, db *gorm.DB, dbType string) (vectorstore.Store, error) {
	cfg := global.GVA_CONFIG.RAG
	backend := strings.ToLower(strings.TrimSpace(cfg.VectorBackend))
	// 未显式配置时：服务端 PostgreSQL 使用 pgvector，桌面端 SQLite 使用 USearch。
	if backend == "" {
		if dbType == "postgres" {
			backend = "pgvector"
		} else {
			backend = "usearch"
		}
	}
	var backendType vectorstore.BackendType
	switch backend {
	case "pgvector":
		if dbType != "postgres" {
			return nil, fmt.Errorf("pgvector requires system.db-type=pgsql")
		}
		backendType = vectorstore.BackendPGVector
	case "usearch":
		backendType = vectorstore.BackendUSearch
	default:
		return nil, fmt.Errorf("unsupported vector backend %q", backend)
	}
	return vectorstore.NewVectorStore(backendType, map[vectorstore.BackendType]vectorstore.Factory{
		vectorstore.BackendPGVector: func() (vectorstore.Store, error) {
			return initializePostgresVectorStore(db)
		},
		vectorstore.BackendUSearch: func() (vectorstore.Store, error) {
			return initializeUSearch(ctx)
		},
	})
}

// initializePostgresVectorStore 将业务集合映射传入通用 pgvector 查询适配器。
func ensurePostgresVectorExtension(db *gorm.DB) error {
	if db == nil {
		return vectorstore.ErrNotConfigured
	}
	return db.Exec("CREATE EXTENSION IF NOT EXISTS vector").Error
}

func initializePostgresVectorStore(db *gorm.DB) (vectorstore.Store, error) {
	if err := ensurePostgresVectorExtension(db); err != nil {
		return nil, err
	}
	dimension := global.GVA_CONFIG.RAG.VectorDimension
	if dimension <= 0 {
		dimension = vectorstore.DefaultDimension
	}
	m := global.GVA_CONFIG.RAG.HNSWM
	if m <= 0 {
		m = 32
	}
	efConstruction := global.GVA_CONFIG.RAG.HNSWEFConstruction
	if efConstruction <= 0 {
		efConstruction = 256
	}
	statement := fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_knowledge_chunks_embedding_hnsw ON knowledge_chunks USING hnsw ((embedding::vector(%d)) vector_cosine_ops) WITH (m = %d, ef_construction = %d)", dimension, m, efConstruction)
	if err := db.Exec(statement).Error; err != nil {
		return nil, fmt.Errorf("create knowledge chunk HNSW index: %w", err)
	}
	return &vectorstore.PostgresStore{}, nil
}

// CloseVectorStore 释放由初始化层创建的向量后端资源。
// 当前只有 USearch 持有需要显式关闭的本地 HNSW 索引；pgvector 使用主数据库连接。
func CloseVectorStore() error {
	return closeUSearch()
}
