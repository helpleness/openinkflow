package promptstore

import (
	"context"
	"strings"
	"sync"
)

type Store interface {
	GetPrompt(ctx context.Context, key string) (string, bool, error)
}

type userIDContextKey struct{}

func WithUserID(ctx context.Context, userID uint) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, userIDContextKey{}, userID)
}

func UserID(ctx context.Context) uint {
	if ctx == nil {
		return 0
	}
	if userID, ok := ctx.Value(userIDContextKey{}).(uint); ok {
		return userID
	}
	return 0
}

type StaticStore map[string]string

func (s StaticStore) GetPrompt(_ context.Context, key string) (string, bool, error) {
	value := strings.TrimSpace(s[key])
	return value, value != "", nil
}

var (
	mu           sync.RWMutex
	defaultStore Store = StaticStore{}
)

func SetDefault(store Store) {
	mu.Lock()
	defer mu.Unlock()
	if store == nil {
		defaultStore = StaticStore{}
		return
	}
	defaultStore = store
}

func Get(ctx context.Context, key string, fallback string) string {
	if ctx == nil {
		ctx = context.Background()
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return strings.TrimSpace(fallback)
	}
	mu.RLock()
	store := defaultStore
	mu.RUnlock()
	if store == nil {
		return strings.TrimSpace(fallback)
	}
	value, ok, err := store.GetPrompt(ctx, key)
	if err != nil || !ok || strings.TrimSpace(value) == "" {
		return strings.TrimSpace(fallback)
	}
	return strings.TrimSpace(value)
}
