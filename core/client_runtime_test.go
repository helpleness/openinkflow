package core

import (
	"InkFlow/config"
	"InkFlow/global"
	"path/filepath"
	"testing"
)

func TestConfigureClientRuntimeUsesDedicatedDataDirectory(t *testing.T) {
	t.Setenv(clientDataDirEnv, t.TempDir())
	oldConfig := global.GVA_CONFIG
	oldViper := global.GVA_VP
	global.GVA_VP = nil
	t.Cleanup(func() {
		global.GVA_CONFIG = oldConfig
		global.GVA_VP = oldViper
	})

	paths, err := ConfigureClientRuntime()
	if err != nil {
		t.Fatal(err)
	}
	if global.GVA_CONFIG.System.Addr != "127.0.0.1:0" {
		t.Fatalf("desktop address = %q", global.GVA_CONFIG.System.Addr)
	}
	if global.GVA_CONFIG.System.DbType != "sqlite" {
		t.Fatalf("desktop database type = %q", global.GVA_CONFIG.System.DbType)
	}
	if paths.Database != filepath.Join(paths.Data, "inkflow.db") {
		t.Fatalf("database path = %q", paths.Database)
	}
	if global.GVA_CONFIG.RAG.VectorPath != paths.Vectors {
		t.Fatalf("vector path = %q, want %q", global.GVA_CONFIG.RAG.VectorPath, paths.Vectors)
	}
	if global.GVA_CONFIG.LLMLocal.Embedding.ModelPath != config.DefaultEmbeddingModelPath(paths.Models) {
		t.Fatalf("embedding model path = %q", global.GVA_CONFIG.LLMLocal.Embedding.ModelPath)
	}
	if global.GVA_CONFIG.LLMLocal.Rerank.ModelPath != config.DefaultRerankModelPath(paths.Models) {
		t.Fatalf("rerank model path = %q", global.GVA_CONFIG.LLMLocal.Rerank.ModelPath)
	}
	if config.NormalizeRemoteAuthBaseURL(config.BootstrapAuthBaseURL) == "" {
		t.Fatal("bootstrap auth address is invalid")
	}
}

func TestNormalizeDesktopInferenceProvider(t *testing.T) {
	for _, testCase := range []struct {
		input string
		want  string
	}{
		{input: "local", want: "local"},
		{input: "frontend", want: "frontend"},
		{input: " FRONTEND ", want: "frontend"},
		{input: "unknown", want: "local"},
		{input: "", want: "local"},
	} {
		if got := normalizeDesktopInferenceProvider(testCase.input); got != testCase.want {
			t.Fatalf("normalizeDesktopInferenceProvider(%q) = %q, want %q", testCase.input, got, testCase.want)
		}
	}
}
