// agent-bench measures the retrieval layer used by Agent Eval. It deliberately
// uses a deterministic synthetic vector corpus so the 100k-chunk run is safe
// to publish and comparable across local machines.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	usearch "github.com/unum-cloud/usearch/golang"
)

type retrievalReport struct {
	Workload            string  `json:"workload"`
	MeasuredAt          string  `json:"measured_at"`
	Chunks              int     `json:"chunks"`
	Dimensions          int     `json:"dimensions"`
	QueryCount          int     `json:"query_count"`
	TopK                int     `json:"top_k"`
	BuildSeconds        float64 `json:"build_seconds"`
	RetrievalP50MS      float64 `json:"retrieval_p50_ms"`
	RetrievalP95MS      float64 `json:"retrieval_p95_ms"`
	RecallAtK           float64 `json:"recall_at_k"`
	IndexMemoryMB       float64 `json:"index_memory_mb"`
	USearchCompiledISA  string  `json:"usearch_compiled_isa"`
	USearchAvailableISA string  `json:"usearch_available_isa"`
}

func main() {
	flags := flag.NewFlagSet("agent-bench", flag.ExitOnError)
	chunks := flags.Int("chunks", 100000, "number of synthetic chunks")
	dimensions := flags.Int("dimensions", 384, "vector dimensions")
	queries := flags.Int("queries", 1000, "number of exact-vector queries")
	topK := flags.Int("top-k", 10, "retrieval result count")
	output := flags.String("output", "", "optional JSON output path")
	_ = flags.Parse(os.Args[1:])
	if *chunks <= 0 || *dimensions <= 0 || *queries <= 0 || *topK <= 0 {
		fail("all numeric options must be positive")
	}
	if *topK > *chunks {
		fail("top-k cannot exceed chunks")
	}

	config := usearch.DefaultConfig(uint(*dimensions))
	config.Connectivity = 16
	config.ExpansionAdd = 128
	config.ExpansionSearch = 64
	index, err := usearch.NewIndex(config)
	if err != nil {
		fail("create index: %v", err)
	}
	defer index.Destroy()
	threads := uint(runtime.NumCPU())
	if threads == 0 {
		threads = 1
	}
	if err := index.ChangeThreadsAdd(threads); err != nil {
		fail("configure add threads: %v", err)
	}
	if err := index.ChangeThreadsSearch(threads); err != nil {
		fail("configure search threads: %v", err)
	}
	if err := index.Reserve(uint(*chunks)); err != nil {
		fail("reserve index: %v", err)
	}

	vectors := make([][]float32, *chunks)
	random := rand.New(rand.NewSource(20261008))
	start := time.Now()
	for indexPosition := range vectors {
		vector := randomVector(random, *dimensions)
		vectors[indexPosition] = vector
		if err := index.Add(usearch.Key(indexPosition+1), vector); err != nil {
			fail("add vector %d: %v", indexPosition+1, err)
		}
	}
	buildSeconds := time.Since(start).Seconds()

	latencies := make([]float64, 0, *queries)
	hits := 0
	for queryIndex := 0; queryIndex < *queries; queryIndex++ {
		position := (queryIndex * *chunks) / *queries
		started := time.Now()
		keys, _, err := index.Search(vectors[position], uint(*topK))
		if err != nil {
			fail("search query %d: %v", queryIndex, err)
		}
		latencies = append(latencies, float64(time.Since(started).Microseconds())/1000)
		for _, key := range keys {
			if key == usearch.Key(position+1) {
				hits++
				break
			}
		}
	}
	indexBytes, err := index.MemoryUsage()
	if err != nil {
		fail("read index memory: %v", err)
	}
	report := retrievalReport{
		Workload:   "deterministic-synthetic-hnsw-retrieval",
		MeasuredAt: time.Now().Format(time.RFC3339), Chunks: *chunks, Dimensions: *dimensions,
		QueryCount: *queries, TopK: *topK, BuildSeconds: round(buildSeconds),
		RetrievalP50MS: percentile(latencies, .50), RetrievalP95MS: percentile(latencies, .95),
		RecallAtK: round(float64(hits) / float64(*queries)), IndexMemoryMB: round(float64(indexBytes) / 1024 / 1024),
		USearchCompiledISA: usearch.HardwareAccelerationCompiled(), USearchAvailableISA: usearch.HardwareAccelerationAvailable(),
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fail("encode report: %v", err)
	}
	fmt.Println(string(encoded))
	if *output != "" {
		if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
			fail("make output directory: %v", err)
		}
		if err := os.WriteFile(*output, append(encoded, '\n'), 0o644); err != nil {
			fail("write report: %v", err)
		}
	}
}

func randomVector(random *rand.Rand, dimensions int) []float32 {
	vector := make([]float32, dimensions)
	var sum float64
	for position := range vector {
		value := random.Float32()*2 - 1
		vector[position] = value
		sum += float64(value * value)
	}
	scale := float32(1 / math.Sqrt(sum))
	for position := range vector {
		vector[position] *= scale
	}
	return vector
}

func percentile(values []float64, portion float64) float64 {
	copyOfValues := append([]float64(nil), values...)
	sort.Float64s(copyOfValues)
	position := int(math.Ceil(portion*float64(len(copyOfValues)))) - 1
	if position < 0 {
		position = 0
	}
	return round(copyOfValues[position])
}
func round(value float64) float64 { return math.Round(value*1000) / 1000 }
func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "agent-bench: "+format+"\n", args...)
	os.Exit(2)
}
