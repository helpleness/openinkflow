// agent-retrieval-eval runs the vector retrieval stage of the public-office
// suite against a saved and reloaded SQLite + USearch fixture. It does not
// call a chat agent or judge writing quality.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"InkFlow/internal/ai/eval"
	"InkFlow/utils/llamacpp"
)

func main() {
	backend := flag.String("backend", "cpu", "cpu, cuda or vulkan; must match build tag and PATH")
	embeddingPath := flag.String("embedding-model", "", "embedding GGUF model")
	rerankPath := flag.String("rerank-model", "", "rerank GGUF model")
	queryMode := flag.String("query-mode", "full", "full task prompt, focused retrieval query or topic only")
	fixtureRoot := flag.String("fixture-root", "eval/.local", "directory for retained SQLite and USearch fixtures")
	resultPath := flag.String("results", "eval/results/retrieval-eval.jsonl", "result JSONL path")
	reportPath := flag.String("report", "eval/results/retrieval-eval-report.json", "score report JSON path")
	validationPath := flag.String("validation", "eval/results/retrieval-eval-validation.json", "index validation JSON path")
	flag.Parse()
	if *embeddingPath == "" || *rerankPath == "" {
		fail("embedding-model and rerank-model are required")
	}
	if *backend != "cpu" && *backend != "cuda" && *backend != "vulkan" {
		fail("unsupported backend %q", *backend)
	}
	if *queryMode != "full" && *queryMode != "focused" && *queryMode != "topic" {
		fail("unsupported query mode %q", *queryMode)
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
		vectors[i], err = embedding.Embedding(strings.TrimSpace(chunk.Title + "\n" + chunk.Content))
		if err != nil {
			fail("embed chunk %s: %v", chunk.ID, err)
		}
	}
	fixture, err := buildFixture(*fixtureRoot, corpus, vectors)
	if err != nil {
		fail("build file-backed fixture: %v", err)
	}
	defer fixture.Close()
	fixture.Validation.QueryMode = *queryMode

	ranker, err := llamacpp.NewLocal(*rerankPath, llamacpp.Options{ContextSize: llamacpp.RerankBatchTokens, Threads: 8, ThreadsBatch: 8, IsRerank: true, GPULayers: gpuLayers, BatchSize: llamacpp.RerankBatchTokens, PhysicalBatchSize: llamacpp.RerankBatchTokens, RerankMaxSequences: 2})
	if err != nil {
		fail("load rerank model: %v", err)
	}
	defer ranker.Close()

	results := make([]eval.Result, 0, 20)
	exactResults := make([]eval.Result, 0, 20)
	for _, task := range eval.BuildSuite() {
		if task.Kind != eval.KindRetrieval {
			continue
		}
		query := task.Prompt
		switch *queryMode {
		case "focused":
			// Only use terms present in the user request, never gold chunk text or IDs.
			query = task.Topic + " 办理事项 完成时限 承办责任"
		case "topic":
			query = task.Topic
		}
		queryVector, err := embedding.Embedding(query)
		if err != nil {
			fail("embed task %s: %v", task.ID, err)
		}
		found, err := fixture.Search(queryVector, 10)
		if err != nil {
			fail("retrieve task %s: %v", task.ID, err)
		}
		ids := make([]string, 0, len(found))
		docs := make([]string, 0, len(found))
		for _, row := range found {
			ids = append(ids, row.Metadata)
			docs = append(docs, strings.TrimSpace(row.Title+"\n"+row.Content))
		}
		exactIDs, err := fixture.ExactTopK(queryVector, 10)
		if err != nil {
			fail("exact baseline for %s: %v", task.ID, err)
		}
		exactResults = append(exactResults, eval.Result{TaskID: task.ID, RetrievedChunkIDs: exactIDs})
		exactSet := make(map[string]bool, len(exactIDs))
		for _, id := range exactIDs {
			exactSet[id] = true
		}
		for _, id := range ids {
			if exactSet[id] {
				fixture.Validation.ExactTop10Overlap++
			}
		}
		fixture.Validation.ExactTop10Total += len(exactIDs)
		scores, err := ranker.Rerank(query, docs)
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
	exactReport, err := eval.Score(eval.BuildSuite(), exactResults)
	if err != nil {
		fail("score exact baseline: %v", err)
	}
	fixture.Validation.ExactRecallAtK = exactReport.RetrievalRecallAtK
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
	validation, err := json.MarshalIndent(fixture.Validation, "", "  ")
	if err != nil {
		fail("encode validation: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(*validationPath), 0o755); err != nil {
		fail("create validation directory: %v", err)
	}
	if err := os.WriteFile(*validationPath, append(validation, '\n'), 0o644); err != nil {
		fail("write validation: %v", err)
	}
	fmt.Printf("fixture directory: %s\n", fixture.Directory)
	fmt.Println(string(encoded))
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "agent-retrieval-eval: "+format+"\n", args...)
	os.Exit(2)
}
