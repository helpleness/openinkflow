package core

import (
	"InkFlow/config"
	"InkFlow/global"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const clientDataDirEnv = "INKFLOW_DATA_DIR"

// ClientRuntimePaths 统一描述桌面客户端运行时产生的数据位置。
// 业务数据库是事实源；向量、模型镜像和日志均是可独立维护的本地资源。
type ClientRuntimePaths struct {
	Root           string
	Data           string
	Database       string
	Vectors        string
	Cache          string
	EmbeddingCache string
	ModelCache     string
	Models         string
	Logs           string
	Backups        string
	WebViewData    string
}

// ConfigureClientRuntime 将桌面客户端的可变数据全部放到系统用户数据目录。
// INKFLOW_DATA_DIR 可覆盖根目录，便于开发、便携模式和自动化测试；该函数不会移动或删除既有数据。
// 调用前必须已通过 InitializeViper 加载配置。
func ConfigureClientRuntime() (ClientRuntimePaths, error) {
	paths, err := prepareClientRuntimePaths()
	if err != nil {
		return ClientRuntimePaths{}, err
	}
	overrides := map[string]any{
		"system.addr":              "127.0.0.1:0",
		"system.db-type":           "sqlite",
		"system.data-dir":          paths.Root,
		"system.db-path":           paths.Database,
		"system.model-cache-path":  paths.ModelCache,
		"rag.vector-path":          paths.Vectors,
		"rag.embedding-cache-path": paths.EmbeddingCache,
		"zap.director":             paths.Logs,
	}
	if strings.TrimSpace(global.GVA_CONFIG.LLMLocal.Embedding.ModelPath) == "" {
		// 默认模型放在用户数据目录，安装升级不会覆盖；也允许
		// INKFLOW_CLIENT_CONFIG 指向任意已下载的 GGUF 文件。
		overrides["llm-local.embedding.model-path"] = config.DefaultEmbeddingModelPath(paths.Models)
	}
	if strings.TrimSpace(global.GVA_CONFIG.LLMLocal.Rerank.ModelPath) == "" {
		overrides["llm-local.rerank.model-path"] = config.DefaultRerankModelPath(paths.Models)
	}
	if global.GVA_VP != nil {
		for key, value := range overrides {
			global.GVA_VP.Set(key, value)
		}
		if err := global.GVA_VP.Unmarshal(&global.GVA_CONFIG); err != nil {
			return ClientRuntimePaths{}, fmt.Errorf("apply desktop runtime configuration: %w", err)
		}
		return paths, nil
	}

	// 该分支仅用于直接构造全局配置的测试或嵌入式调用。
	global.GVA_CONFIG.System.Addr = "127.0.0.1:0"
	global.GVA_CONFIG.System.DbType = "sqlite"
	global.GVA_CONFIG.System.DataDir = paths.Root
	global.GVA_CONFIG.System.DbPath = paths.Database
	global.GVA_CONFIG.System.ModelCachePath = paths.ModelCache
	global.GVA_CONFIG.RAG.VectorPath = paths.Vectors
	global.GVA_CONFIG.RAG.EmbeddingCachePath = paths.EmbeddingCache
	global.GVA_CONFIG.Zap.Director = paths.Logs
	if strings.TrimSpace(global.GVA_CONFIG.LLMLocal.Embedding.ModelPath) == "" {
		global.GVA_CONFIG.LLMLocal.Embedding.ModelPath = config.DefaultEmbeddingModelPath(paths.Models)
	}
	if strings.TrimSpace(global.GVA_CONFIG.LLMLocal.Rerank.ModelPath) == "" {
		global.GVA_CONFIG.LLMLocal.Rerank.ModelPath = config.DefaultRerankModelPath(paths.Models)
	}
	if global.GVA_CONFIG.Auth.RemoteTimeoutSeconds <= 0 {
		global.GVA_CONFIG.Auth.RemoteTimeoutSeconds = 15
	}
	return paths, nil
}

func prepareClientRuntimePaths() (ClientRuntimePaths, error) {
	root, err := defaultClientDataDir()
	if err != nil {
		return ClientRuntimePaths{}, err
	}
	paths := ClientRuntimePaths{
		Root:        root,
		Data:        filepath.Join(root, "data"),
		Vectors:     filepath.Join(root, "vectors"),
		Cache:       filepath.Join(root, "cache"),
		ModelCache:  filepath.Join(root, "model-cache"),
		Models:      filepath.Join(root, "models"),
		Logs:        filepath.Join(root, "logs"),
		Backups:     filepath.Join(root, "backups"),
		WebViewData: filepath.Join(root, "webview"),
	}
	paths.Database = filepath.Join(paths.Data, "inkflow.db")
	paths.EmbeddingCache = filepath.Join(paths.Cache, "embedding_cache.sqlite")

	for _, dir := range []string{
		paths.Root,
		paths.Data,
		paths.Vectors,
		paths.Cache,
		paths.ModelCache,
		paths.Models,
		paths.Logs,
		paths.Backups,
		paths.WebViewData,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return ClientRuntimePaths{}, fmt.Errorf("create client runtime directory %q: %w", dir, err)
		}
	}
	return paths, nil
}

func defaultClientDataDir() (string, error) {
	if configured := strings.TrimSpace(os.Getenv(clientDataDirEnv)); configured != "" {
		root, err := filepath.Abs(configured)
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", clientDataDirEnv, err)
		}
		return root, nil
	}
	if runtime.GOOS == "windows" {
		if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
			return filepath.Join(localAppData, "InkFlow"), nil
		}
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve client user data directory: %w", err)
	}
	return filepath.Join(configDir, "InkFlow"), nil
}
