// agent-eval materializes the public-office evaluation suite and scores traces
// produced by a real InkFlow agent adapter.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"InkFlow/internal/ai/eval"
)

func main() {
	if len(os.Args) < 2 {
		die("usage: agent-eval <suite|score> [options]")
	}
	switch os.Args[1] {
	case "suite":
		runSuite(os.Args[2:])
	case "score":
		runScore(os.Args[2:])
	default:
		die("unknown command %q; use suite or score", os.Args[1])
	}
}

func runSuite(args []string) {
	flags := flag.NewFlagSet("suite", flag.ExitOnError)
	tasksPath := flags.String("tasks", "eval/public_office_tasks_v2.jsonl", "task JSONL output")
	corpusPath := flags.String("corpus", "eval/public_office_corpus_v1.jsonl", "corpus JSONL output")
	_ = flags.Parse(args)
	must(writeJSONL(*tasksPath, eval.BuildSuite()))
	must(writeJSONL(*corpusPath, eval.Corpus()))
	fmt.Printf("wrote %d tasks to %s and %d chunks to %s\n", len(eval.BuildSuite()), *tasksPath, len(eval.Corpus()), *corpusPath)
}

func runScore(args []string) {
	flags := flag.NewFlagSet("score", flag.ExitOnError)
	resultsPath := flags.String("results", "", "agent result JSONL (required)")
	reportPath := flags.String("output", "", "optional JSON report output")
	ksRaw := flags.String("k", "1,3,5,10", "comma-separated K values")
	_ = flags.Parse(args)
	if strings.TrimSpace(*resultsPath) == "" {
		die("-results is required")
	}
	results := mustReadJSONL[eval.Result](*resultsPath)
	report, err := eval.Score(eval.BuildSuite(), results, parseKs(*ksRaw)...)
	must(err)
	bytes, err := json.MarshalIndent(report, "", "  ")
	must(err)
	fmt.Println(string(bytes))
	if *reportPath != "" {
		must(writeFile(*reportPath, append(bytes, '\n')))
	}
}

func parseKs(raw string) []int {
	var values []int
	for _, part := range strings.Split(raw, ",") {
		var value int
		if _, err := fmt.Sscan(strings.TrimSpace(part), &value); err == nil && value > 0 {
			values = append(values, value)
		}
	}
	if len(values) == 0 {
		die("-k must contain at least one positive integer")
	}
	return values
}

func writeJSONL[T any](path string, items []T) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	for _, item := range items {
		if err := encoder.Encode(item); err != nil {
			return err
		}
	}
	return file.Close()
}

func mustReadJSONL[T any](path string) []T {
	file, err := os.Open(path)
	must(err)
	defer file.Close()
	items := []T{}
	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 1024)
	scanner.Buffer(buffer, 2*1024*1024)
	for line := 1; scanner.Scan(); line++ {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var item T
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			die("decode %s:%d: %v", path, line, err)
		}
		items = append(items, item)
	}
	must(scanner.Err())
	return items
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
func must(err error) {
	if err != nil {
		die("%v", err)
	}
}
func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "agent-eval: "+format+"\n", args...)
	os.Exit(2)
}
