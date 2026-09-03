package initialize

import (
	"context"
	"fmt"

	"InkFlow/utils/vectorstore"

	"gorm.io/gorm"
)

const sqliteFTS5Table = "inkflow_fts"
const knowledgeChunkFTSCollection = "officialdoc_knowledge_chunks"

// initializeSQLiteFTS5 先建立全文索引结构，再创建实现统一 Store 契约的 FTS5 适配器。
func initializeSQLiteFTS5(ctx context.Context, db *gorm.DB) (vectorstore.Store, error) {
	if err := ensureSQLiteFTS5Schema(ctx, db); err != nil {
		return nil, err
	}
	return &vectorstore.SQLiteFTS5Store{Table: sqliteFTS5Table}, nil
}

// ensureSQLiteFTS5Schema 预留通用全文检索虚拟表。
// 小说业务的 FTS 索引与触发器不会在本次代码清理中被删除，以免修改用户的历史
// 本地数据；公文资料的同步规则将由 officialdoc 初始化器在对应模型落地后注册。
func ensureSQLiteFTS5Schema(ctx context.Context, db *gorm.DB) error {
	statements := []string{
		`CREATE VIRTUAL TABLE IF NOT EXISTS inkflow_fts USING fts5(
			collection UNINDEXED,
			record_id UNINDEXED,
			content,
			tokenize='trigram case_sensitive 0'
		)`,
		`CREATE TRIGGER IF NOT EXISTS knowledge_chunks_fts_insert AFTER INSERT ON knowledge_chunks BEGIN
			INSERT INTO inkflow_fts(collection, record_id, content) VALUES ('officialdoc_knowledge_chunks', new.id, trim(coalesce(new.title, '') || ' ' || coalesce(new.parent_title, '') || ' ' || coalesce(new.content, '')));
		END`,
		`CREATE TRIGGER IF NOT EXISTS knowledge_chunks_fts_update AFTER UPDATE OF title, parent_title, content ON knowledge_chunks BEGIN
			DELETE FROM inkflow_fts WHERE collection = 'officialdoc_knowledge_chunks' AND record_id = old.id;
			INSERT INTO inkflow_fts(collection, record_id, content) VALUES ('officialdoc_knowledge_chunks', new.id, trim(coalesce(new.title, '') || ' ' || coalesce(new.parent_title, '') || ' ' || coalesce(new.content, '')));
		END`,
		`CREATE TRIGGER IF NOT EXISTS knowledge_chunks_fts_delete AFTER DELETE ON knowledge_chunks BEGIN
			DELETE FROM inkflow_fts WHERE collection = 'officialdoc_knowledge_chunks' AND record_id = old.id;
		END`,
	}
	// 所有 DDL、触发器和回填在同一事务内完成，失败时不留下半初始化状态。
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, statement := range statements {
			if err := tx.Exec(statement).Error; err != nil {
				return fmt.Errorf("initialize SQLite FTS5 schema: %w", err)
			}
		}
		if err := tx.Exec("DELETE FROM "+sqliteFTS5Table+" WHERE collection = ?", knowledgeChunkFTSCollection).Error; err != nil {
			return fmt.Errorf("clear knowledge SQLite FTS5 records: %w", err)
		}
		if err := tx.Exec(`INSERT INTO inkflow_fts(collection, record_id, content)
			SELECT ?, id, trim(coalesce(title, '') || ' ' || coalesce(parent_title, '') || ' ' || coalesce(content, ''))
			FROM knowledge_chunks WHERE deleted_at IS NULL`, knowledgeChunkFTSCollection).Error; err != nil {
			return fmt.Errorf("backfill knowledge SQLite FTS5 records: %w", err)
		}
		return nil
	})
}
