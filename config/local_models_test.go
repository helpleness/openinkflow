package config

import (
	"testing"
)

func TestDefaultLocalModelsHaveStableFiles(t *testing.T) {
	embedding := DefaultEmbeddingModel()
	rerank := DefaultRerankModel()
	if embedding.Filename != "qwen3-embedding-0.6b-q4_k_m.gguf" || embedding.RepoID == "" {
		t.Fatalf("unexpected embedding model: %#v", embedding)
	}
	if rerank.Filename != "bge-reranker-v2-m3-Q4_K_M.gguf" || rerank.RepoID == "" {
		t.Fatalf("unexpected rerank model: %#v", rerank)
	}
}

func TestResolveLocalModelDownloadURL(t *testing.T) {
	got := ResolveLocalModelDownloadURL("https://hf-mirror.com/", DefaultEmbeddingModel())
	want := "https://hf-mirror.com/enacimie/Qwen3-Embedding-0.6B-Q4_K_M-GGUF/resolve/main/qwen3-embedding-0.6b-q4_k_m.gguf?download=true"
	if got != want {
		t.Fatalf("download URL = %q, want %q", got, want)
	}
}
