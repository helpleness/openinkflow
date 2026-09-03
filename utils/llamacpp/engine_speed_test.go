package llamacpp_test

import (
	"fmt"
	"os"
	"testing"
	"time"
	"unicode/utf8"

	"InkFlow/utils/llamacpp"
)

const testModelPath = "../../llama/llama.cpp/models/qwen2.5-3b-instruct-q4_k_m.gguf"
const testEmbedModelPath = "../../llama/llama.cpp/models/qwen3-embedding-0.6b-q4_k_m.gguf"

func TestLocalEngineSpeed(t *testing.T) {
	skipIfModelMissing(t, testModelPath)

	opt := llamacpp.Options{
		ContextSize:   4096,
		Threads:       8,
		ThreadsBatch:  8,
		FlashAttnAuto: true,
	}

	t.Logf("loading chat model: %s", testModelPath)
	startLoad := time.Now()
	eng, err := llamacpp.NewLocal(testModelPath, opt)
	if err != nil {
		t.Fatalf("load chat model: %v", err)
	}
	defer eng.Close()
	t.Logf("chat model loaded in %v", time.Since(startLoad))

	prompt := "请用冷峻、简洁的实体书风格，描写一段主角在雨夜中被仇家追杀的场景，约 200 字。"
	genOpt := llamacpp.Options{Temperature: 0.7, MaxTokens: 512}
	startGen := time.Now()
	out, err := eng.Complete(prompt, genOpt)
	if err != nil {
		t.Fatalf("complete text: %v", err)
	}
	genDuration := time.Since(startGen)
	charCount := utf8.RuneCountInString(out)
	fmt.Printf("\n>>> generation result <<<\n%s\n\n", out)
	t.Logf("generated %d chars in %v (%.2f chars/sec)", charCount, genDuration, float64(charCount)/genDuration.Seconds())

	skipIfModelMissing(t, testEmbedModelPath)
	embedOpt := llamacpp.Options{
		ContextSize: 8192,
		Threads:     8,
		IsEmbedding: true,
	}

	t.Logf("loading embedding model: %s", testEmbedModelPath)
	startEmbLoad := time.Now()
	embEng, err := llamacpp.NewLocal(testEmbedModelPath, embedOpt)
	if err != nil {
		t.Fatalf("load embedding model: %v", err)
	}
	defer embEng.Close()
	t.Logf("embedding model loaded in %v", time.Since(startEmbLoad))

	embedText := "【角色状态】墨离 轻微擦伤 @地牢大厅 (HP:80, MP:20) 试图利用断剑反击，但被对方看破。"
	startEmb := time.Now()
	vec, err := embEng.Embedding(embedText)
	if err != nil {
		t.Fatalf("embedding text: %v", err)
	}
	t.Logf("embedding dimension: %d", len(vec))
	t.Logf("embedding duration: %v", time.Since(startEmb))
}

func TestChatSessionSpeed(t *testing.T) {
	skipIfModelMissing(t, testModelPath)

	opt := llamacpp.Options{
		ContextSize:  2048,
		Threads:      8,
		SystemPrompt: "你是一个专业的跑团 DM，用简短的中文回答。",
	}

	eng, err := llamacpp.NewLocal(testModelPath, opt)
	if err != nil {
		t.Fatalf("load chat model: %v", err)
	}
	defer eng.Close()

	sess := llamacpp.NewChatSession(eng, opt)

	start1 := time.Now()
	ans1, err := sess.Send("玩家墨离走进了破庙，他四处张望。请描述环境。")
	if err != nil {
		t.Fatalf("first chat turn: %v", err)
	}
	t.Logf("first turn took %v:\n%s", time.Since(start1), ans1)

	start2 := time.Now()
	ans2, err := sess.Send("墨离拔出了断剑，向佛像后面的阴影走去。")
	if err != nil {
		t.Fatalf("second chat turn: %v", err)
	}
	t.Logf("second turn took %v:\n%s", time.Since(start2), ans2)
}

func skipIfModelMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Skipf("model file not found: %s", path)
	}
}
