package config

type RAG struct {
	// VectorBackend 指定向量存储后端，可选 usearch、pgvector。
	// 留空时，SQLite 默认使用 USearch，PostgreSQL 默认使用 pgvector。
	VectorBackend string `mapstructure:"vector-backend" json:"vector-backend" yaml:"vector-backend"`

	// VectorPath 是 USearch 本地索引目录；留空时使用 ./data/inkflow_vectors。
	// pgvector 模式直接使用 PostgreSQL 连接，因此忽略该配置。
	VectorPath string `mapstructure:"vector-path" json:"vector-path" yaml:"vector-path"`

	// VectorDimension 是 Embedding 向量维度，必须与当前 Embedding 模型输出一致。
	// 小于等于 0 时使用默认值 1024；已有 USearch 索引和 PostgreSQL 向量列也必须保持相同维度。
	VectorDimension int `mapstructure:"vector-dimension" json:"vector-dimension" yaml:"vector-dimension"`

	// HNSWM 是 USearch 与 PostgreSQL HNSW 索引中每个节点维护的最大邻接数，默认 32。
	// 数值越大通常召回率越高，但索引体积、构建时间和写入成本也越高。
	HNSWM int `mapstructure:"hnsw-m" json:"hnsw-m" yaml:"hnsw-m"`

	// HNSWEFConstruction 是 USearch 与 PostgreSQL HNSW 建索引时的候选队列大小，默认 256。
	// 数值越大通常索引质量越好，但建索引所需时间和内存也越多。
	HNSWEFConstruction int `mapstructure:"hnsw-ef-construction" json:"hnsw-ef-construction" yaml:"hnsw-ef-construction"`

	// HNSWEFSearch 是 USearch 单次 HNSW 查询的候选队列大小，默认 64。
	// 数值越大通常召回率越高，但查询延迟和 CPU 使用也会相应增加；pgvector 模式忽略该配置。
	HNSWEFSearch int `mapstructure:"hnsw-ef-search" json:"hnsw-ef-search" yaml:"hnsw-ef-search"`

	// EmbeddingCacheEnabled 控制是否启用服务端 SQLite Embedding 持久化缓存。
	// 进程内 LRU 缓存不受此开关影响；前端 WebGPU 模式通常不需要开启。
	EmbeddingCacheEnabled bool `mapstructure:"embedding-cache-enabled" json:"embedding-cache-enabled" yaml:"embedding-cache-enabled"`

	// EmbeddingCachePath 是服务端 Embedding SQLite 缓存文件路径。
	// 仅在 EmbeddingCacheEnabled 为 true 时使用；留空时使用 ./.cache/embedding_cache.sqlite。
	EmbeddingCachePath string `mapstructure:"embedding-cache-path" json:"embedding-cache-path" yaml:"embedding-cache-path"`
}
