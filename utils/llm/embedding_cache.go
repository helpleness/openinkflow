package llm

import (
	"container/list"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"

	"InkFlow/global"

	_ "github.com/glebarez/go-sqlite"
)

// embeddingCache 是一个进程内 LRU，用于减少频繁检索场景下的 embedding 请求。
// 这里不做 TTL：向量随模型变化才会失效；如果你切换 embedding 模型，重启服务即可。
var embedCache = newEmbeddingLRU(2048)
var embedSQLite embeddingSQLiteCache

const embeddingCacheNamespace = "qwen3-embedding-0.6b:q4f16:1024:v2"

type embeddingLRU struct {
	mu         sync.Mutex
	maxEntries int
	ll         *list.List
	cache      map[string]*list.Element
}

type embeddingEntry struct {
	key string
	vec []float32
}

func newEmbeddingLRU(maxEntries int) *embeddingLRU {
	if maxEntries <= 0 {
		maxEntries = 1
	}
	return &embeddingLRU{
		maxEntries: maxEntries,
		ll:         list.New(),
		cache:      make(map[string]*list.Element, maxEntries),
	}
}

func embeddingCacheGet(key string) ([]float32, bool) {
	embedCache.mu.Lock()
	defer embedCache.mu.Unlock()

	if ele, ok := embedCache.cache[key]; ok {
		embedCache.ll.MoveToFront(ele)
		ent := ele.Value.(embeddingEntry)
		// 避免外部修改底层数组；复制一份（向量长度一般 768，拷贝成本可接受）
		out := make([]float32, len(ent.vec))
		copy(out, ent.vec)
		return out, true
	}

	return nil, false
}

func embeddingCachePut(key string, vec []float32) {
	embedCache.mu.Lock()
	defer embedCache.mu.Unlock()

	if ele, ok := embedCache.cache[key]; ok {
		embedCache.ll.MoveToFront(ele)
		ent := ele.Value.(embeddingEntry)
		ent.vec = cloneVec(vec)
		ele.Value = ent
		return
	}

	ele := embedCache.ll.PushFront(embeddingEntry{key: key, vec: cloneVec(vec)})
	embedCache.cache[key] = ele

	if embedCache.ll.Len() > embedCache.maxEntries {
		last := embedCache.ll.Back()
		if last != nil {
			ent := last.Value.(embeddingEntry)
			delete(embedCache.cache, ent.key)
			embedCache.ll.Remove(last)
		}
	}
}

func cloneVec(in []float32) []float32 {
	out := make([]float32, len(in))
	copy(out, in)
	return out
}

func embeddingCacheKey(text string) string {
	sum := sha256.Sum256([]byte(embeddingCacheNamespace + "\x00" + text))
	return hex.EncodeToString(sum[:])
}

type embeddingSQLiteCache struct {
	once sync.Once
	db   *sql.DB
	err  error
}

func embeddingSQLiteEnabled() bool {
	return global.GVA_CONFIG.RAG.EmbeddingCacheEnabled
}

func embeddingSQLitePath() string {
	path := global.GVA_CONFIG.RAG.EmbeddingCachePath
	if path == "" {
		path = "./.cache/embedding_cache.sqlite"
	}
	return path
}

func (c *embeddingSQLiteCache) open() (*sql.DB, error) {
	if !embeddingSQLiteEnabled() {
		return nil, nil
	}
	c.once.Do(func() {
		path := embeddingSQLitePath()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			c.err = err
			return
		}
		db, err := sql.Open("sqlite", path)
		if err != nil {
			c.err = err
			return
		}
		db.SetMaxOpenConns(1)
		if _, err = db.Exec(`
CREATE TABLE IF NOT EXISTS embedding_cache (
	key TEXT PRIMARY KEY,
	namespace TEXT NOT NULL,
	dim INTEGER NOT NULL,
	vector BLOB NOT NULL,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_embedding_cache_namespace ON embedding_cache(namespace);
`); err != nil {
			_ = db.Close()
			c.err = err
			return
		}
		c.db = db
	})
	return c.db, c.err
}

func embeddingSQLiteGet(key string) ([]float32, bool) {
	db, err := embedSQLite.open()
	if err != nil || db == nil {
		return nil, false
	}
	var blob []byte
	var dim int
	err = db.QueryRow(`SELECT vector, dim FROM embedding_cache WHERE key = ? AND namespace = ?`, key, embeddingCacheNamespace).Scan(&blob, &dim)
	if err != nil || dim <= 0 {
		return nil, false
	}
	vec, err := decodeFloat32Vector(blob, dim)
	if err != nil {
		return nil, false
	}
	return vec, true
}

func embeddingSQLitePut(key string, vec []float32) {
	db, err := embedSQLite.open()
	if err != nil || db == nil || len(vec) == 0 {
		return
	}
	blob := encodeFloat32Vector(vec)
	_, _ = db.Exec(`
INSERT INTO embedding_cache(key, namespace, dim, vector)
VALUES (?, ?, ?, ?)
ON CONFLICT(key) DO UPDATE SET
	namespace = excluded.namespace,
	dim = excluded.dim,
	vector = excluded.vector,
	updated_at = CURRENT_TIMESTAMP
`, key, embeddingCacheNamespace, len(vec), blob)
}

func encodeFloat32Vector(vec []float32) []byte {
	out := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(v))
	}
	return out
}

func decodeFloat32Vector(blob []byte, dim int) ([]float32, error) {
	if len(blob) != dim*4 {
		return nil, fmt.Errorf("embedding cache vector length mismatch")
	}
	out := make([]float32, dim)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(blob[i*4:]))
	}
	return out, nil
}
