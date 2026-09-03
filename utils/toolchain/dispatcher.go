// Package toolchain 提供大模型工具调用的统一注册与编排能力。
//
// 调用链分为三层：
//
//   - Registry：声明工具的名称、参数、类型和处理函数；同时处理领域名称与 LLM 函数名称之间的转换。
//   - RunWithTools：驱动“模型选择工具 → 执行工具 → 回传结果 → 生成结论”的编排循环，并记录调用轨迹。
//   - MCP：将 stdio MCP 服务发现到的工具适配为 Registry 工具。
//
// 领域工具使用“领域.动作”形式的名称，例如 document.search。部分 OpenAI 兼容端点不接受
// 点号，因此 Registry 会在发给模型时转换名称，并在接收 tool call 时再转换回来。调用方只需要
// 使用领域名称，不应依赖转换后的协议名称。
//
// RunWithTools 不负责具体业务、权限校验和任务调度。工具 Handler 应在所属领域服务中完成这些
// 事情，再通过 Registry 注册给本包。
//
// 并发池、限速、请求合并、缓存和公平队列均属于独立 utils 组件。Dispatcher 仅持有调用方
// 显式传入的实例，不读取或创建 global 全局资源。
package toolchain

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"InkFlow/utils/cache"
	"InkFlow/utils/fairness"
	"InkFlow/utils/pool/goroutineware"

	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"
)

const defaultQueryCacheTTL = time.Minute

// DispatchOptions 是 Dispatcher 使用的共享并发组件。
//
// 调用方在应用初始化处直接传入全局实例；本包不读取 global，因而可以独立复用和测试。
type DispatchOptions struct {
	Queue        *fairness.Queue
	Pool         *goroutineware.Pool
	Limiter      *rate.Limiter
	Cache        *cache.ResultCache
	Singleflight *singleflight.Group

	// QueryCacheTTL 是只读工具成功结果的缓存时长。小于等于零时使用一分钟。
	QueryCacheTTL time.Duration
}

// Dispatcher 持有一次工具调度所需的组件，并按“缓存/请求合并 → 公平队列 → 协程池 → 限流器”执行工具。
// 写工具不读取结果缓存，但成功后会清空查询缓存。
type Dispatcher struct {
	registry      *Registry
	queue         *fairness.Queue
	pool          *goroutineware.Pool
	limiter       *rate.Limiter
	cache         *cache.ResultCache
	singleflight  *singleflight.Group
	queryCacheTTL time.Duration

	// draining 防止多个 goroutine 同时从同一公平队列批量取任务，破坏队列已经决定的顺序。
	schedulerMu sync.Mutex
	draining    bool
}

// NewDispatcher 创建一个工具调度器。所有共享组件均由调用方传入，避免工具包依赖应用全局状态。
func NewDispatcher(registry *Registry, options DispatchOptions) (*Dispatcher, error) {
	if registry == nil {
		return nil, fmt.Errorf("工具调度器尚未绑定注册表")
	}
	if options.Queue == nil || options.Pool == nil || options.Limiter == nil || options.Cache == nil || options.Singleflight == nil {
		return nil, fmt.Errorf("工具调度组件尚未初始化")
	}
	if options.QueryCacheTTL <= 0 {
		options.QueryCacheTTL = defaultQueryCacheTTL
	}
	return &Dispatcher{
		registry:      registry,
		queue:         options.Queue,
		pool:          options.Pool,
		limiter:       options.Limiter,
		cache:         options.Cache,
		singleflight:  options.Singleflight,
		queryCacheTTL: options.QueryCacheTTL,
	}, nil
}

// Execute 按工具类型调度一次调用。
// 查询工具使用“用户名 + 工具名 + 参数哈希”作为缓存键，并合并同一时刻的重复请求。
func (dispatcher *Dispatcher) Execute(ctx context.Context, userName, name string, args json.RawMessage) (any, Trace, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if dispatcher == nil || dispatcher.registry == nil {
		err := fmt.Errorf("工具调度器尚未绑定注册表")
		return nil, failedTrace(name, "", args, err), err
	}

	registeredTool, ok := dispatcher.registry.Get(name)
	if !ok {
		return dispatcher.registry.Call(ctx, name, args)
	}
	// 调度器会在队列中异步执行，复制参数以避免调用方复用底层字节数组导致数据竞争。
	safeArgs := append(json.RawMessage(nil), args...)
	if registeredTool.Kind != KindQuery {
		return dispatcher.enqueueAndWait(ctx, userName, name, registeredTool.Kind, safeArgs)
	}

	cacheKey := cache.ToolCacheKey(userName, name, safeArgs)
	payload, err := dispatcher.loadOrStore(cacheKey, func() ([]byte, error) {
		result, _, executeErr := dispatcher.enqueueAndWait(ctx, userName, name, registeredTool.Kind, safeArgs)
		if executeErr != nil {
			return nil, executeErr
		}
		encoded, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return nil, fmt.Errorf("工具 %s 的结果无法写入请求缓存: %w", name, marshalErr)
		}
		return encoded, nil
	})
	if err != nil {
		return nil, failedTrace(name, registeredTool.Kind, safeArgs, err), err
	}

	var result any
	if err := json.Unmarshal(payload, &result); err != nil {
		cacheErr := fmt.Errorf("工具 %s 的缓存结果格式异常: %w", name, err)
		return nil, failedTrace(name, registeredTool.Kind, safeArgs, cacheErr), cacheErr
	}
	trace := dispatcher.registry.ResultTrace(name, safeArgs, result)
	if trace.Status == "error" {
		return nil, trace, fmt.Errorf("%s", trace.Error)
	}
	return result, trace, nil
}

func (dispatcher *Dispatcher) loadOrStore(key string, load func() ([]byte, error)) ([]byte, error) {
	if value, ok := dispatcher.cache.Get(key); ok {
		return value, nil
	}
	value, err, _ := dispatcher.singleflight.Do("cache:"+key, func() (any, error) {
		if cached, ok := dispatcher.cache.Get(key); ok {
			return cached, nil
		}
		loaded, loadErr := load()
		if loadErr != nil {
			return nil, loadErr
		}
		dispatcher.cache.Set(key, loaded, dispatcher.queryCacheTTL)
		return append([]byte(nil), loaded...), nil
	})
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), value.([]byte)...), nil
}

type dispatchOutcome struct {
	result any
	trace  Trace
	err    error
}

// enqueueAndWait 将一次调用交给公平队列，并等待 ants 工作协程返回结果。
func (dispatcher *Dispatcher) enqueueAndWait(ctx context.Context, userName, name string, kind Kind, args json.RawMessage) (any, Trace, error) {
	done := make(chan dispatchOutcome, 1)
	finish := func(outcome dispatchOutcome) {
		select {
		case done <- outcome:
		default:
		}
	}

	job := fairness.Job{
		UserName: userName,
		Run: func() {
			if err := ctx.Err(); err != nil {
				finish(dispatchOutcome{trace: failedTrace(name, kind, args, err), err: err})
				return
			}
			if err := dispatcher.limiter.Wait(ctx); err != nil {
				finish(dispatchOutcome{trace: failedTrace(name, kind, args, err), err: err})
				return
			}
			result, trace, err := dispatcher.registry.Call(ctx, name, args)
			if err == nil && kind == KindMutation {
				dispatcher.cache.Clear()
			}
			finish(dispatchOutcome{result: result, trace: trace, err: err})
		},
		OnRejected: func(err error) {
			rejectedErr := fmt.Errorf("工具任务无法提交到协程池: %w", err)
			finish(dispatchOutcome{trace: failedTrace(name, kind, args, rejectedErr), err: rejectedErr})
		},
	}
	if err := dispatcher.queue.EnqueueJob(job); err != nil {
		return nil, failedTrace(name, kind, args, err), err
	}
	dispatcher.startDraining()

	select {
	case outcome := <-done:
		return outcome.result, outcome.trace, outcome.err
	case <-ctx.Done():
		return nil, failedTrace(name, kind, args, ctx.Err()), ctx.Err()
	}
}

func (dispatcher *Dispatcher) startDraining() {
	dispatcher.schedulerMu.Lock()
	if dispatcher.draining {
		dispatcher.schedulerMu.Unlock()
		return
	}
	dispatcher.draining = true
	dispatcher.schedulerMu.Unlock()
	go dispatcher.drain()
}

// drain 严格按公平队列的 Dequeue 顺序向 ants 池提交任务。
func (dispatcher *Dispatcher) drain() {
	defer func() {
		dispatcher.schedulerMu.Lock()
		dispatcher.draining = false
		pending := dispatcher.queue.Pending() > 0
		dispatcher.schedulerMu.Unlock()
		if pending {
			dispatcher.startDraining()
		}
	}()

	for {
		job, ok := dispatcher.queue.Dequeue()
		if !ok {
			return
		}
		if err := dispatcher.pool.Submit(job.Run); err != nil && job.OnRejected != nil {
			job.OnRejected(err)
		}
	}
}

func failedTrace(name string, kind Kind, args json.RawMessage, err error) Trace {
	trace := Trace{ToolName: name, Kind: kind, Input: append(json.RawMessage(nil), args...), Status: "error", CreatedAt: time.Now()}
	if err != nil {
		trace.Error = err.Error()
	}
	return trace
}
