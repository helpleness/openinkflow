// Package cache 提供并发控制所需的线程安全结果缓存，不依赖 global 包。
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

const defaultEntries = 1024

// ResultCache 是带过期时间的进程内缓存。
type ResultCache struct {
	mu         sync.RWMutex
	entries    map[string]entry
	maxEntries int
}

type entry struct {
	value     []byte
	expiresAt time.Time
}

// NewResultCache 创建一个独立缓存。容量小于等于零时使用默认值。
func NewResultCache(maxEntries int) *ResultCache {
	if maxEntries <= 0 {
		maxEntries = defaultEntries
	}
	return &ResultCache{
		entries:    make(map[string]entry),
		maxEntries: maxEntries,
	}
}

// Get 返回缓存值副本；缓存不存在或已过期时返回 false。
func (cache *ResultCache) Get(key string) ([]byte, bool) {
	if cache == nil {
		return nil, false
	}

	now := time.Now()
	cache.mu.RLock()
	value, ok := cache.entries[key]
	cache.mu.RUnlock()
	if !ok || !value.expiresAt.After(now) {
		if ok {
			cache.mu.Lock()
			if current, exists := cache.entries[key]; exists && !current.expiresAt.After(now) {
				delete(cache.entries, key)
			}
			cache.mu.Unlock()
		}
		return nil, false
	}
	return append([]byte(nil), value.value...), true
}

// Set 写入带有效期的缓存值。容量满时淘汰最早过期的数据。
func (cache *ResultCache) Set(key string, value []byte, ttl time.Duration) {
	if cache == nil || ttl <= 0 {
		return
	}

	now := time.Now()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.removeExpiredLocked(now)
	if _, exists := cache.entries[key]; !exists {
		for len(cache.entries) >= cache.maxEntries {
			cache.evictOldestLocked()
		}
	}
	cache.entries[key] = entry{value: append([]byte(nil), value...), expiresAt: now.Add(ttl)}
}

// Clear 移除缓存中的全部结果。
func (cache *ResultCache) Clear() {
	if cache == nil {
		return
	}
	cache.mu.Lock()
	clear(cache.entries)
	cache.mu.Unlock()
}

func (cache *ResultCache) removeExpiredLocked(now time.Time) {
	for key, value := range cache.entries {
		if !value.expiresAt.After(now) {
			delete(cache.entries, key)
		}
	}
}

func (cache *ResultCache) evictOldestLocked() {
	var oldestKey string
	var oldestExpiry time.Time
	for key, value := range cache.entries {
		if oldestKey == "" || value.expiresAt.Before(oldestExpiry) {
			oldestKey = key
			oldestExpiry = value.expiresAt
		}
	}
	if oldestKey != "" {
		delete(cache.entries, oldestKey)
	}
}

// ToolCacheKey 按“用户名 + 工具名 + 参数哈希”构造缓存键。合法 JSON 参数会先
// 规范化，确保仅字段顺序不同的等价对象共享同一个键。
func ToolCacheKey(userName, toolName string, arguments json.RawMessage) string {
	userName = normalize(userName, "anonymous")
	toolName = normalize(toolName, "unknown")
	digest := sha256.Sum256(canonicalJSON(arguments))
	return fmt.Sprintf("%d:%s:%d:%s:%s", len(userName), userName, len(toolName), toolName, hex.EncodeToString(digest[:]))
}

func canonicalJSON(arguments json.RawMessage) []byte {
	trimmed := []byte(strings.TrimSpace(string(arguments)))
	if len(trimmed) == 0 {
		return []byte("{}")
	}
	var value any
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return trimmed
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return trimmed
	}
	return canonical
}

func normalize(value, fallback string) string {
	if normalized := strings.TrimSpace(value); normalized != "" {
		return normalized
	}
	return fallback
}
