// Package goroutineware 只负责创建 ants 协程池；全局实例由 global 包保存。
package goroutineware

import (
	"fmt"
	"runtime"

	"github.com/panjf2000/ants/v2"
)

// Pool 是 ants.Pool 的别名，让 global 包无需依赖 ants 的具体包路径。
type Pool = ants.Pool

// Initialize 创建协程池。size 小于等于零时使用当前 CPU 可用的并发度。
// 创建出的池由调用方保存和释放。
func Initialize(size int) (*Pool, error) {
	if size <= 0 {
		size = runtime.GOMAXPROCS(0)
	}
	if size <= 0 {
		size = 1
	}

	pool, err := ants.NewPool(size)
	if err != nil {
		return nil, fmt.Errorf("create ants pool: %w", err)
	}
	return pool, nil
}
