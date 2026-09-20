package vectorstore

import (
	"context"
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
)

type PostgresStore struct {
}

// 编译期确认 PostgreSQL 适配器只实现统一的三方法向量 CRUD 接口。
var _ Store = (*PostgresStore)(nil)

// Upsert 是 pgvector 的空操作。
// 业务服务保存主表模型时已经一并持久化 Embedding；只有独立的本地向量后端需要额外同步索引。
func (*PostgresStore) Upsert(context.Context, []StoreRequest) error {
	return nil
}

// Delete 是 pgvector 的空操作。
// 业务服务删除主表记录时，记录自己的 embedding 列会随该行一同删除。
func (*PostgresStore) Delete(context.Context, []StoreRequest) error {
	return nil
}

// Search 直接在调用方传入的业务 GORM 查询上追加向量排序。
// 调用方随后用返回的查询对象加载具体业务模型，因此 PostgreSQL 不需要先查 ID 再回表。
func (s *PostgresStore) Search(ctx context.Context, req StoreRequest) (gorm.DB, error) {
	if req.Db == nil {
		return gorm.DB{}, ErrNotConfigured
	}
	if len(req.Vector) == 0 {
		return gorm.DB{}, ErrInvalidDimension
	}
	query := req.Db.WithContext(ctx).
		Order(gorm.Expr("embedding <=> ?", pgvector.NewVector(req.Vector)))
	if req.Limit > 0 {
		query = query.Limit(req.Limit)
	}
	return *query, nil
}
