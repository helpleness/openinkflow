package core

import (
	"InkFlow/global"
	"os"
	"path/filepath"
	"testing"
)

func TestInitializeClientViperUsesLocalEngineDefaultsWithoutConfigFile(t *testing.T) {
	t.Setenv(clientDataDirEnv, t.TempDir())
	t.Setenv("INKFLOW_CLIENT_CONFIG", "")
	oldConfig := global.GVA_CONFIG
	oldBackend := desktopBackend
	desktopBackend = "cuda"
	defer func() {
		global.GVA_CONFIG = oldConfig
		desktopBackend = oldBackend
	}()

	InitializeClientViper()
	if got := global.GVA_CONFIG.LLMLocal.Rerank.GPULayers; got != -1 {
		t.Fatalf("default rerank gpu-layers = %d, want -1", got)
	}
	if got := global.GVA_CONFIG.LLMLocal.Embedding.GPULayers; got != -1 {
		t.Fatalf("default embedding gpu-layers = %d, want -1", got)
	}
	if got := global.GVA_CONFIG.LLMLocal.Embedding.ContextSize; got != 2048 {
		t.Fatalf("default embedding context-size = %d, want 2048", got)
	}
	if got := global.GVA_CONFIG.LLMLocal.Rerank.ContextSize; got != 8192 {
		t.Fatalf("default rerank context-size = %d, want 8192", got)
	}
	if got := global.GVA_CONFIG.LLMLocal.Rerank.RerankMaxSequences; got != 2 {
		t.Fatalf("default CUDA rerank max-sequences = %d, want 2", got)
	}
}

func TestInitializeClientViperReadsUserConfig(t *testing.T) {
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "client.yaml")
	if err := os.WriteFile(configPath, []byte("llm-local:\n  rerank:\n    gpu-layers: 0\n    threads: 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INKFLOW_CLIENT_CONFIG", configPath)
	oldConfig := global.GVA_CONFIG
	oldBackend := desktopBackend
	desktopBackend = "cuda"
	defer func() {
		global.GVA_CONFIG = oldConfig
		desktopBackend = oldBackend
	}()

	InitializeClientViper()
	if got := global.GVA_CONFIG.LLMLocal.Rerank.GPULayers; got != 0 {
		t.Fatalf("configured rerank gpu-layers = %d, want 0", got)
	}
	if got := global.GVA_CONFIG.LLMLocal.Rerank.Threads; got != 3 {
		t.Fatalf("configured rerank threads = %d, want 3", got)
	}
}

func TestInitializeClientViperForcesCPUOnlyPackageToCPU(t *testing.T) {
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "client.yaml")
	if err := os.WriteFile(configPath, []byte("llm-local:\n  rerank:\n    gpu-layers: -1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INKFLOW_CLIENT_CONFIG", configPath)
	oldConfig := global.GVA_CONFIG
	oldBackend := desktopBackend
	desktopBackend = "cpu"
	defer func() {
		global.GVA_CONFIG = oldConfig
		desktopBackend = oldBackend
	}()

	InitializeClientViper()
	if got := global.GVA_CONFIG.LLMLocal.Rerank.GPULayers; got != 0 {
		t.Fatalf("CPU package rerank gpu-layers = %d, want 0", got)
	}
}
