package vectorstore

import (
	"context"
	"strings"

	"gorm.io/gorm"
)

// SQLiteFTS5Store 是 SQLite FTS5 的词法检索适配器。
// 它只执行查询；虚拟表、触发器、历史数据回填和业务字段映射均属于应用初始化职责，
// 由 initialize 层创建，不能沉入通用检索包。
type SQLiteFTS5Store struct {
	Table string
}

const (
	sqliteFTS5RecordIDColumn   = "record_id"
	sqliteFTS5CollectionColumn = "collection"
)

// 编译期确认 FTS5 适配器使用与向量后端相同的 CRUD 检索契约。
var _ Store = (*SQLiteFTS5Store)(nil)

// Upsert 是 FTS5 的空操作。
// FTS5 索引由 initialize 创建的 SQLite 触发器随业务主表的写入自动维护。
func (*SQLiteFTS5Store) Upsert(context.Context, []StoreRequest) error {
	return nil
}

// Delete 是 FTS5 的空操作。
// 删除业务主表记录时，对应触发器会一并删除 FTS5 索引行。
func (*SQLiteFTS5Store) Delete(context.Context, []StoreRequest) error {
	return nil
}

// Search 使用 FTS5 MATCH 和 BM25 生成命中 ID 子查询，再附加到调用方传入的业务 GORM 查询。
// 适配器会自动按 req.Collection 限制统一 FTS5 表；业务过滤由 req.Db 作为候选 ID 子查询提供。
func (s *SQLiteFTS5Store) Search(ctx context.Context, req StoreRequest) (gorm.DB, error) {
	if s == nil || s.Table == "" || req.Db == nil {
		return gorm.DB{}, ErrNotConfigured
	}
	queryText := strings.TrimSpace(req.Query)
	if queryText == "" || req.Limit <= 0 {
		return *req.Db.WithContext(ctx).Where("1 = 0"), nil
	}
	candidateIDs := req.Db.WithContext(ctx).Select("id")
	// FTS5 虚拟表没有 GORM 的 deleted_at 字段，必须在此处取消软删除作用域；
	// 候选业务表和最终回表查询仍保留各自的软删除条件。
	ftsQuery := req.Db.Session(&gorm.Session{NewDB: true}).Unscoped().WithContext(ctx).Table(s.Table).
		Select(sqliteFTS5RecordIDColumn).
		Where(s.Table+" MATCH ?", "\""+strings.ReplaceAll(queryText, "\"", "\"\"")+"\"").
		Where(sqliteFTS5CollectionColumn+" = ?", string(req.Collection)).
		Where(sqliteFTS5RecordIDColumn+" IN (?)", candidateIDs)
	ftsQuery = ftsQuery.Order("bm25(" + s.Table + ") ASC").Limit(req.Limit)
	return *req.Db.WithContext(ctx).Where("id IN (?)", ftsQuery), nil
}
