package toolchain

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"InkFlow/utils/cache"
	"InkFlow/utils/fairness"
	"InkFlow/utils/pool/goroutineware"

	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"
)

func TestDispatcherCachesQueryAndInvalidatesAfterMutation(t *testing.T) {
	dispatcher, registry, release := newTestDispatcher(t)
	defer release()

	var queryCalls atomic.Int32
	if err := registry.Register(Tool{Name: "fact.search", Kind: KindQuery, Handler: func(context.Context, json.RawMessage) (any, error) {
		return map[string]any{"call": queryCalls.Add(1)}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(Tool{Name: "fact.create", Kind: KindMutation, Handler: func(context.Context, json.RawMessage) (any, error) {
		return map[string]any{"created": true}, nil
	}}); err != nil {
		t.Fatal(err)
	}

	args := json.RawMessage(`{"query":"墨水"}`)
	first, firstTrace, err := dispatcher.Execute(context.Background(), "alice", "fact.search", args)
	if err != nil {
		t.Fatal(err)
	}
	second, secondTrace, err := dispatcher.Execute(context.Background(), "alice", "fact.search", args)
	if err != nil {
		t.Fatal(err)
	}
	if queryCalls.Load() != 1 {
		t.Fatalf("查询缓存未生效，实际执行 %d 次", queryCalls.Load())
	}
	if firstTrace.Status != "ok" || secondTrace.Status != "ok" || first == nil || second == nil {
		t.Fatalf("查询结果或轨迹异常: %#v / %#v", firstTrace, secondTrace)
	}

	if _, _, err = dispatcher.Execute(context.Background(), "alice", "fact.create", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, _, err = dispatcher.Execute(context.Background(), "alice", "fact.search", args); err != nil {
		t.Fatal(err)
	}
	if queryCalls.Load() != 2 {
		t.Fatalf("写操作成功后应清空查询缓存，实际执行 %d 次", queryCalls.Load())
	}
}

func TestDispatcherSingleflightMergesIdenticalQueries(t *testing.T) {
	dispatcher, registry, release := newTestDispatcher(t)
	defer release()

	var calls atomic.Int32
	if err := registry.Register(Tool{Name: "fact.search", Kind: KindQuery, Handler: func(context.Context, json.RawMessage) (any, error) {
		calls.Add(1)
		time.Sleep(30 * time.Millisecond)
		return map[string]any{"ok": true}, nil
	}}); err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 2)
	for range 2 {
		go func() {
			_, trace, err := dispatcher.Execute(context.Background(), "alice", "fact.search", json.RawMessage(`{"query":"同一请求"}`))
			if err == nil && trace.Status != "ok" {
				err = &traceError{status: trace.Status}
			}
			errCh <- err
		}()
	}
	for range 2 {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("相同并发查询应合并为一次执行，实际为 %d 次", calls.Load())
	}
}

type traceError struct{ status string }

func (err *traceError) Error() string { return "工具轨迹状态异常: " + err.status }

func newTestDispatcher(t *testing.T) (*Dispatcher, *Registry, func()) {
	t.Helper()
	pool, err := goroutineware.Initialize(1)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	dispatcher, err := NewDispatcher(registry, DispatchOptions{
		Queue: fairness.NewQueue(), Pool: pool, Limiter: rate.NewLimiter(rate.Inf, 1),
		Cache: cache.NewResultCache(16), Singleflight: new(singleflight.Group), QueryCacheTTL: time.Minute,
	})
	if err != nil {
		pool.Release()
		t.Fatal(err)
	}
	return dispatcher, registry, pool.Release
}
