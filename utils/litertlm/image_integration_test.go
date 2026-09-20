//go:build litertlm && cgo

package litertlm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGemma4E4BImageInference(t *testing.T) {
	if os.Getenv("LITERTLM_RUN_IMAGE_TEST") != "1" {
		t.Skip("set LITERTLM_RUN_IMAGE_TEST=1 to run the local Gemma 4 E4B image inference test")
	}

	root := repoRoot(t)
	modelPath := envOrDefault("LITERTLM_MODEL", filepath.Join(root, "model", "litertlm", "gemma-4-E4B-it.litertlm"))
	imagePath := envOrDefault("LITERTLM_IMAGE", filepath.Join(root, "utils", "litertlm", "测试.jpg"))

	mustExist(t, modelPath)
	mustExist(t, imagePath)

	eng, err := NewLocal(modelPath, Options{
		Backend:       "cpu",
		VisionBackend: "cpu",
		MaxTokens:     512,
		Temperature:   0.8,
		TopP:          0.9,
		SystemPrompt:  "调皮捣蛋的爱吐槽小猫",
		LogLevel:      1,
	})
	if err != nil {
		t.Fatalf("create LiteRT-LM engine: %v", err)
	}
	defer eng.Close()

	out, err := eng.Chat([]Message{
		{
			Role: "user",
			Content: []Part{
				{Type: "text", Text: "用中文识别并描述这张图片。保持调皮捣蛋、爱吐槽的小猫口吻，但先说清楚你看到了什么。"},
				{Type: "image", Path: imagePath},
			},
		},
	}, Options{
		MaxTokens:    512,
		Temperature:  0.8,
		TopP:         0.9,
		SystemPrompt: "调皮捣蛋的爱吐槽小猫",
	})
	if err != nil {
		t.Fatalf("image inference: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("image inference returned an empty response")
	}

	t.Logf("Gemma 4 E4B image response:\n%s", out)
}

func repoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			t.Fatalf("resolve repository root from working directory %q: go.mod not found", wd)
		}
		wd = parent
	}
}

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func mustExist(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("required file %q is not available: %v", path, err)
	}
}
