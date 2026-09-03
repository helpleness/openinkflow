//go:build windows && cgo

package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"InkFlow/global"
	"InkFlow/utils/llamacpp"
)

const (
	// top_n 是测试需要验证的稳定行为；查询文本和候选数量从传入日志读取，
	// 这样既能复用仓库样例日志，也能直接复测客户端当天产生的日志。
	expectedRerankTopN = 6
)

type rerankInfoInput struct {
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	DocCount  int      `json:"doc_count"`
	TopN      int      `json:"top_n"`
}

// TestLocalProviderRerankFromInfoLog exercises utils/inference/local.go all
// the way through the Windows cgo llama.cpp engine. It intentionally reads
// the last "Rerank model raw input" record instead of embedding the very large
// JSON payload from info.log into the test source.
func TestLocalProviderRerankFromInfoLog(t *testing.T) {
	overallStart := time.Now()

	infoPath := testPathFromEnv("INKFLOW_RERANK_INFO_LOG", defaultRerankInfoLog())
	modelPath := testPathFromEnv("INKFLOW_RERANK_MODEL", defaultRerankModelPath())
	t.Logf("[rerank] info log: %s", infoPath)
	t.Logf("[rerank] model: %s", modelPath)

	stageStart := time.Now()
	fixture, err := readLastRerankInfoInput(infoPath)
	if err != nil {
		t.Fatalf("read rerank fixture: %v", err)
	}
	logRerankStage(t, "read/parse info.log", stageStart)

	if fixture.DocCount != 0 && fixture.DocCount != len(fixture.Documents) {
		t.Fatalf("info.log doc_count = %d, but documents contains %d items", fixture.DocCount, len(fixture.Documents))
	}
	if fixture.TopN != expectedRerankTopN {
		t.Fatalf("top_n from info.log = %d, want %d", fixture.TopN, expectedRerankTopN)
	}
	t.Logf("[rerank] fixture query=%q documents=%d top_n=%d", fixture.Query, len(fixture.Documents), fixture.TopN)

	stageStart = time.Now()
	if fileInfo, err := os.Stat(modelPath); err != nil {
		t.Fatalf("stat rerank model %q: %v", modelPath, err)
	} else if fileInfo.Size() == 0 {
		t.Fatalf("rerank model is empty: %s", modelPath)
	}
	logRerankStage(t, "stat model file", stageStart)

	// Keep these values aligned with initialize.InitRerankEngine. In
	// particular, IsRerank selects rank pooling and the two batch sizes select
	// the batched graph used by the cgo implementation.
	batchTokens := rerankTestIntEnv("INKFLOW_RERANK_BATCH_TOKENS", llamacpp.RerankBatchTokens)
	// Two sequences is the validated Ada CUDA sweet spot for this BERT shape.
	// It changes only micro-batch scheduling: all documents are still encoded.
	maxSequences := rerankTestIntEnv("INKFLOW_RERANK_MAX_SEQUENCES", 2)
	maxTokensPerSequence := rerankTestIntEnv("INKFLOW_RERANK_MAX_TOKENS_PER_SEQUENCE", 0)
	options := llamacpp.Options{
		ContextSize:                llamacpp.RerankBatchTokens,
		Threads:                    8,
		ThreadsBatch:               8,
		IsRerank:                   true,
		GPULayers:                  rerankTestGPULayers(),
		DisableFlashAttn:           rerankTestDisableFlashAttn(),
		BatchSize:                  batchTokens,
		PhysicalBatchSize:          batchTokens,
		RerankMaxSequences:         maxSequences,
		RerankMaxTokensPerSequence: maxTokensPerSequence,
		OnRerankBatch: func(stats llamacpp.RerankBatchStats) {
			t.Logf("[rerank][batch %d] documents=%d token_total=%d max_seq_len=%d llama_encode=%s",
				stats.BatchIndex,
				stats.DocumentCount,
				stats.TokenCount,
				stats.MaxSequenceLength,
				stats.EncodeDuration,
			)
		},
	}
	t.Logf("[rerank] gpu-layers=%d", options.GPULayers)
	t.Logf("[rerank] disable-flash-attn=%t", options.DisableFlashAttn)
	t.Logf("[rerank] batch-tokens=%d max-sequences=%d", batchTokens, maxSequences)
	t.Logf("[rerank] max-tokens-per-sequence=%d", maxTokensPerSequence)

	stageStart = time.Now()
	engine, err := llamacpp.NewLocal(modelPath, options)
	if err != nil {
		t.Fatalf("load local rerank model through cgo: %v", err)
	}
	logRerankStage(t, "load model through cgo", stageStart)
	defer func() {
		closeStart := time.Now()
		if err := engine.Close(); err != nil {
			t.Errorf("close rerank engine: %v", err)
		}
		logRerankStage(t, "close model", closeStart)
	}()

	// LocalProvider.Rerank is the code under test. The global is only swapped
	// for this test and restored before it returns; the engine is closed by the
	// defer above.
	oldEngine := global.GVA_LLM_RERANK
	global.GVA_LLM_RERANK = engine
	defer func() { global.GVA_LLM_RERANK = oldEngine }()

	stageStart = time.Now()
	results, err := (LocalProvider{}).Rerank(
		context.Background(),
		fixture.Query,
		fixture.Documents,
		fixture.TopN,
	)
	if err != nil {
		t.Fatalf("rerank through LocalProvider: %v", err)
	}
	logRerankStage(t, "rerank inference + LocalProvider sorting", stageStart)

	stageStart = time.Now()
	validateRerankResults(t, fixture.Documents, fixture.TopN, results)
	logRerankStage(t, "validate top-n results", stageStart)

	for rank, result := range results {
		t.Logf("[rerank][result] rank=%d index=%d score=%.7f first_line=%q",
			rank+1, result.Index, result.Score, firstLine(result.Text))
	}
	logRerankStage(t, "total test", overallStart)
}

func readLastRerankInfoInput(path string) (rerankInfoInput, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return rerankInfoInput{}, err
	}

	const marker = "Rerank model raw input"
	var (
		input rerankInfoInput
		found bool
	)
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if !bytes.Contains(line, []byte(marker)) {
			continue
		}
		jsonStart := bytes.IndexByte(line, '{')
		if jsonStart < 0 {
			return rerankInfoInput{}, fmt.Errorf("raw-input log line has no JSON object")
		}
		var candidate rerankInfoInput
		if err := json.Unmarshal(line[jsonStart:], &candidate); err != nil {
			return rerankInfoInput{}, fmt.Errorf("decode raw-input JSON: %w", err)
		}
		input = candidate
		found = true
	}
	if !found {
		return rerankInfoInput{}, fmt.Errorf("no %q entry found", marker)
	}
	if input.Query == "" || len(input.Documents) == 0 || input.TopN <= 0 {
		return rerankInfoInput{}, fmt.Errorf("raw-input entry is incomplete: query=%q documents=%d top_n=%d", input.Query, len(input.Documents), input.TopN)
	}
	return input, nil
}

func validateRerankResults(t *testing.T, documents []string, topN int, results []ScoredDocument) {
	t.Helper()
	wantCount := topN
	if wantCount > len(documents) {
		wantCount = len(documents)
	}
	if len(results) != wantCount {
		t.Fatalf("result count = %d, want %d", len(results), wantCount)
	}

	seen := make(map[int]struct{}, len(results))
	for index, result := range results {
		if result.Index < 0 || result.Index >= len(documents) {
			t.Fatalf("result %d has out-of-range index %d", index, result.Index)
		}
		if _, ok := seen[result.Index]; ok {
			t.Fatalf("result %d repeats document index %d", index, result.Index)
		}
		seen[result.Index] = struct{}{}
		if result.Text != documents[result.Index] {
			t.Fatalf("result %d text does not match document index %d", index, result.Index)
		}
		if math.IsNaN(float64(result.Score)) || math.IsInf(float64(result.Score), 0) {
			t.Fatalf("result %d has invalid score %v", index, result.Score)
		}
		if index > 0 && results[index-1].Score < result.Score {
			t.Fatalf("results are not sorted descending at rank %d: %.7f < %.7f", index+1, results[index-1].Score, result.Score)
		}
	}
}

func testPathFromEnv(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func defaultRerankModelPath() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		home = strings.TrimSpace(os.Getenv("USERPROFILE"))
	}
	return filepath.Join(home, "AppData", "Local", "InkFlow", "models", "bge-reranker-v2-m3-Q4_K_M.gguf")
}

func defaultRerankInfoLog() string {
	return filepath.Join("..", "..", "docs", "reranktestcuda.log")
}

func rerankTestDisableFlashAttn() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("INKFLOW_RERANK_DISABLE_FLASH_ATTN"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func rerankTestIntEnv(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func firstLine(text string) string {
	line := strings.SplitN(text, "\n", 2)[0]
	const maxRunes = 160
	runes := []rune(line)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "…"
	}
	return line
}

func logRerankStage(t *testing.T, stage string, start time.Time) {
	t.Helper()
	t.Logf("[rerank][timing] %-36s %s", stage, time.Since(start))
}
