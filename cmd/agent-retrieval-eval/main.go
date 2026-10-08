// agent-retrieval-eval runs the retrieval stage of the public-office suite
// with local GGUF embedding and rerank models. It does not call a chat agent
// or judge writing quality; those require a separately configured runner.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"InkFlow/internal/ai/eval"
	"InkFlow/utils/llamacpp"
	usearch "github.com/unum-cloud/usearch/golang"
)

func main() {
	backend := flag.String("backend", "cpu", "cpu, cuda or vulkan; must match build tag and PATH")
	embeddingPath := flag.String("embedding-model", "", "embedding GGUF model")
	rerankPath := flag.String("rerank-model", "", "rerank GGUF model")
	resultPath := flag.String("results", "eval/results/retrieval-eval.jsonl", "result JSONL path")
	reportPath := flag.String("report", "eval/results/retrieval-eval-report.json", "score report JSON path")
	flag.Parse()
	if *embeddingPath == "" || *rerankPath == "" {
		fail("embedding-model and rerank-model are required")
	}
	if *backend != "cpu" && *backend != "cuda" && *backend != "vulkan" {
		fail("unsupported backend %q", *backend)
	}
	gpuLayers := 0
	if *backend != "cpu" {
		gpuLayers = -1
	}
	embedding, err := llamacpp.NewLocal(*embeddingPath, llamacpp.Options{ContextSize: 2048, Threads: 8, ThreadsBatch: 8, IsEmbedding: true, GPULayers: gpuLayers})
	if err != nil {
		fail("load embedding model: %v", err)
	}
	defer embedding.Close()
	corpus := eval.Corpus()
	vectors := make([][]float32, len(corpus))
	for i, chunk := range corpus {
		vectors[i], err = embedding.Embedding(chunk.Content)
		if err != nil {
			fail("embed chunk %s: %v", chunk.ID, err)
		}
	}
	config := usearch.DefaultConfig(uint(len(vectors[0])))
	index, err := usearch.NewIndex(config)
	if err != nil {
		fail("create index: %v", err)
	}
	defer index.Destroy()
	if err := index.Reserve(uint(len(corpus))); err != nil {
		fail("reserve index: %v", err)
	}
	for i, vector := range vectors {
		if err := index.Add(usearch.Key(i+1), vector); err != nil {
			fail("index chunk %s: %v", corpus[i].ID, err)
		}
	}

	ranker, err := llamacpp.NewLocal(*rerankPath, llamacpp.Options{ContextSize: llamacpp.RerankBatchTokens, Threads: 8, ThreadsBatch: 8, IsRerank: true, GPULayers: gpuLayers, BatchSize: llamacpp.RerankBatchTokens, PhysicalBatchSize: llamacpp.RerankBatchTokens, RerankMaxSequences: 2})
	if err != nil {
		fail("load rerank model: %v", err)
	}
	defer ranker.Close()

	results := make([]eval.Result, 0, 20)
	for _, task := range eval.BuildSuite() {
		if task.Kind != eval.KindRetrieval {
			continue
		}
		queryVector, err := embedding.Embedding(task.Prompt)
		if err != nil {
			fail("embed task %s: %v", task.ID, err)
		}
		keys, _, err := index.Search(queryVector, 10)
		if err != nil {
			fail("retrieve task %s: %v", task.ID, err)
		}
		ids := make([]string, 0, len(keys))
		docs := make([]string, 0, len(keys))
		for _, key := range keys {
			chunk := corpus[int(key)-1]
			ids = append(ids, chunk.ID)
			docs = append(docs, chunk.Content)
		}
		scores, err := ranker.Rerank(task.Prompt, docs)
		if err != nil {
			fail("rerank task %s: %v", task.ID, err)
		}
		if len(scores) != len(ids) {
			fail("rerank task %s returned %d scores for %d docs", task.ID, len(scores), len(ids))
		}
		order := make([]int, len(ids))
		for i := range order {
			order[i] = i
		}
		sort.SliceStable(order, func(i, j int) bool { return scores[order[i]] > scores[order[j]] })
		reranked := make([]string, len(ids))
		for i, position := range order {
			reranked[i] = ids[position]
		}
		results = append(results, eval.Result{TaskID: task.ID, RetrievedChunkIDs: ids, RerankedChunkIDs: reranked})
	}
	if err := os.MkdirAll(filepath.Dir(*resultPath), 0o755); err != nil {
		fail("create result directory: %v", err)
	}
	file, err := os.Create(*resultPath)
	if err != nil {
		fail("create result file: %v", err)
	}
	encoder := json.NewEncoder(file)
	for _, result := range results {
		if err := encoder.Encode(result); err != nil {
			fail("write result: %v", err)
		}
	}
	if err := file.Close(); err != nil {
		fail("close result file: %v", err)
	}
	report, err := eval.Score(eval.BuildSuite(), results)
	if err != nil {
		fail("score results: %v", err)
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fail("encode report: %v", err)
	}
	if err := os.WriteFile(*reportPath, append(encoded, '\n'), 0o644); err != nil {
		fail("write report: %v", err)
	}
	fmt.Println(string(encoded))
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "agent-retrieval-eval: "+format+"\n", args...)
	os.Exit(2)
}
