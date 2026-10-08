// agent-model-bench measures InkFlow's local embedding and rerank models using
// the public synthetic corpus. Build tags and PATH select CPU, CUDA or Vulkan.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"InkFlow/internal/ai/eval"
	"InkFlow/utils/llamacpp"
)

type report struct {
	Backend         string  `json:"backend"`
	Mode            string  `json:"mode"`
	MeasuredAt      string  `json:"measured_at"`
	Model           string  `json:"model"`
	ModelLoadMS     float64 `json:"model_load_ms"`
	InputCount      int     `json:"input_count"`
	DocumentsPerRun int     `json:"documents_per_run,omitempty"`
	WarmupRuns      int     `json:"warmup_runs"`
	Runs            int     `json:"runs"`
	LatencyP50MS    float64 `json:"latency_p50_ms"`
	LatencyP95MS    float64 `json:"latency_p95_ms"`
	ThroughputPerS  float64 `json:"throughput_per_second"`
	VectorDims      int     `json:"vector_dimensions,omitempty"`
}

func main() {
	mode := flag.String("mode", "embedding", "embedding or rerank")
	backend := flag.String("backend", "cpu", "cpu, cuda or vulkan; must match build tag and PATH")
	model := flag.String("model", "", "GGUF model path")
	count := flag.Int("count", 20, "number of embedding inputs or rerank runs")
	docsPerRun := flag.Int("docs", 8, "rerank documents per run")
	warmup := flag.Int("warmup", 1, "untimed warmup runs")
	output := flag.String("output", "", "optional JSON output path")
	flag.Parse()
	if *model == "" || *count <= 0 || *docsPerRun <= 0 || *warmup < 0 {
		fail("model path, positive count/docs and nonnegative warmup are required")
	}
	if *mode != "embedding" && *mode != "rerank" {
		fail("unsupported mode %q", *mode)
	}
	if *backend != "cpu" && *backend != "cuda" && *backend != "vulkan" {
		fail("unsupported backend %q", *backend)
	}
	if _, err := os.Stat(*model); err != nil {
		fail("model: %v", err)
	}

	options := llamacpp.Options{ContextSize: 2048, Threads: 8, ThreadsBatch: 8}
	if *backend != "cpu" {
		options.GPULayers = -1
	}
	if *mode == "embedding" {
		options.IsEmbedding = true
	}
	if *mode == "rerank" {
		options.ContextSize = llamacpp.RerankBatchTokens
		options.IsRerank = true
		options.BatchSize = llamacpp.RerankBatchTokens
		options.PhysicalBatchSize = llamacpp.RerankBatchTokens
		options.RerankMaxSequences = 2
	}
	started := time.Now()
	engine, err := llamacpp.NewLocal(*model, options)
	if err != nil {
		fail("load model: %v", err)
	}
	defer engine.Close()
	loadMS := elapsedMS(started)

	corpus := eval.Corpus()
	latencies := make([]float64, 0, *count)
	vectorDims := 0
	run := func(iteration int) {
		if *mode == "embedding" {
			vector, err := engine.Embedding(corpus[iteration%len(corpus)].Content)
			if err != nil {
				fail("embed input %d: %v", iteration, err)
			}
			vectorDims = len(vector)
			if vectorDims == 0 {
				fail("empty embedding for input %d", iteration)
			}
		} else {
			docs := make([]string, *docsPerRun)
			for index := range docs {
				docs[index] = corpus[(iteration+index)%len(corpus)].Content
			}
			scores, err := engine.Rerank("档案移交工作应当如何办理？", docs)
			if err != nil {
				fail("rerank run %d: %v", iteration, err)
			}
			if len(scores) != len(docs) {
				fail("rerank returned %d scores for %d documents", len(scores), len(docs))
			}
		}
	}
	for iteration := 0; iteration < *warmup; iteration++ {
		run(iteration)
	}
	for iteration := 0; iteration < *count; iteration++ {
		started = time.Now()
		run(iteration)
		latencies = append(latencies, elapsedMS(started))
	}
	totalSeconds := 0.0
	for _, latency := range latencies {
		totalSeconds += latency / 1000
	}
	throughput := float64(*count) / totalSeconds
	if *mode == "rerank" {
		throughput *= float64(*docsPerRun)
	}
	result := report{Backend: *backend, Mode: *mode, MeasuredAt: time.Now().Format(time.RFC3339), Model: filepath.Base(*model), ModelLoadMS: round(loadMS), InputCount: *count, WarmupRuns: *warmup, Runs: *count, LatencyP50MS: percentile(latencies, .50), LatencyP95MS: percentile(latencies, .95), ThroughputPerS: round(throughput), VectorDims: vectorDims}
	if *mode == "rerank" {
		result.DocumentsPerRun = *docsPerRun
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fail("encode report: %v", err)
	}
	fmt.Println(string(encoded))
	if *output != "" {
		if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
			fail("create output directory: %v", err)
		}
		if err := os.WriteFile(*output, append(encoded, '\n'), 0o644); err != nil {
			fail("write report: %v", err)
		}
	}
}

func elapsedMS(start time.Time) float64 { return float64(time.Since(start).Microseconds()) / 1000 }
func percentile(values []float64, p float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	index := int(math.Ceil(p*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	return round(sorted[index])
}
func round(value float64) float64 { return math.Round(value*1000) / 1000 }
func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "agent-model-bench: "+format+"\n", args...)
	os.Exit(2)
}
