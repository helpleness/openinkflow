package cache

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestToolCacheKeyUsesUserToolAndCanonicalArguments(t *testing.T) {
	first := ToolCacheKey("alice", "knowledge.search", json.RawMessage(`{"page":1,"query":"通知"}`))
	same := ToolCacheKey("alice", "knowledge.search", json.RawMessage(`{"query":"通知","page":1}`))
	otherUser := ToolCacheKey("bob", "knowledge.search", json.RawMessage(`{"page":1,"query":"通知"}`))
	otherTool := ToolCacheKey("alice", "knowledge.read", json.RawMessage(`{"page":1,"query":"通知"}`))
	if first != same {
		t.Fatalf("equivalent arguments have different keys: %q != %q", first, same)
	}
	if first == otherUser || first == otherTool {
		t.Fatalf("cache key did not include user and tool: %q", first)
	}
}

func TestResultCacheReturnsCopies(t *testing.T) {
	cache := NewResultCache(1)
	cache.Set("key", []byte("result"), time.Minute)
	first, ok := cache.Get("key")
	if !ok {
		t.Fatal("cache miss")
	}
	first[0] = 'R'
	second, ok := cache.Get("key")
	if !ok || !bytes.Equal(second, []byte("result")) {
		t.Fatalf("cache value was mutated: %q", second)
	}
}
