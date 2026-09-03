package initialize

// postgresVectorTable 是 PostgreSQL 初始化需要建立 HNSW 索引的主表映射。
// 它只用于建表后的数据库初始化，不进入通用向量 CRUD 包。
type postgresVectorTable struct {
	Table           string
	EmbeddingColumn string
}

// postgresVectorTables 定义需要 pgvector HNSW 索引的业务主表。
func postgresVectorTables() []postgresVectorTable {
	return nil
}
