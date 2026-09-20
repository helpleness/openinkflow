package initialize

import "testing"

func TestHFMirrorAllowsFrontendModelIDs(t *testing.T) {
	paths := []string{
		"/onnx-community/Qwen3-Embedding-0.6B-ONNX/resolve/main/tokenizer.json",
		"/onnx-community/Qwen3-Embedding-0.6B-ONNX/resolve/main/onnx/model_q4f16.onnx",
		"/onnx-community/bge-reranker-v2-m3-ONNX/resolve/main/tokenizer.json",
		"/onnx-community/bge-reranker-v2-m3-ONNX/resolve/main/tokenizer_config.json",
		"/onnx-community/bge-reranker-v2-m3-ONNX/resolve/main/onnx/model_q4f16.onnx",
	}
	for _, path := range paths {
		if !isPathAllowed(path) {
			t.Errorf("frontend model path unexpectedly rejected: %s", path)
		}
	}
}

func TestHFMirrorRejectsUnrelatedModels(t *testing.T) {
	if isPathAllowed("/untrusted/model/resolve/main/model.onnx") {
		t.Fatal("unrelated model path unexpectedly allowed")
	}
}
