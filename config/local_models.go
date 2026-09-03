package config

import (
	"path/filepath"
	"strings"
)

// LocalModel describes one GGUF model bundled with the desktop inference
// profile. Keep names, Hugging Face repositories and default file names here
// so runtime configuration and the installer cannot silently drift apart.
type LocalModel struct {
	ID       string
	Filename string
	RepoID   string
}

// LocalModelDownloadSource 描述桌面安装器可选的模型下载端点。端点需要
// 兼容 Hugging Face 的 /{repo}/resolve/main/{file} 下载路径。
type LocalModelDownloadSource struct {
	ID      string
	Name    string
	BaseURL string
}

const (
	LocalModelEmbedding = "embedding"
	LocalModelRerank    = "rerank"

	LocalModelDownloadSourceOfficial = "huggingface"
	LocalModelDownloadSourceChina    = "hf-mirror"
)

var DefaultLocalModelDownloadSources = []LocalModelDownloadSource{
	{
		ID:      LocalModelDownloadSourceOfficial,
		Name:    "Hugging Face 官方",
		BaseURL: "https://huggingface.co",
	},
	{
		ID:      LocalModelDownloadSourceChina,
		Name:    "hf-mirror.com（国内镜像）",
		BaseURL: "https://hf-mirror.com",
	},
}

var defaultLocalModels = map[string]LocalModel{
	LocalModelEmbedding: {
		ID:       LocalModelEmbedding,
		Filename: "qwen3-embedding-0.6b-q4_k_m.gguf",
		RepoID:   "enacimie/Qwen3-Embedding-0.6B-Q4_K_M-GGUF",
	},
	LocalModelRerank: {
		ID:       LocalModelRerank,
		Filename: "bge-reranker-v2-m3-Q4_K_M.gguf",
		RepoID:   "gpustack/bge-reranker-v2-m3-GGUF",
	},
}

// DefaultLocalModel returns immutable metadata for a model shipped by the
// local inference profile. The bool is false only for an unknown model ID.
func DefaultLocalModel(id string) (LocalModel, bool) {
	model, ok := defaultLocalModels[id]
	return model, ok
}

func DefaultEmbeddingModel() LocalModel { return defaultLocalModels[LocalModelEmbedding] }

func DefaultRerankModel() LocalModel { return defaultLocalModels[LocalModelRerank] }

func DefaultEmbeddingModelPath(modelsDir string) string {
	return filepath.Join(modelsDir, DefaultEmbeddingModel().Filename)
}

func DefaultRerankModelPath(modelsDir string) string {
	return filepath.Join(modelsDir, DefaultRerankModel().Filename)
}

// ResolveLocalModelDownloadURL 根据已注册源生成默认 GGUF 的下载地址。
// 调用方如使用私有或其他镜像，可自行传入与 Hugging Face 兼容的 BaseURL。
func ResolveLocalModelDownloadURL(baseURL string, model LocalModel) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/" + model.RepoID + "/resolve/main/" + model.Filename + "?download=true"
}
