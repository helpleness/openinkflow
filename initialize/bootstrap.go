package initialize

import (
	"InkFlow/core"
	"InkFlow/global"
	"InkFlow/utils/cache"
	"InkFlow/utils/ocr"
	"InkFlow/utils/pool/goroutineware"
	"github.com/bwmarrin/snowflake"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// InitializeServices 完成不依赖 HTTP 监听地址的应用服务初始化，并返回资源关闭函数。
// Web 服务入口和桌面客户端入口共用此流程，避免两种启动方式出现数据库、向量库或推理引擎初始化差异。
func InitializeServices() func() {
	global.GVA_LOG = core.Zap()
	zap.ReplaceGlobals(global.GVA_LOG)
	InitializeObjectStorage()

	config := global.GVA_CONFIG.Concurrency
	node, err := snowflake.NewNode(config.NodeID)
	if err != nil {
		panic(err)
	}
	global.Lock.Lock()
	global.GVALimiter = rate.NewLimiter(rate.Limit(config.RequestsPerSecond), config.Burst)
	global.SnowflakeNode = node
	global.GVARequestCache = cache.NewResultCache(config.CacheEntries)
	global.Lock.Unlock()

	pool, err := goroutineware.Initialize(0)
	if err != nil {
		panic(err)
	}
	global.AntPoolWare = pool

	global.GVA_DB = Gorm()
	initializeDocumentLayoutDetector()
	SetupLLMClient()
	InitializeLLM()
	InitRerankEngine()
	InitLocalEmbeddingEngine()

	return func() {
		if global.AntPoolWare != nil {
			global.AntPoolWare.Release()
			global.AntPoolWare = nil
		}
		if global.GVA_LLM_EMBEDDING != nil {
			_ = global.GVA_LLM_EMBEDDING.Close()
		}
		if global.GVA_LLM_RERANK != nil {
			_ = global.GVA_LLM_RERANK.Close()
		}
		if global.GVA_LLM_LOCAL != nil {
			_ = global.GVA_LLM_LOCAL.Close()
		}
		if global.GVA_OCR != nil {
			global.GVA_OCR.Close()
			global.GVA_OCR = nil
		}
		_ = CloseVectorStore()
		global.GVA_OBJECT_STORAGE = nil
		if global.GVA_DB != nil {
			if sqlDB, err := global.GVA_DB.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}
		if global.GVA_LOG != nil {
			_ = global.GVA_LOG.Sync()
		}
	}
}

// initializeDocumentLayoutDetector 创建进程复用的 C++ ONNX 版面检测器。开发环境
// 未携带原生 DLL、或部署端显式关闭 OCR 时只记录告警，不能阻塞其他服务启动。
func initializeDocumentLayoutDetector() {
	detector, err := ocr.NewLayoutDetector(global.GVA_CONFIG.OCR)
	if err != nil {
		global.GVA_LOG.Warn("本地 ONNX 文档版面检测器未启用", zap.Error(err))
		return
	}
	global.GVA_OCR = detector
	global.GVA_LOG.Info("本地 ONNX 文档版面检测器已就绪")
}
