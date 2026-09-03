package inference

import (
	"InkFlow/global"
	"testing"
)

func TestActiveProviderFollowsInferenceProvider(t *testing.T) {
	oldConfig := global.GVA_CONFIG
	oldLogger := global.GVA_LOG
	t.Cleanup(func() {
		global.GVA_CONFIG = oldConfig
		global.GVA_LOG = oldLogger
	})
	global.GVA_LOG = nil

	global.GVA_CONFIG.LLM.InferenceProvider = "frontend"
	if _, ok := ActiveProvider().(FrontendProvider); !ok {
		t.Fatal("frontend provider should select FrontendProvider")
	}

	global.GVA_CONFIG.LLM.InferenceProvider = "local"
	if _, ok := ActiveProvider().(LocalProvider); !ok {
		t.Fatal("local provider should select LocalProvider")
	}

	global.GVA_CONFIG.LLM.InferenceProvider = "unexpected"
	if _, ok := ActiveProvider().(LocalProvider); !ok {
		t.Fatal("unknown provider should safely fall back to LocalProvider")
	}
}
