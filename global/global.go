package global

import (
	"context"
	"sync"
	"time"

	"InkFlow/config"
	"InkFlow/utils/cache"
	"InkFlow/utils/fairness"
	"InkFlow/utils/llamacpp"
	"InkFlow/utils/ocr/layout"
	"InkFlow/utils/pool/goroutineware"
	"InkFlow/utils/storage"
	"InkFlow/utils/vectorstore"

	"github.com/bwmarrin/snowflake"
	"github.com/go-resty/resty/v2"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"
	"gorm.io/gorm"
)

var (
	Lock               sync.RWMutex
	GVA_DB             *gorm.DB // 全局数据库连接
	GVA_VECTOR_STORE   vectorstore.Store
	GVA_LEXICAL_STORE  vectorstore.Store
	GVA_VP             *viper.Viper          // Viper 配置读取器
	GVA_LOG            *zap.Logger           // 全局日志
	GVA_CONFIG         config.Server         // 全局配置结构体数据
	GVA_OBJECT_STORAGE storage.ObjectStorage // 进程级复用的私有对象存储客户端
	GVA_LLM            *resty.Client         // 全局 LLM 客户端
	GVA_LLM_LOCAL      llamacpp.Engine
	GVA_LLM_EMBEDDING  llamacpp.Engine
	GVA_LLM_RERANK     llamacpp.Engine
	GVA_RERANK         *resty.Client // 全局 Rerank 客户端（可与 GVA_LLM 相同）
	// GVA_OCR 是进程级复用的 ONNX 文档版面检测器。它只识别文字、表格等区域，
	// 不承担 OCR 正文转写；初始化失败时保持 nil，文档导入会跳过图片语义分流。
	GVA_OCR *layout.Detector
	// GVA_Concurrency_Control 合并同一时刻的相同请求，调用方应自行添加业务前缀。
	GVA_Concurrency_Control = &singleflight.Group{}
	// SnowflakeNode 是全局唯一 ID 生成节点，由并发控制组件初始化。
	SnowflakeNode *snowflake.Node
	// AntPoolWare 是全局 ants 协程池，由启动流程创建、关闭流程释放。
	AntPoolWare *goroutineware.Pool
	// GVALimiter 是全局令牌桶限速器，由并发控制组件初始化。
	GVALimiter *rate.Limiter
	// GVA_FairQueue 按用户轮转任务，避免高频用户独占协程池。
	GVA_FairQueue   = fairness.NewQueue()
	GVARequestCache *cache.ResultCache
)

// Wait 在应用级限速器允许后继续执行。
func Wait(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	Lock.RLock()
	limiter := GVALimiter
	Lock.RUnlock()
	return limiter.Wait(ctx)
}

// NextID 生成一个全局唯一的雪花 ID。
func NextID() snowflake.ID {
	Lock.RLock()
	node := SnowflakeNode
	Lock.RUnlock()
	return node.Generate()
}

// LoadOrStore 先读取全局缓存；未命中时通过全局 singleflight 合并同一键的并发
// 加载请求。加载和缓存均使用字节副本，因此调用方无需担心可变结果串改缓存。
func LoadOrStore(key string, ttl time.Duration, load func() ([]byte, error)) ([]byte, error) {
	if value, ok := GVARequestCache.Get(key); ok {
		return value, nil
	}
	value, err, _ := GVA_Concurrency_Control.Do("cache:"+key, func() (any, error) {
		if cached, ok := GVARequestCache.Get(key); ok {
			return cached, nil
		}
		loaded, loadErr := load()
		if loadErr != nil {
			return nil, loadErr
		}
		GVARequestCache.Set(key, loaded, ttl)
		return append([]byte(nil), loaded...), nil
	})
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), value.([]byte)...), nil
}
