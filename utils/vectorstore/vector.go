// Package vectorstore 定义可替换的向量 CRUD 接口。
//
// PostgreSQL、USearch 等后端都只实现 Store 的 Upsert、Search、Delete 三个方法。
// 数据库连接、数据表和 ANN 索引由 initialize 层创建；Embedding 与 Rerank 继续复用
// utils/inference 已有适配器，不在本包重复实现。
package vectorstore

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// DefaultDimension 是未显式配置时采用的默认 Embedding 维度。
const DefaultDimension = 1024

var (
	ErrNotConfigured                = errors.New("vector store is not configured")
	ErrBackendDisabled              = errors.New("vector store backend is not included in this build")
	ErrUnsupportedBackend           = errors.New("unsupported vector store backend")
	ErrUnknownCollection            = errors.New("unknown vector collection")
	ErrInvalidID                    = errors.New("invalid vector record id")
	ErrInvalidDimension             = errors.New("invalid vector dimension")
	ErrCollectionRebuildUnsupported = errors.New("vector collection rebuild is not supported")
)

// Collection 是业务层定义的逻辑集合名，例如 entities、facts、rag_chunks。
type Collection string

// StoreRequest 是统一检索 CRUD 的输入。
//
// Search 时 Db 必须是调用方已构造好的业务主表查询（包含 Model、Where、Select 等）；
// PostgreSQL 在其上追加向量排序，USearch 与 SQLite FTS5 则在检索到记录 ID 后追加 id IN 条件。
// Vector 供向量后端检索，Query 供 FTS5 词法检索使用；Db 同时也是 FTS5 的候选集：
// 调用方应将项目、文档、权限等业务过滤直接附加在 Db 上，而不是传入字段名或 SQL 到本包。
// Upsert / Delete 仅使用 Collection、ID、Vector，不需要 Db。
type StoreRequest struct {
	Collection Collection
	ID         uint
	Vector     []float32
	Query      string
	Limit      int
	Db         *gorm.DB
}

// Store 是所有向量与词法检索后端的唯一 CRUD 契约。
// Search 返回可继续调用 Find、First、Scan 等 GORM 方法的查询对象，调用方负责加载业务模型。
type Store interface {
	Upsert(context.Context, []StoreRequest) error
	Search(context.Context, StoreRequest) (gorm.DB, error)
	Delete(context.Context, []StoreRequest) error
}

// CollectionRebuilder 是具备独立物理索引的向量后端可选实现的维护能力。
// records 必须是同一 collection 的完整事实源快照；实现方应在成功后让该集合
// 只保留这些记录。数据库内嵌向量（如 pgvector）无需实现本接口。
type CollectionRebuilder interface {
	RebuildCollection(context.Context, Collection, []StoreRequest) error
}

// BackendType 是初始化阶段可选择的向量后端类型。
type BackendType string

const (
	BackendPGVector BackendType = "pgvector"
	BackendUSearch  BackendType = "usearch"
)

// Factory 由 initialize 层提供，用于创建已经完成连接与表结构准备的后端实例。
// 这样 NewVectorStore 只负责选择实现，不会把数据库初始化逻辑带入抽象 CRUD 包。
type Factory func() (Store, error)

// NewVectorStore 根据后端类型选择并创建对应的向量 CRUD 实现。
// factories 由 initialize 注入，因此 vectorstore 不依赖任何具体数据库 SDK。
func NewVectorStore(backend BackendType, factories map[BackendType]Factory) (Store, error) {
	factory, ok := factories[backend]
	if !ok || factory == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedBackend, backend)
	}
	store, err := factory()
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, ErrNotConfigured
	}
	return store, nil
}
